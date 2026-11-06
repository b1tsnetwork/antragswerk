package app

// GLPI-Schritte: Ticket/Change über die moderne GLPI-API (v2) anlegen, Status abfragen, Rückmeldung
// übernehmen. Netzwerkzugriffe laufen nie unter der Vorgangssperre (a.mu): erst Daten vorbereiten,
// dann GLPI ansprechen, dann das Ergebnis unter Sperre eintragen.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"antragswerk/internal/flow"
	"antragswerk/internal/glpi"
	"antragswerk/internal/store"
)

// Zustand eines GLPI-Schritts (in Instance.Outputs, Schlüssel mit Präfix glpi_):
//
//	glpi_phase   senden | offen | fehler | fertig
//	glpi_id, glpi_type, glpi_url, glpi_conn, glpi_status, glpi_status_name, glpi_solution
//	glpi_tries, glpi_next (Wiederholung), glpi_checked (letzte Abfrage), glpi_error, glpi_warn
const maxGLPITries = 4

var glpiBackoff = []time.Duration{0, time.Minute, 5 * time.Minute, 15 * time.Minute}

// GLPIConns liefert die Verbindungen.
func (a *App) GLPIConns(ctx context.Context) []glpi.Conn {
	var out []glpi.Conn
	for _, raw := range a.Store.RawDocs(ctx, "glpi") {
		var c glpi.Conn
		if json.Unmarshal([]byte(raw), &c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// GLPIConn liefert eine Verbindung (leer = erste).
func (a *App) GLPIConn(ctx context.Context, key string) (glpi.Conn, error) {
	cs := a.GLPIConns(ctx)
	for _, c := range cs {
		if key == "" || c.Key == key {
			return c, nil
		}
	}
	if key == "" {
		return glpi.Conn{}, errors.New("keine GLPI-Verbindung eingerichtet (Einstellungen → Schnittstellen)")
	}
	return glpi.Conn{}, fmt.Errorf("GLPI-Verbindung %q nicht vorhanden", key)
}

func glpiRef(r *store.Request, in *store.Instance) string {
	ref := r.Number + "/" + in.StepID
	if in.Item >= 0 {
		ref += fmt.Sprintf("#%d", in.Item+1)
	}
	return ref
}

// glpiVars sind die Platzhalter {{.glpi.*}} für Ticket-Texte.
func (a *App) glpiVars(r *store.Request, in *store.Instance) map[string]any {
	return map[string]any{
		"ref":          glpiRef(r, in),
		"token":        in.Token,
		"rueckmeldung": a.Cfg.BaseURL + "/glpi/r/" + in.Token,
		"api":          a.Cfg.BaseURL + "/api/glpi/r/" + in.Token,
		"vorgang":      a.Cfg.BaseURL + fmt.Sprintf("/v/%d", r.ID),
		"marker":       "[AW:" + in.Token + "]",
	}
}

// activateGLPI legt den Schritt an; die Übergabe erfolgt danach außerhalb der Sperre (GLPIKick).
func (a *App) activateGLPI(ctx context.Context, r *store.Request, s *flow.Step, in *store.Instance) error {
	in.State = "extern"
	in.Token = token()
	in.Outputs = map[string]any{"glpi_phase": "senden", "glpi_tries": 0}
	if s.GLPI != nil {
		in.Outputs["glpi_type"] = glpi.Itemtype(s.GLPI.Type)
		in.Outputs["glpi_conn"] = s.GLPI.Conn
	}
	if err := a.Store.InsertInstance(ctx, in); err != nil {
		return err
	}
	what := "Ticket"
	if s.GLPI != nil && s.GLPI.IsChange() {
		what = "Change"
	}
	if s.GLPI != nil && s.GLPI.Action == "followup" {
		what = "Folgeeintrag"
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "glpi",
		Msg: "Übergabe an GLPI vorbereitet: " + s.Title + " (" + what + ")"})
	a.glpiPending = append(a.glpiPending, in.ID)
	return nil
}

// GLPIKick verarbeitet frisch angelegte GLPI-Schritte (nach Freigabe der Sperre aufrufen).
func (a *App) GLPIKick(ctx context.Context) {
	a.mu.Lock()
	ids := a.glpiPending
	a.glpiPending = nil
	a.mu.Unlock()
	for _, id := range ids {
		a.GLPIProcess(ctx, id)
	}
}

// glpiJob ist der vorbereitete Auftrag (ohne Sperre ausführbar).
type glpiJob struct {
	conn     glpi.Conn
	step     flow.Step
	g        flow.GLPIStep
	itemtype string
	phase    string
	id       int64
	body     map[string]any
	team     []teamReq
	tasks    []map[string]any
	taskRefs []map[string]string
	valid    map[string]string
	followup map[string]any
	err      error
}

type teamReq struct{ role, typ, value string }

// GLPIProcess bringt einen GLPI-Schritt voran: übergeben bzw. Status abfragen.
func (a *App) GLPIProcess(ctx context.Context, instID int64) {
	job, ok := a.glpiPrepare(ctx, instID)
	if !ok {
		return
	}
	switch job.phase {
	case "senden":
		a.glpiSend(ctx, instID, job)
	case "offen":
		a.glpiPoll(ctx, instID, job)
	}
	a.mu.Lock()
	more := len(a.glpiPending) > 0
	a.mu.Unlock()
	if more { // durch den Abschluss gestartete Folgeschritte
		a.afterUnlock(ctx)
	}
}

func (a *App) glpiPrepare(ctx context.Context, instID int64) (*glpiJob, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	in, err := a.Store.Instance(ctx, instID)
	if err != nil || in.State != "extern" {
		return nil, false
	}
	r, env, ins, err := a.Load(ctx, in.RequestID)
	if err != nil || (r.Status != "laufend" && r.Status != "rueckfrage") {
		return nil, false
	}
	s := env.Def.StepByID(in.StepID)
	if s == nil || s.GLPI == nil {
		return nil, false
	}
	job := &glpiJob{step: *s, g: *s.GLPI, phase: str(in.Outputs["glpi_phase"]), itemtype: glpi.Itemtype(s.GLPI.Type), id: glpi.ID(in.Outputs["glpi_id"])}
	if job.phase == "senden" {
		if t, err := time.Parse(time.RFC3339, str(in.Outputs["glpi_next"])); err == nil && time.Now().Before(t) {
			return nil, false
		}
	}
	job.conn, job.err = a.GLPIConn(ctx, s.GLPI.Conn)
	if job.phase != "senden" {
		return job, true
	}
	// Texte rendern
	c := a.Context(env, r, ins)
	var item any
	if in.Item >= 0 && s.ForEach != "" {
		if items := env.Items(c, s.ForEach); in.Item < len(items) {
			item = items[in.Item]
		}
	}
	cc := a.mailCtx(env, c, s, in, item, r)
	cc["glpi"] = a.glpiVars(r, in)
	errs := a.glpiBuild(ctx, env, c, cc, s, item, in, ins, job)
	if len(errs) > 0 && job.err == nil {
		job.err = errors.New("Vorlagenfehler – " + strings.Join(errs, "; "))
	}
	return job, true
}

// glpiBuild rendert alle Angaben des Schritts in den Auftrag (auch für die Vorschau im Designer).
func (a *App) glpiBuild(ctx context.Context, env *flow.Env, c, cc map[string]any, s *flow.Step, item any, in *store.Instance, ins []*store.Instance, job *glpiJob) []string {
	var errs []string
	render := func(label, tpl string) string {
		if tpl == "" {
			return ""
		}
		v, err := flow.Render(tpl, cc, env.Vars)
		if err != nil {
			errs = append(errs, label+": "+err.Error())
		}
		return strings.TrimSpace(v)
	}
	g := s.GLPI
	if g.Action == "followup" {
		ref := env.Def.StepByID(g.Ref)
		var target *store.Instance
		for _, o := range ins {
			if o.StepID == g.Ref && glpi.ID(o.Outputs["glpi_id"]) > 0 && (o.Item == in.Item || o.Item < 0) {
				target = o
			}
		}
		if ref == nil || target == nil {
			job.err = errors.New("kein Ticket aus dem Schritt „" + g.Ref + "“ vorhanden")
			return errs
		}
		job.itemtype = str(target.Outputs["glpi_type"])
		job.id = glpi.ID(target.Outputs["glpi_id"])
		if tc := str(target.Outputs["glpi_conn"]); tc != "" {
			job.conn, job.err = a.GLPIConn(ctx, tc)
		}
		job.followup = map[string]any{"content": toHTML(render("Text", g.Content)), "is_private": g.Private}
	} else {
		content := g.Content
		if strings.TrimSpace(content) == "" {
			content = flow.GLPIDefaultContent
		}
		b := map[string]any{"name": render("Titel", g.Name), "content": toHTML(render("Text", content))}
		ints := map[string]string{"urgency": g.Urgency, "impact": g.Impact, "priority": g.Priority}
		if !g.IsChange() {
			ints["type"] = g.TicketType
		}
		for k, tpl := range ints {
			if v := render(k, tpl); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					b[k] = n
				} else {
					errs = append(errs, fmt.Sprintf("%s: %q ist keine Zahl", k, v))
				}
			}
		}
		if v := render("Status", g.Status); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				b["status"] = map[string]any{"id": n}
			}
		}
		if v := render("Lösungsfrist", g.DueDate); v != "" {
			b["resolution_date"] = glpiDate(v)
		}
		ext := g.ExternalID
		if ext == "" {
			ext = "{{.glpi.ref}}"
		}
		if !g.IsChange() {
			b["external_id"] = render("Externe ID", ext)
		}
		if g.IsChange() {
			for k, tpl := range map[string]string{"impactcontent": g.ImpactContent, "controlistcontent": g.ControlContent, "rolloutplancontent": g.RolloutPlan,
				"backoutplancontent": g.BackoutPlan, "checklistcontent": g.ChecklistPlan} {
				if v := render(k, tpl); v != "" {
					b[k] = toHTML(v)
				}
			}
		}
		// Verweise (ID oder Name) – werden beim Senden aufgelöst
		refs := map[string]string{"entity": render("Entität", g.Entity), "category": render("Kategorie", g.Category), "location": render("Ort", g.Location)}
		if refs["entity"] == "" {
			refs["entity"] = job.conn.Entity
		}
		if !g.IsChange() {
			refs["request_type"] = render("Anfragequelle", g.RequestType)
		}
		b["__refs"] = refs
		if x := render("Weitere Felder", g.Extra); x != "" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(x), &extra); err != nil {
				errs = append(errs, "Weitere Felder: kein gültiges JSON ("+err.Error()+")")
			}
			for k, v := range extra {
				b[k] = v
			}
		}
		job.body = b
		for _, t := range g.Team {
			if v := render("Team", t.Value); v != "" {
				for _, p := range strings.Split(v, ";") {
					if p = strings.TrimSpace(p); p != "" {
						job.team = append(job.team, teamReq{role: orStr(t.Role, "assigned"), typ: orStr(t.Type, "Group"), value: p})
					}
				}
			}
		}
		for i, t := range g.Tasks {
			if !t.When.Match(c, item) {
				continue
			}
			tb := map[string]any{"content": toHTML(render(fmt.Sprintf("Aufgabe %d", i+1), t.Content)), "is_private": t.Private}
			st := 1
			if v := render("Aufgabenstatus", t.State); v != "" {
				st, _ = strconv.Atoi(v)
			}
			tb["state"] = st
			if v := render("Dauer", t.Duration); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					tb["duration"] = n * 60
				}
			}
			if v := render("Beginn", t.Begin); v != "" {
				tb["planned_begin"] = glpiDate(v)
			}
			if v := render("Ende", t.End); v != "" {
				tb["planned_end"] = glpiDate(v)
			}
			job.tasks = append(job.tasks, tb)
			job.taskRefs = append(job.taskRefs, map[string]string{"user_tech": render("Techniker", t.UserTech), "group_tech": render("Techniker-Gruppe", t.GroupTech), "category": render("Aufgabenkategorie", t.Category)})
		}
		if v := g.Validation; v != nil && strings.TrimSpace(v.Approver) != "" {
			job.valid = map[string]string{"type": orStr(v.ApproverType, "User"), "approver": render("Genehmigende", v.Approver), "comment": render("Genehmigungstext", v.Comment)}
		}
	}
	return errs
}

