package web

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"antragswerk/internal/app"
	"antragswerk/internal/flow"
	"antragswerk/internal/store"
	"antragswerk/internal/ui"
)

func randTok() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func pathID(r *http.Request, k string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(k), 10, 64)
	return n
}

func (s *Server) mayUse(se *store.Session, d *flow.Def) bool {
	if len(d.Audience) == 0 || se.Role == "admin" {
		return true
	}
	for _, a := range d.Audience {
		if contains(se.Stellen, a) {
			return true
		}
	}
	return false
}

// ---------- Antrag stellen ----------

type catGroup struct {
	Name  string
	Items []*store.Workflow
}

type neuData struct {
	Groups   []*catGroup
	Disabled []*store.Workflow
}

func (s *Server) newCatalog(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	wfs, _ := s.st.Workflows(r.Context(), se.Role == "admin")
	var groups []*catGroup
	var disabled []*store.Workflow
	idx := map[string]*catGroup{}
	for _, wf := range wfs {
		if wf.Active == nil || !s.mayUse(se, &wf.Active.Def) {
			continue
		}
		if wf.Archived {
			disabled = append(disabled, wf)
			continue
		}
		c := wf.Active.Def.Category
		if c == "" {
			c = "Weitere Anträge"
		}
		g := idx[c]
		if g == nil {
			g = &catGroup{Name: c}
			idx[c] = g
			groups = append(groups, g)
		}
		g.Items = append(g.Items, wf)
	}
	s.render(w, r, "neu", page{Title: "Neuer Antrag", Nav: "neu", Data: neuData{Groups: groups, Disabled: disabled}})
}

// formPayload ist alles, was form.js zum Rendern braucht.
type formPayload struct {
	Mode     string                   `json:"mode"` // new, edit
	Key      string                   `json:"key"`
	Def      flow.Def                 `json:"def"`
	Catalogs map[string]*flow.Catalog `json:"catalogs"`
	Data     map[string]any           `json:"data,omitempty"`
	Submit   string                   `json:"submit"`
	Question string                   `json:"question,omitempty"`
	ReqID    int64                    `json:"req_id,omitempty"`
	Number   string                   `json:"number,omitempty"`
	Ops      any                      `json:"ops"`
	Me       map[string]string        `json:"me"`
}

func (s *Server) catalogsFor(env *flow.Env, d *flow.Def) map[string]*flow.Catalog {
	out := map[string]*flow.Catalog{}
	for _, fr := range d.AllFields() {
		if fr.Field.Type == "catalog" {
			if c := env.Catalogs[fr.Field.Catalog]; c != nil {
				cc := *c
				var en []flow.Entry
				for _, e := range c.Entries {
					if !e.Inactive {
						en = append(en, e)
					}
				}
				cc.Entries = en
				out[c.Key] = &cc
			}
		}
	}
	return out
}

func (s *Server) newForm(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err != nil || wf.Active == nil {
		s.fail(w, r, 404, "Diesen Antrag gibt es nicht (mehr).")
		return
	}
	if wf.Archived {
		w.WriteHeader(http.StatusGone)
		s.render(w, r, "disabled", page{Title: wf.Active.Def.Name, Nav: "neu", Data: wf})
		return
	}
	if !s.mayUse(se, &wf.Active.Def) {
		s.fail(w, r, 403, "Diesen Antrag dürfen Sie nicht stellen.")
		return
	}
	env, _ := s.app.Env(r.Context(), &wf.Active.Def)
	p := formPayload{Mode: "new", Key: wf.Key, Def: wf.Active.Def, Catalogs: s.catalogsFor(env, &wf.Active.Def), Submit: ui.T("form.submit"), Ops: flow.Ops,
		Me: map[string]string{"sam": se.Username, "name": se.Display, "mail": se.Mail}}
	s.render(w, r, "form", page{Title: wf.Active.Def.Name, Nav: "neu", Wide: true, Data: p})
}

func decodeJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// normNumbers wandelt json.Number in Zeichenketten (Formularwerte sind Text).
func normNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case map[string]any:
		for k, y := range x {
			x[k] = normNumbers(y)
		}
	case []any:
		for i, y := range x {
			x[i] = normNumbers(y)
		}
	}
	return v
}

