// Package app steuert die Vorgänge: Antrag stellen, Schritte aktivieren, Freigaben, Rückfragen, Mails.
package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"antragswerk/internal/config"
	"antragswerk/internal/flow"
	"antragswerk/internal/mail"
	"antragswerk/internal/store"
	"antragswerk/internal/ui"
)

type App struct {
	Cfg      *config.Config
	Store    *store.Store
	Mailer   *mail.Mailer
	mu       sync.Mutex
	OnChange func(reqID int64) // Live-Hinweis an Browser
	GLPISync bool              // GLPI-Übergaben sofort im Aufrufer ausführen (Tests); sonst im Hintergrund

	glpiPending []int64 // frisch angelegte GLPI-Schritte (unter a.mu)
}

// afterUnlock startet GLPI-Übergaben, die während einer Aktion entstanden sind (außerhalb der Sperre).
func (a *App) afterUnlock(ctx context.Context) {
	if a.GLPISync {
		a.GLPIKick(ctx)
		return
	}
	go a.GLPIKick(context.Background())
}

// Actor ist die handelnde Person.
type Actor struct {
	User    string
	Name    string
	Mail    string
	Role    string
	Stellen []string
	Via     string // "mail" bei Mail-Link ohne Anmeldung
}

func (a Actor) Label() string {
	if a.Name != "" {
		return a.Name
	}
	return a.User
}

func token() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Env lädt Kataloge, Stellen und Variablen für eine Definition.
func (a *App) Env(ctx context.Context, d *flow.Def) (*flow.Env, error) {
	cats, err := a.Store.Catalogs(ctx)
	if err != nil {
		return nil, err
	}
	sts, err := a.Store.Stellen(ctx)
	if err != nil {
		return nil, err
	}
	vars, err := a.Store.Vars(ctx)
	if err != nil {
		return nil, err
	}
	e := &flow.Env{Def: d, Catalogs: map[string]*flow.Catalog{}, Stellen: map[string]*flow.Stelle{}, Vars: map[string]string{}}
	for i := range cats {
		e.Catalogs[cats[i].Key] = &cats[i]
	}
	for i := range sts {
		e.Stellen[sts[i].Key] = &sts[i]
	}
	for _, v := range vars {
		e.Vars[v.Key] = v.Value
	}
	return e, nil
}

// Meta liefert die Antragsdaten für Vorlagen.
func (a *App) Meta(r *store.Request) map[string]any {
	return map[string]any{
		"nummer": r.Number, "titel": r.Title, "ablauf": r.WfName,
		"antragsteller": r.Requester, "antragsteller_name": r.RequesterName, "antragsteller_mail": r.RequesterMail,
		"datum": r.CreatedAt.Format("02.01.2006"), "link": a.Cfg.BaseURL + fmt.Sprintf("/v/%d", r.ID),
	}
}

// ---------- Validierung der Eingaben ----------

// FieldError ist ein Eingabefehler.
type FieldError struct {
	Path string `json:"path"`
	Msg  string `json:"msg"`
}

var mailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// CleanAndValidate entfernt ausgeblendete Felder und prüft Pflichtfelder und Muster.
func CleanAndValidate(env *flow.Env, data map[string]any) (map[string]any, []FieldError) {
	clean := map[string]any{}
	var errs []FieldError
	ctx := env.Context(data, nil, nil)
	for _, s := range env.Def.Form.Sections {
		if !s.Show.Match(ctx, nil) {
			continue
		}
		for i := range s.Fields {
			f := &s.Fields[i]
			if f.Type == "info" || !f.Show.Match(ctx, nil) {
				continue
			}
			v, es := checkValue(env, f, data[f.Key], f.Key, ctx, nil)
			errs = append(errs, es...)
			if v != nil {
				clean[f.Key] = v
			}
		}
	}
	return clean, errs
}

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case bool:
		return !x
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0 || (x["sam"] == nil && x["name"] == nil && x["mail"] == nil)
	}
	return false
}