var refKinds = map[string]string{"entity": "Entity", "category": "ITILCategory", "location": "Location", "request_type": "RequestType"}

// glpiSend übergibt an GLPI (ohne Sperre) und trägt das Ergebnis ein.
func (a *App) glpiSend(ctx context.Context, instID int64, job *glpiJob) {
	var warns []string
	id := job.id
	err := job.err
	cl := (*glpi.Client)(nil)
	if err == nil {
		cl = glpi.For(job.conn)
	}
	if err == nil && job.followup != nil {
		_, err = cl.AddTimeline(ctx, job.itemtype, id, "Followup", job.followup)
	} else if err == nil {
		body := job.body
		refs, _ := body["__refs"].(map[string]string)
		delete(body, "__refs")
		for field, v := range refs {
			if v == "" {
				continue
			}
			rid, e := cl.Resolve(ctx, refKinds[field], v)
			if e != nil {
				err = e
				break
			}
			body[field] = map[string]any{"id": rid}
		}
		if err == nil {
			id, err = cl.Create(ctx, job.itemtype, body)
		}
		if err == nil {
			// ab hier existiert das Ticket – Fehler bei Ergänzungen nur als Hinweis (nie doppelt anlegen)
			a.glpiSave(ctx, instID, map[string]any{"glpi_id": id, "glpi_url": job.conn.FormURL(job.itemtype, id), "glpi_conn": job.conn.Key, "glpi_type": job.itemtype})
			for _, t := range job.team {
				mid, e := cl.Resolve(ctx, t.typ, t.value)
				if e == nil {
					e = cl.AddTeam(ctx, job.itemtype, id, t.typ, mid, t.role)
				}
				if e != nil {
					warns = append(warns, fmt.Sprintf("Team %s %s: %v", t.role, t.value, e))
				}
			}
			for i, tb := range job.tasks {
				for field, v := range job.taskRefs[i] {
					if v == "" {
						continue
					}
					kind := map[string]string{"user_tech": "User", "group_tech": "Group", "category": "TaskCategory"}[field]
					if rid, e := cl.Resolve(ctx, kind, v); e == nil {
						tb[field] = map[string]any{"id": rid}
					} else {
						warns = append(warns, fmt.Sprintf("Aufgabe %d: %v", i+1, e))
					}
				}
				if _, e := cl.AddTimeline(ctx, job.itemtype, id, "Task", tb); e != nil {
					warns = append(warns, fmt.Sprintf("Aufgabe %d: %v", i+1, e))
				}
			}
			if v := job.valid; v != nil {
				rid, e := cl.Resolve(ctx, v["type"], v["approver"])
				if e == nil {
					_, e = cl.AddTimeline(ctx, job.itemtype, id, "Validation", map[string]any{"requested_approver_type": v["type"], "requested_approver_id": rid,
						"submission_comment": toHTML(v["comment"])})
				}
				if e != nil {
					warns = append(warns, "Genehmigung: "+e.Error())
				}
			}
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	in, r, env, ok := a.glpiReload(ctx, instID)
	if !ok {
		return
	}
	if err != nil {
		tries := int(glpi.ID(in.Outputs["glpi_tries"])) + 1
		in.Outputs["glpi_tries"] = tries
		in.Outputs["glpi_error"] = err.Error()
		if tries >= maxGLPITries || job.err != nil && strings.HasPrefix(job.err.Error(), "Vorlagenfehler") {
			a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "GLPI", Kind: "warnung",
				Msg: "Übergabe an GLPI gescheitert: " + job.step.Title, Detail: err.Error()})
			a.glpiFallback(ctx, r, env, in, &job.step, "Übergabe an GLPI gescheitert: "+err.Error())
			return
		}
		in.Outputs["glpi_next"] = time.Now().Add(glpiBackoff[tries]).Format(time.RFC3339)
		_ = a.Store.UpdateInstance(ctx, in)
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "GLPI", Kind: "warnung",
			Msg: fmt.Sprintf("GLPI-Übergabe fehlgeschlagen (Versuch %d von %d), neuer Versuch %s", tries, maxGLPITries, time.Now().Add(glpiBackoff[tries]).Format("15:04")), Detail: err.Error()})
		a.changed(r.ID)
		return
	}
	delete(in.Outputs, "glpi_error")
	delete(in.Outputs, "glpi_next")
	if len(warns) > 0 {
		in.Outputs["glpi_warn"] = strings.Join(warns, "\n")
	}
	if job.followup != nil {
		in.Outputs["glpi_id"], in.Outputs["glpi_type"], in.Outputs["glpi_url"] = id, job.itemtype, job.conn.FormURL(job.itemtype, id)
		a.glpiComplete(ctx, r, env, in, "GLPI", "Folgeeintrag an "+job.itemtype+" #"+strconv.FormatInt(id, 10)+" geschrieben")
		return
	}
	label := fmt.Sprintf("%s #%d", job.itemtype, id)
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "GLPI", Kind: "glpi",
		Msg: "An GLPI übergeben: " + label + " · " + job.step.Title, Detail: strings.Join(warns, "\n")})
	if job.g.Wait == "none" {
		in.Outputs["glpi_status"], in.Outputs["glpi_status_name"] = 1, glpi.StatusName[job.itemtype][1]
		a.glpiComplete(ctx, r, env, in, "GLPI", label+" angelegt")
		return
	}
	in.Outputs["glpi_phase"] = "offen"
	in.Outputs["glpi_status"], in.Outputs["glpi_status_name"] = 1, glpi.StatusName[job.itemtype][1]
	in.Outputs["glpi_checked"] = time.Now().Format(time.RFC3339)
	in.Assignee.Label = "GLPI " + label
	_ = a.Store.UpdateInstance(ctx, in)
	a.changed(r.ID)
}