func (s *Server) apiSubmit(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err != nil || wf.Active == nil || !s.mayUse(se, &wf.Active.Def) {
		s.jsonErr(w, 404, "Antrag nicht verfügbar.")
		return
	}
	if wf.Archived {
		msg := "Dieser Antrag ist derzeit deaktiviert und kann nicht gestellt werden."
		if wf.DisabledNote != "" {
			msg += " " + wf.DisabledNote
		}
		s.jsonErr(w, http.StatusGone, msg)
		return
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, "Ungültige Daten.")
		return
	}
	normNumbers(body.Data)
	req, errs, err := s.app.Submit(r.Context(), wf, body.Data, s.actor(se))
	if len(errs) > 0 {
		s.jsonOut(w, 422, map[string]any{"errors": errs})
		return
	}
	if err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "antrag", Detail: req.Number + " " + req.Title})
	s.jsonOut(w, 200, map[string]any{"id": req.ID, "number": req.Number, "url": fmt.Sprintf("/v/%d?msg=gestellt", req.ID)})
}

func (s *Server) apiSimulate(w http.ResponseWriter, r *http.Request) {
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err != nil || wf.Active == nil {
		s.jsonErr(w, 404, "unbekannt")
		return
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, "Ungültige Daten.")
		return
	}
	normNumbers(body.Data)
	env, err := s.app.Env(r.Context(), &wf.Active.Def)
	if err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	se := sess(r)
	plan := env.Simulate(body.Data, map[string]any{"antragsteller": se.Username, "antragsteller_name": se.Display, "antragsteller_mail": se.Mail})
	s.jsonOut(w, 200, map[string]any{"plan": plan})
}

func (s *Server) apiUsers(w http.ResponseWriter, r *http.Request) {
	res, err := s.dir.Search(r.URL.Query().Get("q"), 15)
	if err != nil && len(res) == 0 {
		s.jsonErr(w, 502, "Verzeichnissuche fehlgeschlagen: "+err.Error())
		return
	}
	if res == nil {
		res = nil
	}
	s.jsonOut(w, 200, map[string]any{"results": res})
}

// ---------- Listen ----------

type listData struct {
	Items     []*store.Request
	Total     int
	F         store.RequestFilter
	Page      int
	Pages     int
	Workflows []*store.Workflow
	Scope     string
	Counts    map[string]int
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	q := r.URL.Query()
	pg, _ := strconv.Atoi(q.Get("p"))
	if pg < 1 {
		pg = 1
	}
	f := store.RequestFilter{Q: strings.TrimSpace(q.Get("q")), Status: q.Get("status"), WfKey: q.Get("wf"), Limit: 30, Offset: (pg - 1) * 30}
	scope := q.Get("scope")
	if !roleAtLeast(se.Role, "bearbeiter") {
		if scope == "" {
			scope = "beteiligt"
		}
	}
	switch scope {
	case "meine":
		f.Requester = se.Username
	case "beteiligt":
		f.VisibleTo = s.visibility(se)
	}
	if !q.Has("status") {
		f.Status = "offen"
	}
	items, total, err := s.st.Requests(r.Context(), f)
	if err != nil {
		s.fail(w, r, 500, err.Error())
		return
	}
	wfs, _ := s.st.Workflows(r.Context(), true)
	counts, _ := s.st.CountByStatus(r.Context())
	s.render(w, r, "list", page{Title: "Vorgänge", Nav: "list", Data: listData{Items: items, Total: total, F: f, Page: pg, Pages: (total + 29) / 30, Workflows: wfs, Scope: scope, Counts: counts}})
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	var items []*store.Instance
	all := r.URL.Query().Get("alle") == "1" && roleAtLeast(se.Role, "bearbeiter")
	if all {
		items, _ = s.st.Inbox(r.Context(), nil, 500)
	} else {
		items, _ = s.st.Inbox(r.Context(), s.visibility(se), 500)
	}
	s.render(w, r, "inbox", page{Title: "Meine Aufgaben", Nav: "inbox", Data: map[string]any{"Items": items, "All": all, "Stellen": se.Stellen}})
}