func checkValue(env *flow.Env, f *flow.Field, v any, path string, ctx map[string]any, item map[string]any) (any, []FieldError) {
	var errs []FieldError
	add := func(m string) { errs = append(errs, FieldError{path, m}) }
	if f.Type == "group" {
		items, _ := v.([]any)
		var out []any
		for idx, it := range items {
			m, _ := it.(map[string]any)
			if m == nil {
				continue
			}
			ictx := map[string]any{}
			for ci := range f.Fields {
				ictx[f.Fields[ci].Key] = m[f.Fields[ci].Key]
			}
			// Bedingungen innerhalb der Gruppe sehen den erweiterten Eintrag
			exp := map[string]any{}
			tmp := &flow.Env{Def: &flow.Def{Form: flow.Form{Sections: []flow.Section{{Fields: []flow.Field{*f}}}}}, Catalogs: env.Catalogs, Stellen: env.Stellen, Vars: env.Vars}
			if lst, ok := tmp.Context(map[string]any{f.Key: []any{m}}, nil, nil)[f.Key].([]any); ok && len(lst) == 1 {
				exp, _ = lst[0].(map[string]any)
			}
			nm := map[string]any{}
			for ci := range f.Fields {
				c := &f.Fields[ci]
				if c.Type == "info" || !c.Show.Match(ctx, exp) {
					continue
				}
				cv, es := checkValue(env, c, m[c.Key], fmt.Sprintf("%s.%d.%s", path, idx, c.Key), ctx, exp)
				errs = append(errs, es...)
				if cv != nil {
					nm[c.Key] = cv
				}
			}
			if len(nm) > 0 {
				out = append(out, nm)
			}
		}
		if f.Required && len(out) == 0 {
			add("Mindestens einen Eintrag hinzufügen.")
		}
		if f.Min > 0 && len(out) < f.Min {
			add(fmt.Sprintf("Mindestens %d Einträge erforderlich.", f.Min))
		}
		if f.Max > 0 && len(out) > f.Max {
			add(fmt.Sprintf("Höchstens %d Einträge möglich.", f.Max))
		}
		if out == nil {
			return nil, errs
		}
		return out, errs
	}
	if empty(v) {
		if f.Required {
			add("Pflichtfeld")
		}
		return nil, errs
	}
	switch f.Type {
	case "checkbox":
		return true, errs
	case "checkboxes":
		lst, _ := v.([]any)
		return lst, errs
	case "catalog":
		if f.Multi {
			lst, _ := v.([]any)
			return lst, errs
		}
		s := fmt.Sprint(v)
		if env.Lookup(f.Catalog, s) == nil {
			add("Unbekannter Katalogeintrag.")
		}
		return s, errs
	case "user":
		m, _ := v.(map[string]any)
		return m, errs
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	switch f.Type {
	case "email":
		if !mailRe.MatchString(s) {
			add("Keine gültige E-Mail-Adresse.")
		}
	case "date":
		if _, err := time.Parse("2006-01-02", s); err != nil {
			add("Ungültiges Datum.")
		}
	case "number":
		if !regexp.MustCompile(`^-?\d+([.,]\d+)?$`).MatchString(s) {
			add("Bitte eine Zahl eingeben.")
		}
	case "select", "radio":
		ok := false
		for _, o := range f.Options {
			ok = ok || o.Value == s
		}
		if !ok {
			add("Ungültige Auswahl.")
		}
	}
	if f.Pattern != "" {
		if re, err := regexp.Compile(f.Pattern); err == nil && !re.MatchString(s) {
			msg := f.PatternMsg
			if msg == "" {
				msg = "Eingabe entspricht nicht dem erwarteten Format."
			}
			add(msg)
		}
	}
	return s, errs
}

// ---------- Antrag stellen ----------

// Submit legt einen Vorgang an und startet den Ablauf.
func (a *App) Submit(ctx context.Context, w *store.Workflow, data map[string]any, by Actor) (*store.Request, []FieldError, error) {
	if w.Active == nil {
		return nil, nil, errors.New("Ablauf ist nicht veröffentlicht")
	}
	def := w.Active.Def
	env, err := a.Env(ctx, &def)
	if err != nil {
		return nil, nil, err
	}
	clean, errs := CleanAndValidate(env, data)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	r := &store.Request{WfID: w.ID, VersionID: w.Active.ID, WfKey: def.Key, WfName: def.Name, Status: "laufend", Data: clean,
		Requester: by.User, RequesterName: by.Label(), RequesterMail: by.Mail}
	r.Title = a.title(env, r)
	a.mu.Lock()
	defer a.afterUnlock(ctx)
	defer a.mu.Unlock()
	if err := a.Store.CreateRequest(ctx, r, def.Prefix); err != nil {
		return nil, nil, err
	}
	r.Title = a.title(env, r)
	_ = a.Store.UpdateRequest(ctx, r)
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, Actor: by.User, ActorName: by.Label(), Kind: "antrag",
		Msg: "Antrag gestellt: " + def.Name, Detail: Summary(env, clean)})
	if err := a.advance(ctx, r, env); err != nil {
		return r, nil, err
	}
	return r, nil, nil
}

func (a *App) title(env *flow.Env, r *store.Request) string {
	t := env.Def.Name
	if env.Def.TitleTpl != "" {
		if s, err := flow.Render(env.Def.TitleTpl, env.Context(r.Data, a.Meta(r), nil), env.Vars); err == nil && strings.TrimSpace(s) != "" {
			t = strings.TrimSpace(s)
		}
	}
	return t
}