func (a *App) glpiSave(ctx context.Context, instID int64, kv map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	in, err := a.Store.Instance(ctx, instID)
	if err != nil {
		return
	}
	for k, v := range kv {
		in.Outputs[k] = v
	}
	_ = a.Store.UpdateInstance(ctx, in)
}

func (a *App) glpiReload(ctx context.Context, instID int64) (*store.Instance, *store.Request, *flow.Env, bool) {
	in, err := a.Store.Instance(ctx, instID)
	if err != nil || in.State != "extern" {
		return nil, nil, nil, false
	}
	r, env, _, err := a.Load(ctx, in.RequestID)
	if err != nil || (r.Status != "laufend" && r.Status != "rueckfrage") {
		return nil, nil, nil, false
	}
	if in.Outputs == nil {
		in.Outputs = map[string]any{}
	}
	return in, r, env, true
}

// glpiPoll fragt den Status ab (ohne Sperre) und übernimmt Lösung und Rückmeldefelder.
func (a *App) glpiPoll(ctx context.Context, instID int64, job *glpiJob) {
	if job.err != nil || job.id == 0 {
		return
	}
	cl := glpi.For(job.conn)
	item, err := cl.Get(ctx, job.itemtype, job.id)
	var texts []string
	st, stName := 0, ""
	if err == nil {
		st, stName = glpi.ItemStatus(job.itemtype, item)
		if containsInt(doneStatus(&job.g, job.itemtype), st) || containsInt(failStatus(&job.g, job.itemtype), st) {
			if sol, e := cl.Timeline(ctx, job.itemtype, job.id, "Solution"); e == nil {
				texts = append(texts, newestFirst(sol)...)
			}
			if fu, e := cl.Timeline(ctx, job.itemtype, job.id, "Followup"); e == nil {
				texts = append(texts, newestFirst(fu)...)
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	in, r, env, ok := a.glpiReload(ctx, instID)
	if !ok {
		return
	}
	in.Outputs["glpi_checked"] = time.Now().Format(time.RFC3339)
	if err != nil {
		in.Outputs["glpi_error"] = err.Error()
		_ = a.Store.UpdateInstance(ctx, in)
		return
	}
	delete(in.Outputs, "glpi_error")
	prev := glpi.ID(in.Outputs["glpi_status"])
	in.Outputs["glpi_status"], in.Outputs["glpi_status_name"] = st, stName
	label := fmt.Sprintf("%s #%d", job.itemtype, job.id)
	if int64(st) != prev {
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "GLPI", Kind: "glpi",
			Msg: label + ": Status „" + stName + "“"})
	}
	switch {
	case containsInt(doneStatus(&job.g, job.itemtype), st):
		solution := ""
		if len(texts) > 0 {
			solution = texts[0]
		}
		in.Outputs["glpi_solution"] = solution
		for k, v := range extractOutputs(job.step.Outputs, texts) {
			in.Outputs[k] = v
		}
		in.Comment = solution
		a.glpiComplete(ctx, r, env, in, "GLPI · "+stName, label+" "+stName)
	case containsInt(failStatus(&job.g, job.itemtype), st):
		why := label + " in GLPI „" + stName + "“"
		if len(texts) > 0 {
			why += ": " + texts[0]
		}
		a.glpiFailed(ctx, r, env, in, &job.step, why)
	default:
		_ = a.Store.UpdateInstance(ctx, in)
		a.changed(r.ID)
	}
}

func doneStatus(g *flow.GLPIStep, t string) []int {
	if len(g.DoneStatus) > 0 {
		return g.DoneStatus
	}
	return glpi.DefaultDone[t]
}

func failStatus(g *flow.GLPIStep, t string) []int {
	if len(g.FailStatus) > 0 {
		return g.FailStatus
	}
	return glpi.DefaultFail[t]
}

// glpiComplete schließt den Schritt ab und führt den Vorgang weiter (unter Sperre).
func (a *App) glpiComplete(ctx context.Context, r *store.Request, env *flow.Env, in *store.Instance, by, msg string) {
	in.Outputs["glpi_phase"] = "fertig"
	in.State, in.Token = "erledigt", ""
	in.DoneAt = sql.NullTime{Time: time.Now(), Valid: true}
	in.DoneByName = by
	_ = a.Store.UpdateInstance(ctx, in)
	detail := in.Comment
	if s := env.Def.StepByID(in.StepID); s != nil {
		for _, of := range s.Outputs {
			if v := str(in.Outputs[of.Key]); v != "" {
				detail += fmt.Sprintf("\n%s: %s", of.Label, v)
			}
		}
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: by, Kind: "erledigt",
		Msg: "Erledigt: " + in.Title + " – " + msg, Detail: strings.TrimSpace(detail)})
	_ = a.advance(ctx, r, env)
}