// ---------- Vorgangsakte ----------

type outField struct {
	flow.Field
	Value string
}

type card struct {
	Inst     *store.Instance
	ID       int64
	StepID   string
	Title    string
	Kind     string
	Item     string
	State    string
	Level    int
	Assignee string
	Due      string
	DueRel   string
	Overdue  bool
	CanAct   bool
	Mine     bool
	View     app.StepView
	Outputs  []outField
	After    []string
	Planned  bool
	Why      string
	ScriptsN int
	GLPI     *glpiCard
}

// glpiCard: Zustand eines GLPI-Schritts für die Anzeige.
type glpiCard struct {
	Ticket, URL, Status, Phase, Error, Warn, Checked, Next, Feedback, Conn string
	Tries                                                                  int
	CanCtl, Waiting                                                        bool
}

type viewRow struct {
	Label  string
	Value  string
	Multi  bool
	Head   []string
	Rows   [][]string
	Person map[string]any
}

type viewSection struct {
	Title string
	Icon  string
	Rows  []viewRow
}

type kv struct{ K, V string }

type detailData struct {
	Req         *store.Request
	Def         *flow.Def
	Color       string
	Levels      [][]*card
	Cards       []*card
	Sel         int64
	Journal     []store.JournalEntry
	Mails       []store.Mail
	Sections    []viewSection
	HasGLPI     bool
	Computed    []kv
	CanWithdraw bool
	CanEdit     bool
	Done, Total int
	Skipped     int
	Question    string
	Edges       []edge
	Staff       bool
}

type edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Server) canView(se *store.Session, r *store.Request, ins []*store.Instance) bool {
	if roleAtLeast(se.Role, "bearbeiter") || r.Requester == se.Username {
		return true
	}
	ac := s.actor(se)
	for _, in := range ins {
		if app.IsAssignee(in, ac) {
			return true
		}
	}
	return false
}

func viewSections(env *flow.Env, ctx map[string]any) []viewSection {
	var out []viewSection
	for _, sec := range env.Def.Form.Sections {
		vs := viewSection{Title: sec.Title, Icon: sec.Icon}
		for i := range sec.Fields {
			f := &sec.Fields[i]
			if f.Type == "info" {
				continue
			}
			v := ctx[f.Key]
			switch f.Type {
			case "group":
				lst, _ := v.([]any)
				if len(lst) == 0 {
					continue
				}
				row := viewRow{Label: f.Label, Multi: true}
				for _, c := range f.Fields {
					if c.Type != "info" {
						row.Head = append(row.Head, c.Label)
					}
				}
				for _, it := range lst {
					m, _ := it.(map[string]any)
					var cells []string
					for ci := range f.Fields {
						c := &f.Fields[ci]
						if c.Type == "info" {
							continue
						}
						cells = append(cells, app.ValueText(env, c, m[c.Key], m))
					}
					row.Rows = append(row.Rows, cells)
				}
				vs.Rows = append(vs.Rows, row)
			case "user":
				u, _ := v.(flow.UserVal)
				if u == nil || fmt.Sprint(u["name"]) == "" {
					continue
				}
				vs.Rows = append(vs.Rows, viewRow{Label: f.Label, Person: u})
			default:
				t := app.ValueText(env, f, v, ctx)
				if t == "" {
					continue
				}
				vs.Rows = append(vs.Rows, viewRow{Label: f.Label, Value: t})
			}
		}
		if len(vs.Rows) > 0 {
			out = append(out, vs)
		}
	}
	return out
}