// Summary beschreibt die Eingaben als Text (für Tagebuch und Mails).
func Summary(env *flow.Env, data map[string]any) string {
	ctx := env.Context(data, nil, nil)
	var b strings.Builder
	for _, s := range env.Def.Form.Sections {
		var lines []string
		for i := range s.Fields {
			f := &s.Fields[i]
			if f.Type == "info" {
				continue
			}
			if txt := ValueText(env, f, ctx[f.Key], ctx); txt != "" {
				if f.Type == "group" {
					lines = append(lines, f.Label+":\n"+txt)
				} else {
					lines = append(lines, f.Label+": "+txt)
				}
			}
		}
		if len(lines) > 0 {
			b.WriteString("— " + s.Title + " —\n" + strings.Join(lines, "\n") + "\n\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// ValueText formatiert einen erweiterten Wert lesbar.
func ValueText(env *flow.Env, f *flow.Field, v any, ctx map[string]any) string {
	switch f.Type {
	case "checkbox":
		if b, _ := v.(bool); b {
			return "Ja"
		}
		return ""
	case "select", "radio", "checkboxes":
		s, _ := ctx[f.Key+"_text"].(string)
		return s
	case "date":
		s, _ := v.(string)
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t.Format("02.01.2006")
		}
		return s
	case "user":
		u, _ := v.(flow.UserVal)
		if u == nil || u["name"] == "" {
			return ""
		}
		s := fmt.Sprint(u["name"])
		if m := fmt.Sprint(u["mail"]); m != "" {
			s += " <" + m + ">"
		}
		return s
	case "catalog":
		if lst, ok := v.([]any); ok {
			var p []string
			for _, x := range lst {
				p = append(p, fmt.Sprint(x))
			}
			return strings.Join(p, ", ")
		}
		if c, ok := v.(flow.CatVal); ok && c["key"] != "" {
			return c.String()
		}
		return ""
	case "group":
		lst, _ := v.([]any)
		var rows []string
		for i, it := range lst {
			m, _ := it.(map[string]any)
			var p []string
			for ci := range f.Fields {
				c := &f.Fields[ci]
				if t := ValueText(env, c, m[c.Key], m); t != "" {
					p = append(p, c.Label+": "+t)
				}
			}
			rows = append(rows, fmt.Sprintf("  %d. %s", i+1, strings.Join(p, " · ")))
		}
		return strings.Join(rows, "\n")
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// ---------- Ablaufsteuerung ----------

// Load lädt Vorgang, Definition, Umgebung und Instanzen.
func (a *App) Load(ctx context.Context, id int64) (*store.Request, *flow.Env, []*store.Instance, error) {
	r, err := a.Store.Request(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	v, err := a.Store.VersionByID(ctx, r.VersionID)
	if err != nil {
		return nil, nil, nil, err
	}
	env, err := a.Env(ctx, &v.Def)
	if err != nil {
		return nil, nil, nil, err
	}
	ins, err := a.Store.Instances(ctx, r.ID)
	return r, env, ins, err
}

// Outputs sammelt die Ergebnisse erledigter Schritte (.out.<schritt>.<feld>).
func Outputs(ins []*store.Instance) map[string]any {
	out := map[string]any{}
	for _, in := range ins {
		if in.State == "uebersprungen" || in.State == "abgebrochen" {
			continue
		}
		m, _ := out[in.StepID].(map[string]any)
		if m == nil {
			m = map[string]any{}
			out[in.StepID] = m
		}
		done := in.Passed() && in.State != "uebersprungen"
		if done && in.Item >= 0 { // "je Eintrag": Ergebnisse aller Einträge als Liste
			e := map[string]any{"eintrag": in.ItemLabel, "von": in.DoneByName, "kommentar": in.Comment}
			for k, v := range in.Outputs {
				e[k] = v
			}
			lst, _ := m["liste"].([]any)
			m["liste"] = append(lst, e)
		}
		if done {
			for k, v := range in.Outputs {
				if fmt.Sprint(v) != "" {
					m[k] = v
				}
			}
			m["erledigt"] = true
			m["von"] = in.DoneByName
			m["kommentar"] = in.Comment
			if in.DoneAt.Valid {
				m["am"] = in.DoneAt.Time.Format("02.01.2006 15:04")
			}
		}
	}
	return out
}

// Context liefert den vollständigen Vorlagenkontext eines Vorgangs.
func (a *App) Context(env *flow.Env, r *store.Request, ins []*store.Instance) map[string]any {
	return env.Context(r.Data, a.Meta(r), Outputs(ins))
}

func (a *App) advance(ctx context.Context, r *store.Request, env *flow.Env) error {
	if r.Status != "laufend" && r.Status != "rueckfrage" {
		return nil
	}
	order, err := env.Def.Order()
	if err != nil {
		return err
	}
	for round := 0; round < 50; round++ {
		ins, err := a.Store.Instances(ctx, r.ID)
		if err != nil {
			return err
		}
		byStep := map[string][]*store.Instance{}
		for _, in := range ins {
			byStep[in.StepID] = append(byStep[in.StepID], in)
		}
		c := a.Context(env, r, ins)
		changed := false
		for _, s := range order {
			if len(byStep[s.ID]) > 0 {
				continue
			}
			ready := true
			for _, p := range s.After {
				lst := byStep[p]
				if len(lst) == 0 {
					ready = false
					break
				}
				for _, in := range lst {
					if !in.Passed() {
						ready = false
					}
				}
			}
			if !ready {
				continue
			}
			changed = true
			if s.ForEach != "" {
				items := env.Items(c, s.ForEach)
				if len(items) == 0 {
					a.skip(ctx, r, s, -1, "", "keine Einträge in „"+env.FieldLabel(s.ForEach)+"“")
					continue
				}
				for i, it := range items {
					if !s.When.Match(c, it) {
						a.skip(ctx, r, s, i, flow.ItemLabel(it), "Bedingung nicht erfüllt: "+s.When.Text(env.FieldLabel))
						continue
					}
					if err := a.activate(ctx, r, env, c, s, i, it); err != nil {
						return err
					}
				}
				continue
			}
			if !s.When.Match(c, nil) {
				a.skip(ctx, r, s, -1, "", "Bedingung nicht erfüllt: "+s.When.Text(env.FieldLabel))
				continue
			}
			if err := a.activate(ctx, r, env, c, s, -1, nil); err != nil {
				return err
			}
		}
		if !changed {
			break
		}
	}
	// Abschluss?
	ins, err := a.Store.Instances(ctx, r.ID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	all := true
	for _, in := range ins {
		seen[in.StepID] = true
		if !in.Passed() {
			all = false
		}
	}
	for _, s := range env.Def.Steps {
		if !seen[s.ID] {
			all = false
		}
	}
	if all && r.Status == "laufend" {
		r.Status = "abgeschlossen"
		r.ClosedAt = sql.NullTime{Time: time.Now(), Valid: true}
		if err := a.Store.UpdateRequest(ctx, r); err != nil {
			return err
		}
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, Actor: "system", ActorName: "Antragswerk", Kind: "abschluss", Msg: "Vorgang abgeschlossen – alle Schritte erledigt."})
		a.mailRequester(ctx, r, env, ins, "abgeschlossen", "")
	}
	a.changed(r.ID)
	return nil
}

func (a *App) changed(id int64) {
	if a.OnChange != nil {
		a.OnChange(id)
	}
}

func (a *App) skip(ctx context.Context, r *store.Request, s *flow.Step, item int, label, why string) {
	in := &store.Instance{RequestID: r.ID, StepID: s.ID, Item: item, ItemLabel: label, Kind: s.Kind, Title: s.Title, State: "uebersprungen",
		Comment: why, DoneAt: sql.NullTime{Time: time.Now(), Valid: true}, DoneByName: "automatisch"}
	_ = a.Store.InsertInstance(ctx, in)
	msg := "Übersprungen: " + s.Title
	if label != "" {
		msg += " · " + label
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "uebersprungen", Msg: msg, Detail: why})
}

func (a *App) activate(ctx context.Context, r *store.Request, env *flow.Env, c map[string]any, s *flow.Step, item int, it any) error {
	in := &store.Instance{RequestID: r.ID, StepID: s.ID, Item: item, ItemLabel: flow.ItemLabel(it), Kind: s.Kind, Title: s.Title, State: "offen"}
	if s.Kind != "warten" {
		in.Assignee = env.Resolve(s, c, it)
	}
	if s.DueHours > 0 {
		in.DueAt = sql.NullTime{Time: time.Now().Add(time.Duration(s.DueHours) * time.Hour), Valid: true}
	}
	switch s.Kind {
	case "glpi":
		in.Assignee.Label = "GLPI"
		return a.activateGLPI(ctx, r, s, in)
	case "warten":
		ds, _ := c[s.WaitField].(string)
		t, err := time.ParseInLocation("2006-01-02", ds, time.Local)
		if err != nil {
			in.State, in.Comment = "versendet", "Kein Datum in „"+env.FieldLabel(s.WaitField)+"“ – sofort weiter."
			in.DoneAt = sql.NullTime{Time: time.Now(), Valid: true}
			in.Kind = "warten"
		} else {
			t = t.AddDate(0, 0, s.WaitOffset).Add(7 * time.Hour) // 07:00 Uhr am Stichtag
			in.State = "geplant"
			in.DueAt = sql.NullTime{Time: t, Valid: true}
			in.Assignee = flow.Assignee{Label: "Termin " + t.Format("02.01.2006")}
			if !time.Now().Before(t) {
				in.State, in.DoneAt, in.Comment = "versendet", sql.NullTime{Time: time.Now(), Valid: true}, "Stichtag bereits erreicht."
			}
		}
		if err := a.Store.InsertInstance(ctx, in); err != nil {
			return err
		}
		msg := "Termin: " + s.Title
		if in.State == "geplant" {
			msg += " – wartet bis " + in.DueAt.Time.Format("02.01.2006")
		}
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "termin", Msg: msg, Detail: in.Comment})
		return nil
	case "info":
		in.State = "versendet"
		in.DoneAt = sql.NullTime{Time: time.Now(), Valid: true}
		in.DoneByName = "automatisch"
		if err := a.Store.InsertInstance(ctx, in); err != nil {
			return err
		}
		a.mailStep(ctx, r, env, c, s, in, it)
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "mail",
			Msg: "Benachrichtigung: " + s.Title + " → " + in.Assignee.Label})
		return nil
	}
	in.Token = token()
	if err := a.Store.InsertInstance(ctx, in); err != nil {
		return err
	}
	msg := "Schritt bereit: " + s.Title
	if in.ItemLabel != "" {
		msg += " · " + in.ItemLabel
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "schritt",
		Msg: msg + " → " + in.Assignee.Label})
	a.mailStep(ctx, r, env, c, s, in, it)
	return nil
}