// glpiFailed: Ticket/Change in GLPI gescheitert (abgelehnt, abgebrochen, Problem gemeldet).
func (a *App) glpiFailed(ctx context.Context, r *store.Request, env *flow.Env, in *store.Instance, s *flow.Step, why string) {
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "GLPI", Kind: "warnung", Msg: "GLPI meldet ein Problem: " + s.Title, Detail: why})
	switch s.GLPI.OnFail {
	case "ablehnen":
		now := sql.NullTime{Time: time.Now(), Valid: true}
		in.State, in.Token, in.DoneAt, in.DoneByName, in.Comment = "abgelehnt", "", now, "GLPI", why
		_ = a.Store.UpdateInstance(ctx, in)
		ins, _ := a.Store.Instances(ctx, r.ID)
		for _, o := range ins {
			if o.ID != in.ID && o.Active() {
				o.State, o.Token, o.Comment = "abgebrochen", "", "Vorgang abgelehnt"
				_ = a.Store.UpdateInstance(ctx, o)
			}
		}
		r.Status, r.ClosedAt = "abgelehnt", now
		_ = a.Store.UpdateRequest(ctx, r)
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "GLPI", Kind: "ablehnung", Msg: "Abgelehnt: " + in.Title, Detail: why})
		a.mailRequester(ctx, r, env, ins, "abgelehnt", in.Title+": "+why)
		a.changed(r.ID)
	case "weiter":
		in.Comment = why
		a.glpiComplete(ctx, r, env, in, "GLPI", "trotz Problem fortgesetzt")
	default:
		a.glpiFallback(ctx, r, env, in, s, why)
	}
}