func (s *Server) buildDetail(r *http.Request, req *store.Request, env *flow.Env, ins []*store.Instance, ac app.Actor) *detailData {
	ctx := s.app.Context(env, req, ins)
	d := &detailData{Req: req, Def: env.Def, Color: env.Def.Color}
	lv := env.Def.Levels()
	app.SortInstances(env, ins)
	seen := map[string]bool{}
	for _, in := range ins {
		seen[in.StepID] = true
		st := env.Def.StepByID(in.StepID)
		c := &card{Inst: in, ID: in.ID, StepID: in.StepID, Title: in.Title, Kind: in.Kind, Item: in.ItemLabel, State: in.State, Level: lv[in.StepID],
			Assignee: in.Assignee.Label, Overdue: in.Overdue(), CanAct: app.CanAct(in, ac), Mine: app.IsAssignee(in, ac) && in.State == "offen"}
		if in.Kind == "glpi" {
			c.GLPI = glpiCardFor(in, ac, req)
			d.HasGLPI = true
		}
		if in.DueAt.Valid {
			c.Due = in.DueAt.Time.Local().Format("02.01.2006 15:04")
			c.DueRel = relTime(in.DueAt.Time)
		}
		if st != nil {
			c.After = st.After
			if in.State != "uebersprungen" {
				c.View = s.app.RenderStep(env, ctx, st, in)
				c.ScriptsN = len(c.View.Scripts)
				cc := withItemCtx(ctx, c.View.Item)
				for _, of := range st.Outputs {
					val := ""
					if v, ok := in.Outputs[of.Key]; ok {
						val = fmt.Sprint(v)
					} else if in.State == "offen" && of.Default != "" {
						val, _ = flow.Render(of.Default, cc, env.Vars)
					}
					c.Outputs = append(c.Outputs, outField{Field: of, Value: strings.TrimSpace(val)})
				}
			}
		}
		if in.State == "uebersprungen" {
			d.Skipped++
		} else {
			d.Total++
			if in.Passed() {
				d.Done++
			}
		}
		if in.State == "rueckfrage" && d.Question == "" {
			d.Question = in.Comment
		}
		d.Cards = append(d.Cards, c)
	}
	// ausstehende Schritte (Vorhersage)
	if req.Status == "laufend" || req.Status == "rueckfrage" {
		for _, p := range env.Simulate(req.Data, s.app.Meta(req)) {
			if seen[p.StepID] || p.Skipped {
				continue
			}
			st := env.Def.StepByID(p.StepID)
			c := &card{StepID: p.StepID, Title: p.Title, Kind: p.Kind, Item: p.Item, State: "ausstehend", Level: lv[p.StepID], Assignee: p.Assignee.Label, Planned: true}
			if st != nil {
				c.After = st.After
				c.Why = st.Why
			}
			d.Total++
			d.Cards = append(d.Cards, c)
		}
	}
	max := 0
	for _, c := range d.Cards {
		if c.Level > max {
			max = c.Level
		}
	}
	d.Levels = make([][]*card, max+1)
	for _, c := range d.Cards {
		d.Levels[c.Level] = append(d.Levels[c.Level], c)
	}
	for _, st := range env.Def.Steps {
		for _, a := range st.After {
			d.Edges = append(d.Edges, edge{From: a, To: st.ID})
		}
	}
	d.Sections = viewSections(env, ctx)
	if c, ok := ctx["c"].(map[string]any); ok {
		for _, cp := range env.Def.Computed {
			if v := fmt.Sprint(c[cp.Key]); v != "" {
				d.Computed = append(d.Computed, kv{cp.Label, v})
			}
		}
	}
	return d
}

func withItemCtx(c map[string]any, it any) map[string]any {
	cc := make(map[string]any, len(c)+1)
	for k, v := range c {
		cc[k] = v
	}
	cc["item"] = it
	if it == nil {
		cc["item"] = map[string]any{}
	}
	return cc
}

func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	req, env, ins, err := s.app.Load(r.Context(), pathID(r, "id"))
	if err != nil {
		s.fail(w, r, 404, "Vorgang nicht gefunden.")
		return
	}
	if !s.canView(se, req, ins) {
		s.fail(w, r, 403, "Diesen Vorgang dürfen Sie nicht einsehen.")
		return
	}
	d := s.buildDetail(r, req, env, ins, s.actor(se))
	d.Staff = roleAtLeast(se.Role, "bearbeiter")
	d.Journal, _ = s.st.JournalFor(r.Context(), req.ID)
	d.Mails, _ = s.st.Mails(r.Context(), req.ID, 200)
	d.CanWithdraw = (req.Status == "laufend" || req.Status == "rueckfrage") && (req.Requester == se.Username || se.Role == "admin")
	d.CanEdit = req.Status == "rueckfrage" && (req.Requester == se.Username || se.Role == "admin")
	d.Sel, _ = strconv.ParseInt(r.URL.Query().Get("s"), 10, 64)
	if d.Sel == 0 { // erste eigene offene Aufgabe vorwählen
		for _, c := range d.Cards {
			if c.Mine {
				d.Sel = c.ID
				break
			}
		}
	}
	p := page{Title: req.Number + " · " + req.Title, Nav: "list", Wide: true, Data: d, Err: r.URL.Query().Get("err")}
	s.render(w, r, "detail", p)
}