// ---------- Aktionen ----------

var ErrForbidden = errors.New("keine Berechtigung für diesen Schritt")

// CanAct prüft, ob eine Person den Schritt bearbeiten darf.
func CanAct(in *store.Instance, ac Actor) bool {
	if in.State != "offen" {
		return false
	}
	if ac.Via == "mail" {
		return true
	}
	if ac.Role == "admin" {
		return true
	}
	return IsAssignee(in, ac)
}

// IsAssignee: Person ist (direkt oder über Stelle) zuständig.
func IsAssignee(in *store.Instance, ac Actor) bool {
	for _, u := range in.Assignee.Users {
		if strings.EqualFold(u, ac.User) {
			return true
		}
	}
	for _, m := range in.Assignee.Emails {
		if ac.Mail != "" && strings.EqualFold(m, ac.Mail) {
			return true
		}
	}
	for _, st := range in.Assignee.Stellen {
		for _, my := range ac.Stellen {
			if st == my {
				return true
			}
		}
	}
	return false
}

// ActInput sind die Angaben zu einer Aktion.
type ActInput struct {
	Action  string         `json:"action"` // erledigt, freigeben, ablehnen, rueckfrage
	Comment string         `json:"comment"`
	Outputs map[string]any `json:"outputs"`
	Checks  []bool         `json:"checks"`
}