// glpiFallback macht aus dem GLPI-Schritt eine Aufgabe für die Zuständigen.
func (a *App) glpiFallback(ctx context.Context, r *store.Request, env *flow.Env, in *store.Instance, s *flow.Step, why string) {
	ins, _ := a.Store.Instances(ctx, r.ID)
	c := a.Context(env, r, ins)
	in.Outputs["glpi_phase"] = "fehler"
	in.State, in.Comment = "offen", why
	in.Assignee = env.Resolve(s, c, nil)
	if in.Token == "" {
		in.Token = token()
	}
	_ = a.Store.UpdateInstance(ctx, in)
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "schritt",
		Msg: "Manuelle Bearbeitung nötig: " + s.Title + " → " + in.Assignee.Label, Detail: why})
	a.mailStep(ctx, r, env, c, s, in, nil)
	a.changed(r.ID)
}

// GLPIFeedback ist eine Rückmeldung über den Link/die API im Ticket.
type GLPIFeedback struct {
	Status  string         `json:"status"` // erledigt | problem
	Comment string         `json:"kommentar"`
	By      string         `json:"von"`
	Outputs map[string]any `json:"felder"`
}

// GLPIByToken liefert Schritt und Definition zum Rückmelde-Token (für die Rückmeldeseite).
func (a *App) GLPIByToken(ctx context.Context, tok string) (*store.Instance, *store.Request, *flow.Step, error) {
	in, err := a.Store.InstanceByToken(ctx, tok)
	if err != nil || in.Kind != "glpi" {
		return nil, nil, nil, errors.New("Link ungültig oder Schritt bereits erledigt")
	}
	r, env, _, err := a.Load(ctx, in.RequestID)
	if err != nil {
		return nil, nil, nil, err
	}
	s := env.Def.StepByID(in.StepID)
	if s == nil {
		return nil, nil, nil, errors.New("Schritt nicht mehr vorhanden")
	}
	return in, r, s, nil
}