func (s *Server) printView(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	req, env, ins, err := s.app.Load(r.Context(), pathID(r, "id"))
	if err != nil || !s.canView(se, req, ins) {
		s.fail(w, r, 404, "Vorgang nicht gefunden.")
		return
	}
	d := s.buildDetail(r, req, env, ins, s.actor(se))
	d.Journal, _ = s.st.JournalFor(r.Context(), req.ID)
	s.render(w, r, "print", page{Title: "Nachweis " + req.Number, Data: d})
}

func (s *Server) editForm(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	req, env, ins, err := s.app.Load(r.Context(), pathID(r, "id"))
	if err != nil {
		s.fail(w, r, 404, "Vorgang nicht gefunden.")
		return
	}
	if req.Status != "rueckfrage" || (req.Requester != se.Username && se.Role != "admin") {
		http.Redirect(w, r, fmt.Sprintf("/v/%d", req.ID), http.StatusSeeOther)
		return
	}
	q := ""
	for _, in := range ins {
		if in.State == "rueckfrage" {
			q += in.DoneByName + " (" + in.Title + "): " + in.Comment + "\n"
		}
	}
	p := formPayload{Mode: "edit", Key: req.WfKey, Def: *env.Def, Catalogs: s.catalogsFor(env, env.Def), Data: req.Data, Submit: ui.T("form.answer_submit"),
		Question: strings.TrimSpace(q), ReqID: req.ID, Number: req.Number, Ops: flow.Ops, Me: map[string]string{"sam": se.Username, "name": se.Display, "mail": se.Mail}}
	s.render(w, r, "form", page{Title: "Rückfrage " + req.Number, Nav: "list", Wide: true, Data: p})
}