// Act führt eine Aktion auf einem Schritt aus.
func (a *App) Act(ctx context.Context, instID int64, in ActInput, ac Actor) error {
	a.mu.Lock()
	defer a.afterUnlock(ctx)
	defer a.mu.Unlock()
	inst, err := a.Store.Instance(ctx, instID)
	if err != nil {
		return err
	}
	r, env, ins, err := a.Load(ctx, inst.RequestID)
	if err != nil {
		return err
	}
	if !CanAct(inst, ac) {
		return ErrForbidden
	}
	if r.Status != "laufend" && r.Status != "rueckfrage" {
		return errors.New("der Vorgang ist nicht mehr offen")
	}
	step := env.Def.StepByID(inst.StepID)
	if step == nil {
		return errors.New("Schritt nicht mehr in der Definition")
	}
	in.Comment = strings.TrimSpace(in.Comment)
	via := ""
	if ac.Via == "mail" {
		via = " (per Mail-Link)"
	}
	now := sql.NullTime{Time: time.Now(), Valid: true}
	inst.Checks = in.Checks
	setDone := func(state string) {
		inst.State, inst.DoneAt, inst.DoneBy, inst.DoneByName, inst.Comment = state, now, ac.User, ac.Label()+via, in.Comment
		inst.Token = ""
	}
	label := inst.Title
	if inst.ItemLabel != "" {
		label += " · " + inst.ItemLabel
	}
	switch in.Action {
	case "erledigt":
		if step.Kind != "aufgabe" && step.Kind != "glpi" {
			return errors.New("Aktion passt nicht zur Schrittart")
		}
		outs := map[string]any{}
		for _, of := range step.Outputs {
			v := in.Outputs[of.Key]
			if of.Required && empty(v) {
				return fmt.Errorf("„%s“ ist ein Pflichtangabe", of.Label)
			}
			if !empty(v) {
				outs[of.Key] = v
			}
		}
		inst.Outputs = outs
		setDone("erledigt")
		detail := in.Comment
		for _, of := range step.Outputs {
			if v, ok := outs[of.Key]; ok {
				detail += fmt.Sprintf("\n%s: %v", of.Label, v)
			}
		}
		if err := a.Store.UpdateInstance(ctx, inst); err != nil {
			return err
		}
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: inst.ID, Actor: ac.User, ActorName: ac.Label() + via, Kind: "erledigt",
			Msg: "Erledigt: " + label, Detail: strings.TrimSpace(detail)})
	case "freigeben":
		if step.Kind != "freigabe" {
			return errors.New("Aktion passt nicht zur Schrittart")
		}
		setDone("freigegeben")
		if err := a.Store.UpdateInstance(ctx, inst); err != nil {
			return err
		}
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: inst.ID, Actor: ac.User, ActorName: ac.Label() + via, Kind: "freigabe",
			Msg: "Freigegeben: " + label, Detail: in.Comment})
	case "ablehnen":
		if step.Kind != "freigabe" {
			return errors.New("Aktion passt nicht zur Schrittart")
		}
		if in.Comment == "" {
			return errors.New("Bitte die Ablehnung begründen.")
		}
		setDone("abgelehnt")
		if err := a.Store.UpdateInstance(ctx, inst); err != nil {
			return err
		}
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: inst.ID, Actor: ac.User, ActorName: ac.Label() + via, Kind: "ablehnung",
			Msg: "Abgelehnt: " + label, Detail: in.Comment})
		for _, o := range ins {
			if o.ID != inst.ID && o.Active() {
				o.State, o.Token, o.Comment = "abgebrochen", "", "Vorgang abgelehnt"
				_ = a.Store.UpdateInstance(ctx, o)
			}
		}
		r.Status = "abgelehnt"
		r.ClosedAt = now
		if err := a.Store.UpdateRequest(ctx, r); err != nil {
			return err
		}
		a.mailRequester(ctx, r, env, ins, "abgelehnt", label+": "+in.Comment)
		a.changed(r.ID)
		return nil
	case "rueckfrage":
		if in.Comment == "" {
			return errors.New("Bitte die Rückfrage formulieren.")
		}
		inst.State, inst.Comment = "rueckfrage", in.Comment
		inst.DoneBy, inst.DoneByName = ac.User, ac.Label()+via
		if err := a.Store.UpdateInstance(ctx, inst); err != nil {
			return err
		}
		r.Status = "rueckfrage"
		if err := a.Store.UpdateRequest(ctx, r); err != nil {
			return err
		}
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: inst.ID, Actor: ac.User, ActorName: ac.Label() + via, Kind: "rueckfrage",
			Msg: "Rückfrage an Antragsteller/in: " + label, Detail: in.Comment})
		a.mailRequester(ctx, r, env, ins, "rueckfrage", in.Comment)
		a.changed(r.ID)
		return nil
	default:
		return errors.New("unbekannte Aktion")
	}
	return a.advance(ctx, r, env)
}

// Resubmit beantwortet eine Rückfrage mit geänderten Daten.
func (a *App) Resubmit(ctx context.Context, reqID int64, data map[string]any, answer string, ac Actor) ([]FieldError, error) {
	a.mu.Lock()
	defer a.afterUnlock(ctx)
	defer a.mu.Unlock()
	r, env, ins, err := a.Load(ctx, reqID)
	if err != nil {
		return nil, err
	}
	if r.Status != "rueckfrage" {
		return nil, errors.New("keine offene Rückfrage")
	}
	clean, errs := CleanAndValidate(env, data)
	if len(errs) > 0 {
		return errs, nil
	}
	diff := Diff(env, r.Data, clean)
	r.Data = clean
	r.Status = "laufend"
	r.Title = a.title(env, r)
	if err := a.Store.UpdateRequest(ctx, r); err != nil {
		return nil, err
	}
	detail := strings.TrimSpace(answer)
	if diff != "" {
		detail = strings.TrimSpace(detail + "\n\nGeänderte Angaben:\n" + diff)
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, Actor: ac.User, ActorName: ac.Label(), Kind: "korrektur", Msg: "Rückfrage beantwortet", Detail: detail})
	c := a.Context(env, r, ins)
	for _, in := range ins {
		if in.State != "rueckfrage" {
			continue
		}
		s := env.Def.StepByID(in.StepID)
		in.State, in.Token, in.DoneBy, in.DoneByName = "offen", token(), "", ""
		in.Comment = ""
		if err := a.Store.UpdateInstance(ctx, in); err != nil {
			return nil, err
		}
		if s != nil {
			var it any
			if s.ForEach != "" {
				if items := env.Items(c, s.ForEach); in.Item >= 0 && in.Item < len(items) {
					it = items[in.Item]
				}
			}
			a.mailStep(ctx, r, env, c, s, in, it)
		}
	}
	return nil, a.advance(ctx, r, env)
}