// GLPIRespond übernimmt eine Rückmeldung aus GLPI (Link im Ticket oder API-Aufruf).
func (a *App) GLPIRespond(ctx context.Context, tok string, fb GLPIFeedback) error {
	a.mu.Lock()
	in, err := a.Store.InstanceByToken(ctx, tok)
	if err != nil || in.Kind != "glpi" || in.State != "extern" {
		a.mu.Unlock()
		return errors.New("Link ungültig oder Schritt bereits erledigt")
	}
	r, env, _, err := a.Load(ctx, in.RequestID)
	if err != nil || (r.Status != "laufend" && r.Status != "rueckfrage") {
		a.mu.Unlock()
		return errors.New("der Vorgang ist nicht mehr offen")
	}
	s := env.Def.StepByID(in.StepID)
	if s == nil || s.GLPI == nil {
		a.mu.Unlock()
		return errors.New("Schritt nicht mehr vorhanden")
	}
	by := strings.TrimSpace(fb.By)
	if by == "" {
		by = "Rückmeldung aus GLPI"
	} else {
		by += " (über GLPI)"
	}
	fb.Comment = strings.TrimSpace(fb.Comment)
	switch fb.Status {
	case "problem", "fehler", "abgelehnt":
		if fb.Comment == "" {
			a.mu.Unlock()
			return errors.New("Bitte beschreiben, was nicht geklappt hat.")
		}
		a.glpiFailed(ctx, r, env, in, s, by+": "+fb.Comment)
	default:
		for _, of := range s.Outputs {
			v := fb.Outputs[of.Key]
			if of.Required && empty(v) {
				a.mu.Unlock()
				return fmt.Errorf("„%s“ ist eine Pflichtangabe", of.Label)
			}
			if !empty(v) {
				in.Outputs[of.Key] = v
			}
		}
		in.Comment = fb.Comment
		a.glpiComplete(ctx, r, env, in, by, "Rückmeldung erhalten")
	}
	a.mu.Unlock()
	a.afterUnlock(ctx)
	return nil
}

// GLPIAction: Bedienung auf der Vorgangsseite (abfragen, erneut senden, manuell übernehmen).
func (a *App) GLPIAction(ctx context.Context, instID int64, action string, ac Actor) error {
	a.mu.Lock()
	in, err := a.Store.Instance(ctx, instID)
	if err != nil || in.Kind != "glpi" {
		a.mu.Unlock()
		return errors.New("kein GLPI-Schritt")
	}
	r, env, ins, err := a.Load(ctx, in.RequestID)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	s := env.Def.StepByID(in.StepID)
	allowed := ac.Role == "admin" || IsAssignee(in, ac)
	if s != nil && !allowed {
		c := a.Context(env, r, ins)
		allowed = IsAssignee(&store.Instance{Assignee: env.Resolve(s, c, nil)}, ac)
	}
	if !allowed {
		a.mu.Unlock()
		return ErrForbidden
	}
	if r.Status != "laufend" && r.Status != "rueckfrage" {
		a.mu.Unlock()
		return errors.New("der Vorgang ist nicht mehr offen")
	}
	if in.Outputs == nil {
		in.Outputs = map[string]any{}
	}
	switch action {
	case "abfragen":
		if in.State != "extern" {
			a.mu.Unlock()
			return errors.New("Schritt wartet nicht auf GLPI")
		}
	case "erneut": // (wieder) an GLPI übergeben – nach Fehler oder manueller Übernahme
		if in.State != "offen" && in.State != "extern" {
			a.mu.Unlock()
			return errors.New("Schritt ist nicht offen")
		}
		phase := "senden"
		if glpi.ID(in.Outputs["glpi_id"]) > 0 && str(in.Outputs["glpi_error"]) == "" && in.Outputs["glpi_phase"] != "fehler" {
			phase = "offen" // Ticket existiert schon: nur weiter beobachten
		}
		if in.Outputs["glpi_phase"] == "fehler" && glpi.ID(in.Outputs["glpi_id"]) > 0 {
			phase = "offen"
		}
		in.Outputs["glpi_phase"], in.Outputs["glpi_tries"] = phase, 0
		delete(in.Outputs, "glpi_next")
		in.State, in.Comment = "extern", ""
		if in.Token == "" {
			in.Token = token()
		}
		_ = a.Store.UpdateInstance(ctx, in)
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: ac.User, ActorName: ac.Label(), Kind: "glpi",
			Msg: "GLPI-Schritt erneut angestoßen: " + in.Title})
	case "manuell":
		if in.State != "extern" || s == nil {
			a.mu.Unlock()
			return errors.New("Schritt wartet nicht auf GLPI")
		}
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: ac.User, ActorName: ac.Label(), Kind: "glpi",
			Msg: "GLPI-Schritt manuell übernommen: " + in.Title})
		a.glpiFallback(ctx, r, env, in, s, "Manuell übernommen von "+ac.Label())
		a.mu.Unlock()
		return nil
	default:
		a.mu.Unlock()
		return errors.New("unbekannte Aktion")
	}
	a.mu.Unlock()
	a.GLPIProcess(ctx, instID)
	return nil
}