func (s *Server) apiResubmit(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	req, err := s.st.Request(r.Context(), pathID(r, "id"))
	if err != nil || (req.Requester != se.Username && se.Role != "admin") {
		s.jsonErr(w, 404, "Vorgang nicht gefunden.")
		return
	}
	var body struct {
		Data   map[string]any `json:"data"`
		Answer string         `json:"answer"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, "Ungültige Daten.")
		return
	}
	normNumbers(body.Data)
	errs, err := s.app.Resubmit(r.Context(), req.ID, body.Data, body.Answer, s.actor(se))
	if len(errs) > 0 {
		s.jsonOut(w, 422, map[string]any{"errors": errs})
		return
	}
	if err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	s.jsonOut(w, 200, map[string]any{"url": fmt.Sprintf("/v/%d?msg=beantwortet", req.ID)})
}

func back(w http.ResponseWriter, r *http.Request, id int64, sel int64, msg string, err error) {
	u := fmt.Sprintf("/v/%d?", id)
	if sel > 0 {
		u += fmt.Sprintf("s=%d&", sel)
	}
	if err != nil {
		u += "err=" + urlEsc(err.Error())
	} else {
		u += "msg=" + msg
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

func (s *Server) note(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	id := pathID(r, "id")
	req, _, ins, err := s.app.Load(r.Context(), id)
	if err != nil || !s.canView(se, req, ins) {
		s.fail(w, r, 404, "Vorgang nicht gefunden.")
		return
	}
	err = s.app.Note(r.Context(), id, r.FormValue("text"), s.actor(se))
	back(w, r, id, 0, "notiz", err)
}

func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	id := pathID(r, "id")
	err := s.app.Withdraw(r.Context(), id, r.FormValue("reason"), s.actor(se))
	back(w, r, id, 0, "zurueckgezogen", err)
}

func actInput(r *http.Request) app.ActInput {
	_ = r.ParseForm()
	in := app.ActInput{Action: r.FormValue("action"), Comment: r.FormValue("comment"), Outputs: map[string]any{}}
	for k, v := range r.PostForm {
		if strings.HasPrefix(k, "out_") && len(v) > 0 {
			in.Outputs[strings.TrimPrefix(k, "out_")] = strings.TrimSpace(v[0])
		}
	}
	n, _ := strconv.Atoi(r.FormValue("checks_n"))
	for i := 0; i < n && i < 100; i++ {
		in.Checks = append(in.Checks, r.FormValue(fmt.Sprintf("check_%d", i)) != "")
	}
	return in
}

func actMsg(a string) string {
	return map[string]string{"erledigt": "erledigt", "freigeben": "freigegeben", "ablehnen": "abgelehnt", "rueckfrage": "rueckfrage"}[a]
}

func (s *Server) act(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	id, iid := pathID(r, "id"), pathID(r, "iid")
	inst, err := s.st.Instance(r.Context(), iid)
	if err != nil || inst.RequestID != id {
		s.fail(w, r, 404, "Schritt nicht gefunden.")
		return
	}
	in := actInput(r)
	err = s.app.Act(r.Context(), iid, in, s.actor(se))
	if errors.Is(err, app.ErrForbidden) {
		err = errors.New("Sie sind für diesen Schritt nicht zuständig.")
	}
	if err == nil {
		s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "schritt_" + in.Action, Detail: fmt.Sprintf("Vorgang %d · %s", id, inst.Title)})
	}
	back(w, r, id, iid, actMsg(in.Action), err)
}

func (s *Server) reassign(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	id, iid := pathID(r, "id"), pathID(r, "iid")
	err := s.app.Reassign(r.Context(), iid, strings.TrimSpace(r.FormValue("target")), r.FormValue("reason"), s.actor(se))
	back(w, r, id, iid, "weitergeleitet", err)
}

func (s *Server) downloadScript(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	req, env, ins, err := s.app.Load(r.Context(), pathID(r, "id"))
	if err != nil || !s.canView(se, req, ins) {
		s.fail(w, r, 404, "Vorgang nicht gefunden.")
		return
	}
	n, _ := strconv.Atoi(r.PathValue("n"))
	for _, in := range ins {
		if in.ID != pathID(r, "iid") {
			continue
		}
		st := env.Def.StepByID(in.StepID)
		if st == nil {
			break
		}
		v := s.app.RenderStep(env, s.app.Context(env, req, ins), st, in)
		if n < 0 || n >= len(v.Scripts) {
			break
		}
		sc := v.Scripts[n]
		ext := map[string]string{"powershell": "ps1", "cmd": "cmd", "bash": "sh"}[sc.Lang]
		if ext == "" {
			ext = "txt"
		}
		name := fmt.Sprintf("%s_%s_%d.%s", req.Number, in.StepID, n+1, ext)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		body := strings.ReplaceAll(sc.Code, "\n", "\r\n")
		if sc.Lang == "powershell" {
			_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM für Windows PowerShell 5.1
		}
		_, _ = w.Write([]byte(body + "\r\n"))
		return
	}
	s.fail(w, r, 404, "Skript nicht gefunden.")
}

// ---------- Mail-Links ----------

type tokenData struct {
	Card    *card
	Req     *store.Request
	Summary []viewSection
	Token   string
	Done    string
}

func (s *Server) tokenLink(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	inst, err := s.st.InstanceByToken(r.Context(), tok)
	se := s.current(r)
	if err != nil {
		w.WriteHeader(404)
		s.render(w, r, "token", page{Title: "Link nicht mehr gültig", Data: tokenData{Done: ui.T("token.invalid")}})
		return
	}
	if se != nil {
		http.Redirect(w, r, fmt.Sprintf("/v/%d?s=%d", inst.RequestID, inst.ID), http.StatusSeeOther)
		return
	}
	if !s.app.Cfg.TokenLinks || time.Since(inst.CreatedAt) > time.Duration(s.app.Cfg.TokenDays)*24*time.Hour {
		http.Redirect(w, r, "/login?next="+urlEsc(fmt.Sprintf("/v/%d?s=%d", inst.RequestID, inst.ID)), http.StatusSeeOther)
		return
	}
	req, env, ins, err := s.app.Load(r.Context(), inst.RequestID)
	if err != nil {
		s.fail(w, r, 404, "Vorgang nicht gefunden.")
		return
	}
	d := s.buildDetail(r, req, env, ins, app.Actor{Via: "mail"})
	var c *card
	for _, x := range d.Cards {
		if x.ID == inst.ID {
			c = x
		}
	}
	s.render(w, r, "token", page{Title: inst.Title, Data: tokenData{Card: c, Req: req, Summary: d.Sections, Token: tok}, Msg: flashMsg(r.URL.Query().Get("msg")), Err: r.URL.Query().Get("err")})
}

func (s *Server) tokenAct(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	inst, err := s.st.InstanceByToken(r.Context(), tok)
	if err != nil || !s.app.Cfg.TokenLinks {
		http.Redirect(w, r, "/a/"+urlEsc(tok), http.StatusSeeOther)
		return
	}
	in := actInput(r)
	label := strings.Join(inst.Assignee.Emails, ", ")
	if label == "" {
		label = inst.Assignee.Label
	}
	err = s.app.Act(r.Context(), inst.ID, in, app.Actor{User: "mail-link", Name: inst.Assignee.Label, Mail: label, Via: "mail"})
	s.st.Audit(r.Context(), store.AuditEntry{Actor: "mail-link:" + label, IP: s.ip(r), Action: "schritt_" + in.Action, Detail: fmt.Sprintf("Vorgang %d · %s", inst.RequestID, inst.Title)})
	if err != nil {
		http.Redirect(w, r, "/a/"+urlEsc(tok)+"?err="+urlEsc(err.Error()), http.StatusSeeOther)
		return
	}
	msg := map[string]string{"erledigt": ui.T("flash.token_done"), "freigeben": ui.T("flash.token_approved"),
		"ablehnen": ui.T("flash.token_rejected"), "rueckfrage": ui.T("flash.token_question")}[in.Action]
	s.render(w, r, "token", page{Title: "Erledigt", Data: tokenData{Done: msg}})
}

// sortWorkflows nach Kategorie und Name.
func sortWorkflows(wfs []*store.Workflow) {
	sort.SliceStable(wfs, func(i, j int) bool { return wfs[i].Sort < wfs[j].Sort })
}

func glpiCardFor(in *store.Instance, ac app.Actor, req *store.Request) *glpiCard {
	o := in.Outputs
	g := &glpiCard{Phase: fmt.Sprint(o["glpi_phase"]), Status: strOf(o["glpi_status_name"]), URL: strOf(o["glpi_url"]), Error: strOf(o["glpi_error"]),
		Warn: strOf(o["glpi_warn"]), Conn: strOf(o["glpi_conn"]), Waiting: in.State == "extern"}
	if id := strOf(o["glpi_id"]); id != "" && id != "0" {
		g.Ticket = strOf(o["glpi_type"]) + " #" + id
	}
	if n, ok := o["glpi_tries"].(float64); ok {
		g.Tries = int(n)
	}
	if t, err := time.Parse(time.RFC3339, strOf(o["glpi_checked"])); err == nil {
		g.Checked = relTime(t)
	}
	if t, err := time.Parse(time.RFC3339, strOf(o["glpi_next"])); err == nil && g.Waiting {
		g.Next = t.Local().Format("15:04")
	}
	if in.State == "extern" && in.Token != "" && ac.Via != "mail" {
		g.Feedback = "/glpi/r/" + in.Token
	}
	open := in.State == "extern" || in.State == "offen"
	g.CanCtl = open && ac.Via != "mail" && (req.Status == "laufend" || req.Status == "rueckfrage") && (ac.Role == "admin" || app.IsAssignee(in, ac))
	return g
}

func strOf(v any) string {
	if v == nil {
		return ""
	}
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}