// Diff beschreibt Änderungen der Eingaben.
func Diff(env *flow.Env, old, nw map[string]any) string {
	oc, nc := env.Context(old, nil, nil), env.Context(nw, nil, nil)
	var lines []string
	for _, s := range env.Def.Form.Sections {
		for i := range s.Fields {
			f := &s.Fields[i]
			if f.Type == "info" {
				continue
			}
			o, n := ValueText(env, f, oc[f.Key], oc), ValueText(env, f, nc[f.Key], nc)
			if o != n {
				if f.Type == "group" {
					lines = append(lines, f.Label+": geändert\n"+n)
				} else {
					lines = append(lines, fmt.Sprintf("%s: „%s“ → „%s“", f.Label, o, n))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// Withdraw zieht einen Vorgang zurück.
func (a *App) Withdraw(ctx context.Context, reqID int64, reason string, ac Actor) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, _, ins, err := a.Load(ctx, reqID)
	if err != nil {
		return err
	}
	if r.Status != "laufend" && r.Status != "rueckfrage" {
		return errors.New("Vorgang ist bereits beendet")
	}
	if ac.Role != "admin" && ac.User != r.Requester {
		return ErrForbidden
	}
	for _, o := range ins {
		if o.Active() {
			o.State, o.Token, o.Comment = "abgebrochen", "", "Vorgang zurückgezogen"
			_ = a.Store.UpdateInstance(ctx, o)
		}
	}
	r.Status = "zurueckgezogen"
	r.ClosedAt = sql.NullTime{Time: time.Now(), Valid: true}
	if err := a.Store.UpdateRequest(ctx, r); err != nil {
		return err
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, Actor: ac.User, ActorName: ac.Label(), Kind: "zurueckgezogen", Msg: "Vorgang zurückgezogen", Detail: reason})
	a.changed(r.ID)
	return nil
}

// Note schreibt eine Notiz ins Verlaufsprotokoll.
func (a *App) Note(ctx context.Context, reqID int64, text string, ac Actor) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("Notiz ist leer")
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: reqID, Actor: ac.User, ActorName: ac.Label(), Kind: "notiz", Msg: "Notiz", Detail: text})
	a.changed(reqID)
	return nil
}

// Reassign übergibt einen offenen Schritt an eine andere Stelle oder Person (Vertretung).
func (a *App) Reassign(ctx context.Context, instID int64, target, reason string, ac Actor) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	inst, err := a.Store.Instance(ctx, instID)
	if err != nil {
		return err
	}
	if inst.State != "offen" {
		return errors.New("Schritt ist nicht offen")
	}
	if ac.Role != "admin" && !IsAssignee(inst, ac) {
		return ErrForbidden
	}
	r, env, ins, err := a.Load(ctx, inst.RequestID)
	if err != nil {
		return err
	}
	tmp := &flow.Step{Assign: flow.Assign{Type: "email", Email: target}}
	na := env.Resolve(tmp, nil, nil)
	if len(na.Emails) == 0 && len(na.Users) == 0 {
		return errors.New("Ziel unbekannt – Stelle, Mail-Adresse oder Anmeldename angeben")
	}
	old := inst.Assignee.Label
	inst.Assignee = na
	inst.Token = token()
	if err := a.Store.UpdateInstance(ctx, inst); err != nil {
		return err
	}
	a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: inst.ID, Actor: ac.User, ActorName: ac.Label(), Kind: "weiterleitung",
		Msg: fmt.Sprintf("Weitergeleitet: %s (%s → %s)", inst.Title, old, na.Label), Detail: reason})
	if s := env.Def.StepByID(inst.StepID); s != nil {
		c := a.Context(env, r, ins)
		var it any
		if s.ForEach != "" {
			if items := env.Items(c, s.ForEach); inst.Item >= 0 && inst.Item < len(items) {
				it = items[inst.Item]
			}
		}
		a.mailStep(ctx, r, env, c, s, inst, it)
	}
	a.changed(r.ID)
	return nil
}