// GLPIWebhook stößt die Abfrage der betroffenen Schritte an (Inhalt der Nachricht wird nicht übernommen,
// maßgeblich ist immer der Stand per API).
func (a *App) GLPIWebhook(ctx context.Context, connKey string, payload map[string]any, raw []byte) int {
	var ids []int64
	typ := ""
	collect := func(m map[string]any) {
		if m == nil {
			return
		}
		if t := str(m["itemtype"]); t != "" && glpi.ID(m["items_id"]) > 0 {
			ids, typ = append(ids, glpi.ID(m["items_id"])), t
		}
		ids = append(ids, glpi.ID(m["id"]))
	}
	item, _ := payload["item"].(map[string]any)
	parent, _ := payload["parent_item"].(map[string]any)
	collect(parent)
	collect(item)
	toks := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?:glpi/r/|AW:)([A-Za-z0-9_-]{32})`).FindAllStringSubmatch(string(raw), -1) {
		toks[m[1]] = true
	}
	ext, _ := a.Store.ExternInstances(ctx)
	n := 0
	for _, in := range ext {
		if in.Kind != "glpi" {
			continue
		}
		hit := toks[in.Token]
		if !hit && (connKey == "" || str(in.Outputs["glpi_conn"]) == connKey || str(in.Outputs["glpi_conn"]) == "") {
			id := glpi.ID(in.Outputs["glpi_id"])
			for _, x := range ids {
				if x > 0 && x == id && (typ == "" || strings.EqualFold(typ, str(in.Outputs["glpi_type"]))) {
					hit = true
				}
			}
		}
		if hit {
			a.GLPIProcess(ctx, in.ID)
			n++
		}
	}
	return n
}

// GLPITick: fällige Übergaben wiederholen, offene Tickets im eingestellten Abstand abfragen.
func (a *App) GLPITick(ctx context.Context) {
	ext, err := a.Store.ExternInstances(ctx)
	if err != nil {
		return
	}
	conns := map[string]glpi.Conn{}
	for _, c := range a.GLPIConns(ctx) {
		conns[c.Key] = c
	}
	for _, in := range ext {
		if in.Kind != "glpi" {
			continue
		}
		switch str(in.Outputs["glpi_phase"]) {
		case "senden":
			a.GLPIProcess(ctx, in.ID)
		case "offen":
			c, ok := conns[str(in.Outputs["glpi_conn"])]
			if !ok {
				for _, x := range conns {
					c = x
					break
				}
			}
			every := c.PollMinutes
			if every == 0 && c.WebhookSecret == "" {
				every = 5
			}
			if every <= 0 {
				every = 60 // nur Webhook: trotzdem stündlich nachsehen
			}
			last, _ := time.Parse(time.RFC3339, str(in.Outputs["glpi_checked"]))
			if time.Since(last) >= time.Duration(every)*time.Minute-10*time.Second {
				a.GLPIProcess(ctx, in.ID)
			}
		}
	}
}

// ---------- Hilfen ----------

var (
	urlRe   = regexp.MustCompile(`https?://[^\s<>"]+`)
	tagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	brRe    = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>`)
	spaceRe = regexp.MustCompile(`[ \t]+`)
)

// toHTML: Text (Zeilen, Links) → HTML; vorhandenes HTML bleibt.
func toHTML(s string) string {
	t := strings.TrimSpace(s)
	if t == "" || strings.HasPrefix(t, "<") {
		return t
	}
	var b strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(t, "\r\n", "\n"), "\n\n") {
		lines := strings.Split(strings.TrimSpace(para), "\n")
		for i, l := range lines {
			e := html.EscapeString(l)
			e = urlRe.ReplaceAllStringFunc(e, func(u string) string { return `<a href="` + u + `">` + u + `</a>` })
			lines[i] = e
		}
		b.WriteString("<p>" + strings.Join(lines, "<br>") + "</p>")
	}
	return b.String()
}

// PlainText: HTML aus GLPI → Text.
func PlainText(s string) string {
	s = html.UnescapeString(brRe.ReplaceAllString(html.UnescapeString(s), "\n"))
	s = tagRe.ReplaceAllString(s, "")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, strings.TrimSpace(spaceRe.ReplaceAllString(l, " ")))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func newestFirst(list []map[string]any) []string {
	sort.SliceStable(list, func(i, j int) bool { return glpi.ID(list[i]["id"]) > glpi.ID(list[j]["id"]) })
	var out []string
	for _, e := range list {
		if t := PlainText(str(e["content"])); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// extractOutputs liest „Bezeichnung: Wert“ bzw. „schluessel = Wert“ aus Lösung/Folgeeinträgen (neueste zuerst).
func extractOutputs(fields []flow.Field, texts []string) map[string]any {
	out := map[string]any{}
	for _, f := range fields {
		names := []string{regexp.QuoteMeta(f.Key)}
		if f.Label != "" {
			names = append(names, regexp.QuoteMeta(f.Label))
		}
		re := regexp.MustCompile(`(?im)^\s*(?:` + strings.Join(names, "|") + `)\s*[:=]\s*(.+?)\s*$`)
		for _, t := range texts {
			if m := re.FindStringSubmatch(t); m != nil {
				out[f.Key] = m[1]
				break
			}
		}
	}
	return out
}

func glpiDate(v string) string {
	v = strings.TrimSpace(v)
	for _, f := range []string{"2006-01-02", "02.01.2006"} {
		if t, err := time.ParseInLocation(f, v, time.Local); err == nil {
			return t.Format("2006-01-02") + " 23:59:00"
		}
	}
	for _, f := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "02.01.2006 15:04", time.RFC3339} {
		if t, err := time.ParseInLocation(f, v, time.Local); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return v
}

func containsInt(l []int, x int) bool {
	for _, v := range l {
		if v == x {
			return true
		}
	}
	return false
}

func orStr(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

var _ = slog.Info

// GLPIPreview zeigt, was an GLPI geschickt würde (Designer): Textfelder gerendert, Namen noch nicht aufgelöst.
func (a *App) GLPIPreview(ctx context.Context, env *flow.Env, c, data map[string]any, s *flow.Step, item any) (map[string]any, []string) {
	in := &store.Instance{Item: -1, StepID: s.ID, Token: "beispiel-token-0000000000000000"}
	r := &store.Request{Number: str(mapGet(c, "antrag", "nummer")), ID: 42, Data: map[string]any{}}
	cc := withItem(c, item, in)
	cc["schritt"] = map[string]any{"titel": s.Title}
	cc["zusammenfassung"] = indent(Summary(env, data))
	cc["link"] = a.Cfg.BaseURL + "/a/beispiel-token"
	cc["glpi"] = a.glpiVars(r, in)
	job := &glpiJob{step: *s, g: *s.GLPI, itemtype: glpi.Itemtype(s.GLPI.Type)}
	job.conn, _ = a.GLPIConn(ctx, s.GLPI.Conn)
	var errs []string
	if s.GLPI.Action == "followup" {
		v, err := flow.Render(s.GLPI.Content, cc, env.Vars)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return map[string]any{"Followup": map[string]any{"content": toHTML(strings.TrimSpace(v)), "is_private": s.GLPI.Private}}, errs
	}
	errs = a.glpiBuild(ctx, env, c, cc, s, item, in, nil, job)
	out := map[string]any{}
	body := map[string]any{}
	for k, v := range job.body {
		body[k] = v
	}
	if refs, ok := body["__refs"].(map[string]string); ok {
		delete(body, "__refs")
		for k, v := range refs {
			if v != "" {
				body[k] = map[string]any{"id": "‹" + v + "›"}
			}
		}
	}
	out["POST /Assistance/"+job.itemtype] = body
	var team []map[string]any
	for _, t := range job.team {
		team = append(team, map[string]any{"role": t.role, "type": t.typ, "id": "‹" + t.value + "›"})
	}
	if len(team) > 0 {
		out["TeamMember"] = team
	}
	if len(job.tasks) > 0 {
		var tasks []map[string]any
		for i, t := range job.tasks {
			x := map[string]any{}
			for k, v := range t {
				x[k] = v
			}
			for k, v := range job.taskRefs[i] {
				if v != "" {
					x[k] = map[string]any{"id": "‹" + v + "›"}
				}
			}
			tasks = append(tasks, x)
		}
		out["Timeline/Task"] = tasks
	}
	if v := job.valid; v != nil {
		out["Timeline/Validation"] = map[string]any{"requested_approver_type": v["type"], "requested_approver_id": "‹" + v["approver"] + "›", "submission_comment": toHTML(v["comment"])}
	}
	if job.err != nil {
		errs = append(errs, job.err.Error())
	}
	return out, errs
}

func mapGet(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}