// Tick verarbeitet Termine und Erinnerungen (minütlich).
func (a *App) Tick(ctx context.Context) {
	due, err := a.Store.DueInstances(ctx)
	if err != nil {
		slog.Error("tick", "err", err)
		return
	}
	for _, in := range due {
		func() {
			a.mu.Lock()
			defer a.afterUnlock(ctx)
			defer a.mu.Unlock()
			r, env, ins, err := a.Load(ctx, in.RequestID)
			if err != nil || (r.Status != "laufend" && r.Status != "rueckfrage") {
				return
			}
			if in.State == "geplant" {
				in.State, in.DoneAt, in.DoneByName = "versendet", sql.NullTime{Time: time.Now(), Valid: true}, "automatisch"
				_ = a.Store.UpdateInstance(ctx, in)
				a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "termin", Msg: "Stichtag erreicht: " + in.Title})
				_ = a.advance(ctx, r, env)
				return
			}
			in.Reminded = true
			_ = a.Store.UpdateInstance(ctx, in)
			if s := env.Def.StepByID(in.StepID); s != nil {
				c := a.Context(env, r, ins)
				cc := a.mailCtx(env, c, s, in, nil, r)
				cc["schritt"].(map[string]any)["frist"] = in.DueAt.Time.Format("02.01.2006 15:04")
				subject, body := a.mailText(env, "erinnerung", cc)
				a.sendMail(ctx, r.ID, in.ID, in.Assignee.Emails, subject, body, a.stepLink(in), c)
				a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "mail", Msg: "Erinnerung versendet: " + in.Title + " → " + in.Assignee.Label})
			}
		}()
	}
	a.GLPITick(ctx)
}

// RenderedScript ist ein Skript mit eingesetzten Antragsdaten.
type RenderedScript struct {
	Title string
	Lang  string
	Code  string
	Err   string
}

// StepView sind die aufbereiteten Inhalte eines Schritts.
type StepView struct {
	Instructions string
	Why          string
	Scripts      []RenderedScript
	Checklist    []string
	Item         any
}

// RenderStep setzt die Antragsdaten in Anleitung, Checkliste und Skripte ein.
func (a *App) RenderStep(env *flow.Env, c map[string]any, s *flow.Step, in *store.Instance) StepView {
	var it any
	if s.ForEach != "" {
		if items := env.Items(c, s.ForEach); in != nil && in.Item >= 0 && in.Item < len(items) {
			it = items[in.Item]
		}
	}
	if it == nil && s.ForEach != "" {
		it = env.EmptyItem(s.ForEach)
	}
	cc := withItem(c, it, in)
	v := StepView{Item: it}
	v.Instructions, _ = flow.Render(s.Instructions, cc, env.Vars)
	v.Why, _ = flow.Render(s.Why, cc, env.Vars)
	for _, cl := range s.Checklist {
		t, _ := flow.Render(cl, cc, env.Vars)
		v.Checklist = append(v.Checklist, t)
	}
	for _, sc := range s.Scripts {
		code, err := flow.Render(sc.Code, cc, env.Vars)
		rs := RenderedScript{Title: sc.Title, Lang: sc.Lang, Code: strings.TrimSpace(code)}
		if err != nil {
			rs.Err = err.Error()
		}
		v.Scripts = append(v.Scripts, rs)
	}
	return v
}

func withItem(c map[string]any, it any, in *store.Instance) map[string]any {
	cc := make(map[string]any, len(c)+3)
	for k, v := range c {
		cc[k] = v
	}
	if it != nil {
		cc["item"] = it
		if in != nil {
			cc["index"] = in.Item + 1
		}
	} else {
		cc["item"] = map[string]any{}
	}
	return cc
}

// SortInstances ordnet nach Ablaufebene.
func SortInstances(env *flow.Env, ins []*store.Instance) {
	lv := env.Def.Levels()
	pos := map[string]int{}
	for i, s := range env.Def.Steps {
		pos[s.ID] = i
	}
	sort.SliceStable(ins, func(i, j int) bool {
		a, b := ins[i], ins[j]
		if lv[a.StepID] != lv[b.StepID] {
			return lv[a.StepID] < lv[b.StepID]
		}
		if pos[a.StepID] != pos[b.StepID] {
			return pos[a.StepID] < pos[b.StepID]
		}
		return a.Item < b.Item
	})
}

// ---------- Mails ----------

func (a *App) stepLink(in *store.Instance) string {
	if in.Token != "" {
		return a.Cfg.BaseURL + "/a/" + in.Token
	}
	return a.Cfg.BaseURL + fmt.Sprintf("/v/%d?s=%d", in.RequestID, in.ID)
}

// mailCtx baut den Vorlagenkontext für Mails zu einem Schritt.
func (a *App) mailCtx(env *flow.Env, c map[string]any, s *flow.Step, in *store.Instance, it any, r *store.Request) map[string]any {
	cc := withItem(c, it, in)
	v := a.RenderStep(env, c, s, in)
	plain := func(x string) string { return strings.TrimSpace(strings.ReplaceAll(x, "**", "")) }
	cl := make([]any, 0, len(v.Checklist))
	for _, x := range v.Checklist {
		cl = append(cl, plain(x))
	}
	sc := make([]any, 0, len(v.Scripts))
	for _, x := range v.Scripts {
		sc = append(sc, x.Title)
	}
	frist := ""
	if in.DueAt.Valid {
		frist = in.DueAt.Time.Format("02.01.2006 15:04")
	}
	cc["schritt"] = map[string]any{"titel": s.Title, "eintrag": in.ItemLabel, "zustaendig": in.Assignee.Label, "frist": frist, "art": s.Kind,
		"anleitung": plain(v.Instructions), "warum": plain(v.Why), "checkliste": cl, "skripte": sc}
	cc["link"] = a.stepLink(in)
	cc["zusammenfassung"] = indent(Summary(env, r.Data))
	return cc
}

// mailText rendert Betreff und Text einer Standard-Mail aus den (anpassbaren) Textvorlagen.
func (a *App) mailText(env *flow.Env, kind string, cc map[string]any) (string, string) {
	subject, err := flow.Render(ui.T("mail."+kind+".betreff"), cc, env.Vars)
	if err != nil {
		subject = ui.T("mail." + kind + ".betreff")
	}
	body, err := flow.Render(ui.T("mail."+kind+".text"), cc, env.Vars)
	if err != nil {
		body += "\n\n(Vorlagenfehler: " + err.Error() + ")"
	}
	return strings.TrimSpace(subject), strings.TrimSpace(body) + "\n"
}

func (a *App) mailStep(ctx context.Context, r *store.Request, env *flow.Env, c map[string]any, s *flow.Step, in *store.Instance, it any) {
	if len(in.Assignee.Emails) == 0 {
		a.Store.Journal(ctx, store.JournalEntry{RequestID: r.ID, InstanceID: in.ID, Actor: "system", ActorName: "Antragswerk", Kind: "warnung",
			Msg: "Keine Mail-Adresse für „" + in.Assignee.Label + "“ – Schritt nur in „Meine Aufgaben“ sichtbar."})
		return
	}
	cc := a.mailCtx(env, c, s, in, it, r)
	link := cc["link"].(string)
	kind := s.Kind
	if kind != "freigabe" && kind != "info" {
		kind = "aufgabe"
	}
	subject, body := a.mailText(env, kind, cc)
	if s.Mail.Subject != "" {
		if x, err := flow.Render(s.Mail.Subject, cc, env.Vars); err == nil && strings.TrimSpace(x) != "" {
			subject = strings.TrimSpace(x)
		}
	}
	if s.Mail.Body != "" {
		if x, err := flow.Render(s.Mail.Body, cc, env.Vars); err == nil && strings.TrimSpace(x) != "" {
			body = x
		}
	}
	if s.Kind == "info" {
		link = ""
	}
	a.sendMail(ctx, r.ID, in.ID, in.Assignee.Emails, subject, body, link, cc)
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

func (a *App) mailRequester(ctx context.Context, r *store.Request, env *flow.Env, ins []*store.Instance, kind, text string) {
	if r.RequesterMail == "" {
		return
	}
	cc := a.Context(env, r, ins)
	link := a.Cfg.BaseURL + fmt.Sprintf("/v/%d", r.ID)
	if kind == "rueckfrage" {
		link = a.Cfg.BaseURL + fmt.Sprintf("/v/%d/bearbeiten", r.ID)
	}
	cc["link"] = link
	cc["begruendung"] = text
	subject, body := a.mailText(env, kind, cc)
	if kind == "abgeschlossen" {
		if env.Def.Done.Subject != "" {
			if x, err := flow.Render(env.Def.Done.Subject, cc, env.Vars); err == nil {
				subject = strings.TrimSpace(x)
			}
		}
		if env.Def.Done.Body != "" {
			if x, err := flow.Render(env.Def.Done.Body, cc, env.Vars); err == nil {
				body = x
			}
		}
	}
	a.sendMail(ctx, r.ID, 0, []string{r.RequesterMail}, subject, body, link, nil)
}

func (a *App) sendMail(ctx context.Context, reqID, instID int64, to []string, subject, body, link string, _ map[string]any) {
	to = uniq(to)
	if len(to) == 0 {
		return
	}
	m := &store.Mail{RequestID: reqID, InstanceID: instID, To: strings.Join(to, ", "), Subject: subject, Body: body, Link: link, Status: "protokolliert"}
	if a.Mailer.Enabled() {
		if err := a.Mailer.Send(to, subject, body, MailHTML(subject, body, link)); err != nil {
			m.Status, m.Error = "fehler", err.Error()
			slog.Warn("mail failed", "to", m.To, "err", err)
		} else {
			m.Status = "gesendet"
		}
	}
	_ = a.Store.InsertMail(ctx, m)
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// MailHTML erzeugt die HTML-Fassung einer Mail mit Schaltfläche (Farbe, Name und Texte aus den Einstellungen).
func MailHTML(subject, body, link string) string {
	st := ui.Get()
	accent := ui.AccentHex()
	org := st.AppName
	if st.Org != "" {
		org += " · " + st.Org
	}
	esc := html.EscapeString(body)
	if link != "" {
		esc = strings.ReplaceAll(esc, html.EscapeString(link), `<a href="`+html.EscapeString(link)+`" style="color:`+accent+`">`+html.EscapeString(link)+`</a>`)
	}
	btn := ""
	if link != "" {
		btn = `<p style="margin:24px 0"><a href="` + html.EscapeString(link) + `" style="background:` + accent + `;color:#fff;padding:11px 20px;border-radius:8px;text-decoration:none;font-weight:600;display:inline-block">` + html.EscapeString(ui.T("mail.button")) + `</a></p>`
	}
	return `<!doctype html><html><body style="margin:0;background:#f4f5f7;font-family:Segoe UI,Arial,sans-serif;color:#1b1f24">
<div style="max-width:640px;margin:0 auto;padding:24px">
<div style="font-size:13px;color:#5f6b7a;margin-bottom:8px">` + html.EscapeString(org) + `</div>
<div style="background:#fff;border:1px solid #dfe3e8;border-top:4px solid ` + accent + `;border-radius:10px;padding:24px">
<h1 style="font-size:18px;margin:0 0 16px">` + html.EscapeString(subject) + `</h1>
<div style="white-space:pre-wrap;font-size:14px;line-height:1.55">` + esc + `</div>` + btn + `
</div><div style="font-size:12px;color:#5f6b7a;margin-top:12px">` + html.EscapeString(ui.T("mail.footer")) + `</div></div></body></html>`
}
