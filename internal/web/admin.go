package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"antragswerk/internal/app"
	"antragswerk/internal/flow"
	"antragswerk/internal/glpi"
	"antragswerk/internal/orgsync"
	"antragswerk/internal/seed"
	"antragswerk/internal/store"
)

// ---------- Designer: Übersicht ----------

type designerListData struct {
	Items   []*store.Workflow
	Library []flow.Def
}

func (s *Server) designerList(w http.ResponseWriter, r *http.Request) {
	wfs, err := s.st.Workflows(r.Context(), true)
	if err != nil {
		s.fail(w, r, 500, err.Error())
		return
	}
	sortWorkflows(wfs)
	s.render(w, r, "designer_list", page{Title: "Ablauf-Designer", Nav: "designer", Data: designerListData{Items: wfs, Library: seed.Library()}})
}

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,40}$`)

func slug(s string) string {
	s = strings.ToLower(flow.ASCII(strings.ReplaceAll(s, " ", "-")))
	s = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(s, "")
	s = strings.Trim(s, "-_")
	if len(s) > 30 {
		s = s[:30]
	}
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		s = "ablauf-" + s
	}
	return s
}

func (s *Server) uniqueKey(r *http.Request, base string) string {
	k := slug(base)
	for i := 2; ; i++ {
		if _, err := s.st.Workflow(r.Context(), k); err != nil {
			return k
		}
		k = fmt.Sprintf("%s-%d", slug(base), i)
	}
}

func (s *Server) designerCreate(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Neuer Ablauf"
	}
	var d flow.Def
	if from := r.FormValue("from"); from != "" {
		if strings.HasPrefix(from, "lib:") {
			for _, l := range seed.Library() {
				if l.Key == strings.TrimPrefix(from, "lib:") {
					d = l
				}
			}
		} else if wf, err := s.st.Workflow(r.Context(), from); err == nil && wf.Current() != nil {
			d = wf.Current().Def
		}
	}
	if d.Name == "" && len(d.Steps) == 0 {
		d = flow.Def{Category: "Benutzer & Konten", Icon: "📝", Color: "#1f5fbf", Prefix: "AW",
			Form: flow.Form{Sections: []flow.Section{{ID: "s1", Title: "Angaben", Icon: "👤", Fields: []flow.Field{
				{Key: "person", Label: "Person", Type: "user", Required: true, Width: "half"},
				{Key: "begruendung", Label: "Begründung", Type: "textarea", Required: true},
			}}}},
			Steps: []flow.Step{{ID: "freigabe", Title: "Freigabe Vorgesetzte/r", Kind: "freigabe", After: []string{}, Assign: flow.Assign{Type: "stelle", Stelle: "it_benutzer"}, Why: "Bedarf bestätigen", X: 40, Y: 200}}}
	}
	d.Name = name
	d.Key = s.uniqueKey(r, name)
	if _, err := s.st.CreateWorkflow(r.Context(), d, se.Username, false, 0); err != nil {
		s.fail(w, r, 400, err.Error())
		return
	}
	s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "ablauf_neu", Detail: d.Key})
	http.Redirect(w, r, "/designer/"+d.Key, http.StatusSeeOther)
}

func (s *Server) designerCopy(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	r.Form.Set("from", r.PathValue("key"))
	s.designerCreate(w, r)
}

// ---------- Designer: Editor ----------

type designerPayload struct {
	Key          string              `json:"key"`
	Def          flow.Def            `json:"def"`
	Status       string              `json:"status"`
	Version      int                 `json:"version"`
	HasDraft     bool                `json:"has_draft"`
	HasActive    bool                `json:"has_active"`
	Archived     bool                `json:"archived"`
	DisabledNote string              `json:"disabled_note"`
	DisabledInfo string              `json:"disabled_info"`
	Requests     int                 `json:"requests"`
	Open         int                 `json:"open"`
	Versions     []versionInfo       `json:"versions"`
	Stellen      []flow.Stelle       `json:"stellen"`
	Catalogs     []flow.Catalog      `json:"catalogs"`
	Vars         []store.Var         `json:"vars"`
	Snippets     []flow.Snippet      `json:"snippets"`
	FieldTypes   any                 `json:"field_types"`
	StepKinds    any                 `json:"step_kinds"`
	Ops          any                 `json:"ops"`
	Issues       []flow.Issue        `json:"issues"`
	Readonly     bool                `json:"readonly"`
	Others       []map[string]string `json:"others"`
	GLPI         glpiDesigner        `json:"glpi"`
}

type glpiDesigner struct {
	Conns   []map[string]string `json:"conns"`
	Lookups []glpi.Lookup       `json:"lookups"`
	Status  map[string][][2]any `json:"status"`
	Done    map[string][]int    `json:"done"`
	Fail    map[string][]int    `json:"fail"`
	Content string              `json:"content"`
}

func (s *Server) glpiDesignerData(r *http.Request) glpiDesigner {
	g := glpiDesigner{Lookups: glpi.Lookups, Status: map[string][][2]any{}, Done: glpi.DefaultDone, Fail: glpi.DefaultFail, Content: flow.GLPIDefaultContent, Conns: []map[string]string{}}
	for _, c := range s.app.GLPIConns(r.Context()) {
		g.Conns = append(g.Conns, map[string]string{"key": c.Key, "name": c.Name})
	}
	for t, order := range glpi.StatusOrder {
		for _, id := range order {
			g.Status[t] = append(g.Status[t], [2]any{id, glpi.StatusName[t][id]})
		}
	}
	return g
}

type versionInfo struct {
	ID     int64  `json:"id"`
	V      int    `json:"v"`
	Status string `json:"status"`
	At     string `json:"at"`
	By     string `json:"by"`
	Note   string `json:"note"`
}

func (s *Server) masterDataFor(r *http.Request) ([]flow.Stelle, []flow.Catalog, []store.Var, []flow.Snippet) {
	st, _ := s.st.Stellen(r.Context())
	cat, _ := s.st.Catalogs(r.Context())
	vars, _ := s.st.Vars(r.Context())
	sn, _ := s.st.Snippets(r.Context())
	return st, cat, vars, sn
}

func keySets(st []flow.Stelle, cat []flow.Catalog) (map[string]bool, map[string]bool) {
	a, b := map[string]bool{}, map[string]bool{}
	for _, x := range st {
		a[x.Key] = true
	}
	for _, x := range cat {
		b[x.Key] = true
	}
	return a, b
}

func (s *Server) designer(w http.ResponseWriter, r *http.Request) {
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err != nil {
		s.fail(w, r, 404, "Ablauf nicht gefunden.")
		return
	}
	cur := wf.Current()
	readonly := false
	if v := r.URL.Query().Get("v"); v != "" {
		id, _ := strconv.ParseInt(v, 10, 64)
		if old, err := s.st.VersionByID(r.Context(), id); err == nil && old.WfID == wf.ID {
			cur, readonly = old, true
		}
	}
	st, cat, vars, sn := s.masterDataFor(r)
	all, _ := s.st.Workflows(r.Context(), true)
	p := designerPayload{Key: wf.Key, Def: cur.Def, Status: cur.Status, Version: cur.Version, HasDraft: wf.Draft != nil, HasActive: wf.Active != nil, Archived: wf.Archived, DisabledNote: wf.DisabledNote,
		Stellen: st, Catalogs: cat, Vars: vars, Snippets: sn, FieldTypes: flow.FieldTypes, StepKinds: flow.StepKinds, Ops: flow.Ops, Readonly: readonly, GLPI: s.glpiDesignerData(r)}
	if wf.Archived && wf.DisabledAt.Valid {
		p.DisabledInfo = "seit " + wf.DisabledAt.Time.Local().Format("02.01.2006 15:04") + " · " + wf.DisabledBy
	}
	for _, x := range all {
		if x.Key == wf.Key {
			p.Requests, p.Open = x.Requests, x.Open
		} else if c := x.Current(); c != nil {
			p.Others = append(p.Others, map[string]string{"key": x.Key, "name": c.Def.Name})
		}
	}
	vs, _ := s.st.Versions(r.Context(), wf.ID)
	for _, v := range vs {
		vi := versionInfo{ID: v.ID, V: v.Version, Status: v.Status, At: v.CreatedAt.Local().Format("02.01.2006 15:04"), By: v.CreatedBy, Note: v.Note}
		if v.PublishedAt.Valid {
			vi.At, vi.By = v.PublishedAt.Time.Local().Format("02.01.2006 15:04"), v.PublishedBy
		}
		p.Versions = append(p.Versions, vi)
	}
	sk, ck := keySets(st, cat)
	p.Issues = cur.Def.Validate(sk, ck)
	s.render(w, r, "designer", page{Title: "Designer · " + cur.Def.Name, Nav: "designer", Wide: true, Data: p})
}

func (s *Server) apiDesignerSave(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err != nil {
		s.jsonErr(w, 404, "Ablauf nicht gefunden.")
		return
	}
	var body struct {
		Def flow.Def `json:"def"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, "Ungültige Daten: "+err.Error())
		return
	}
	body.Def.Key = wf.Key
	if err := s.st.SaveDraft(r.Context(), wf, body.Def, se.Username); err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	st, cat, _, _ := s.masterDataFor(r)
	sk, ck := keySets(st, cat)
	s.jsonOut(w, 200, map[string]any{"ok": true, "issues": body.Def.Validate(sk, ck)})
}

func (s *Server) apiDesignerValidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Def flow.Def `json:"def"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	st, cat, _, _ := s.masterDataFor(r)
	sk, ck := keySets(st, cat)
	s.jsonOut(w, 200, map[string]any{"issues": body.Def.Validate(sk, ck)})
}

type renderReq struct {
	Def  flow.Def       `json:"def"`
	Data map[string]any `json:"data"`
	Tpls []string       `json:"tpls"`
	Step string         `json:"step"`
	Item int            `json:"item"`
}

func (s *Server) designerEnv(r *http.Request, body *renderReq) (*flow.Env, map[string]any, error) {
	env, err := s.app.Env(r.Context(), &body.Def)
	if err != nil {
		return nil, nil, err
	}
	normNumbers(body.Data)
	se := sess(r)
	meta := map[string]any{"nummer": strings.ToUpper(body.Def.Prefix) + "-2026-0042", "titel": body.Def.Name, "ablauf": body.Def.Name,
		"antragsteller": se.Username, "antragsteller_name": se.Display, "antragsteller_mail": se.Mail, "datum": "05.11.2026", "link": s.app.Cfg.BaseURL + "/v/42"}
	ctx := env.Context(body.Data, meta, nil)
	if t, err := flow.Render(body.Def.TitleTpl, ctx, env.Vars); err == nil && body.Def.TitleTpl != "" {
		meta["titel"] = strings.TrimSpace(t)
	}
	return env, ctx, nil
}

func (s *Server) apiDesignerRender(w http.ResponseWriter, r *http.Request) {
	var body renderReq
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	env, ctx, err := s.designerEnv(r, &body)
	if err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	ctx["item"] = map[string]any{}
	if st := body.Def.StepByID(body.Step); st != nil && st.ForEach != "" {
		items := env.Items(ctx, st.ForEach)
		if body.Item >= 0 && body.Item < len(items) {
			ctx["item"] = items[body.Item]
			ctx["index"] = body.Item + 1
		} else {
			ctx["item"] = env.EmptyItem(st.ForEach)
		}
	}
	ctx["link"] = s.app.Cfg.BaseURL + "/a/beispiel-token"
	ctx["zusammenfassung"] = "  " + strings.ReplaceAll(app.Summary(env, body.Data), "\n", "\n  ")
	base := strings.TrimRight(s.app.Cfg.BaseURL, "/")
	ctx["glpi"] = map[string]any{"ref": fmt.Sprint(mapPath(ctx, "antrag", "nummer")) + "/" + body.Step, "token": "beispiel-token", "rueckmeldung": base + "/glpi/r/beispiel-token",
		"api": base + "/api/glpi/r/beispiel-token", "vorgang": base + "/v/42", "marker": "[AW:beispiel-token]"}
	type res struct {
		Out string `json:"out"`
		Err string `json:"err,omitempty"`
	}
	var out []res
	for _, t := range body.Tpls {
		o, err := flow.Render(t, ctx, env.Vars)
		x := res{Out: o}
		if err != nil {
			x.Err = err.Error()
		}
		out = append(out, x)
	}
	c, _ := ctx["c"].(map[string]any)
	s.jsonOut(w, 200, map[string]any{"results": out, "computed": c})
}

func (s *Server) apiDesignerSimulate(w http.ResponseWriter, r *http.Request) {
	var body renderReq
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	env, ctx, err := s.designerEnv(r, &body)
	if err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	plan := env.Simulate(body.Data, ctx["antrag"].(map[string]any))
	s.jsonOut(w, 200, map[string]any{"plan": plan})
}

func (s *Server) designerPublish(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err != nil || wf.Draft == nil {
		http.Redirect(w, r, "/designer/"+r.PathValue("key"), http.StatusSeeOther)
		return
	}
	st, cat, _, _ := s.masterDataFor(r)
	sk, ck := keySets(st, cat)
	for _, is := range wf.Draft.Def.Validate(sk, ck) {
		if is.Level == "error" {
			s.fail(w, r, 400, "Veröffentlichen nicht möglich: "+is.Msg)
			return
		}
	}
	n, err := s.st.Publish(r.Context(), wf, strings.TrimSpace(r.FormValue("note")), se.Username)
	if err != nil {
		s.fail(w, r, 500, err.Error())
		return
	}
	s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "ablauf_veroeffentlicht", Detail: fmt.Sprintf("%s v%d", wf.Key, n)})
	http.Redirect(w, r, "/designer/"+wf.Key+"?msg=gespeichert", http.StatusSeeOther)
}

func (s *Server) designerDiscard(w http.ResponseWriter, r *http.Request) {
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err == nil && wf.Active != nil {
		_ = s.st.DiscardDraft(r.Context(), wf)
	}
	http.Redirect(w, r, "/designer/"+r.PathValue("key"), http.StatusSeeOther)
}

func (s *Server) designerArchive(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err == nil {
		off := r.FormValue("archived") == "1"
		note := strings.TrimSpace(r.FormValue("note"))
		if len(note) > 1000 {
			note = note[:1000]
		}
		_ = s.st.SetDisabled(r.Context(), wf, off, note, se.Display)
		act := "ablauf_aktiviert"
		if off {
			act = "ablauf_deaktiviert"
		}
		s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: act, Detail: wf.Key + " " + note})
	}
	if r.FormValue("back") == "designer" {
		http.Redirect(w, r, "/designer/"+r.PathValue("key"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/designer", http.StatusSeeOther)
}

func (s *Server) designerSort(w http.ResponseWriter, r *http.Request) {
	_ = s.st.SetSort(r.Context(), strings.Split(r.FormValue("keys"), ","))
	w.WriteHeader(204)
}

func (s *Server) designerExport(w http.ResponseWriter, r *http.Request) {
	wf, err := s.st.Workflow(r.Context(), r.PathValue("key"))
	if err != nil || wf.Current() == nil {
		s.fail(w, r, 404, "Ablauf nicht gefunden.")
		return
	}
	b, _ := yaml.Marshal(wf.Current().Def)
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+wf.Key+`.yaml"`)
	_, _ = w.Write(b)
}

func (s *Server) designerImport(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	f, _, err := r.FormFile("file")
	if err != nil {
		s.fail(w, r, 400, "Keine Datei.")
		return
	}
	defer f.Close()
	raw, _ := io.ReadAll(io.LimitReader(f, 2<<20))
	var d flow.Def
	if err := yaml.Unmarshal(raw, &d); err != nil {
		if err2 := json.Unmarshal(raw, &d); err2 != nil {
			s.fail(w, r, 400, "Datei ist kein gültiger Ablauf (YAML/JSON): "+err.Error())
			return
		}
	}
	if d.Name == "" {
		d.Name = "Importierter Ablauf"
	}
	d.Key = s.uniqueKey(r, firstNonEmpty(d.Key, d.Name))
	if _, err := s.st.CreateWorkflow(r.Context(), d, se.Username, false, 0); err != nil {
		s.fail(w, r, 400, err.Error())
		return
	}
	http.Redirect(w, r, "/designer/"+d.Key, http.StatusSeeOther)
}

func firstNonEmpty(a ...string) string {
	for _, x := range a {
		if x != "" {
			return x
		}
	}
	return ""
}

// ---------- Stammdaten ----------

type masterPayload struct {
	Sources      []orgsync.Source           `json:"sources"`
	States       map[string]*orgsync.Status `json:"states"`
	PersonFields any                        `json:"person_fields"`
	Persons      map[string]int             `json:"persons"`
	Preset       orgsync.Source             `json:"preset"`
	Catalogs     []flow.Catalog             `json:"catalogs"`
	Stellen      []flow.Stelle              `json:"stellen"`
	Vars         []store.Var                `json:"vars"`
	Snippets     []flow.Snippet             `json:"snippets"`
	Usage        map[string][]string        `json:"usage"`
}

func (s *Server) masterData(w http.ResponseWriter, r *http.Request) {
	st, cat, vars, sn := s.masterDataFor(r)
	usage := map[string][]string{}
	wfs, _ := s.st.Workflows(r.Context(), false)
	for _, wf := range wfs {
		c := wf.Current()
		if c == nil {
			continue
		}
		seen := map[string]bool{}
		mark := func(k string) {
			if k != "" && !seen[k] {
				seen[k] = true
				usage[k] = append(usage[k], c.Def.Name)
			}
		}
		for _, fr := range c.Def.AllFields() {
			if fr.Field.Type == "catalog" {
				mark("catalog:" + fr.Field.Catalog)
			}
		}
		for _, stp := range c.Def.Steps {
			if stp.Assign.Type == "stelle" {
				mark("stelle:" + stp.Assign.Stelle)
			}
		}
	}
	srcs := orgsync.Sources(r.Context(), s.st)
	states := map[string]*orgsync.Status{}
	persons := map[string]int{}
	for _, x := range srcs {
		states[x.Key] = orgsync.LastStatus(r.Context(), s.st, x.Key)
		persons[x.Key] = s.st.CountDocs(r.Context(), "person:"+x.Key)
	}
	if srcs == nil {
		srcs = []orgsync.Source{}
	}
	s.render(w, r, "stammdaten", page{Title: "Stammdaten", Nav: "stammdaten", Wide: true, Data: masterPayload{Catalogs: cat, Stellen: st, Vars: vars, Snippets: sn, Usage: usage,
		Sources: srcs, States: states, PersonFields: orgsync.PersonFields, Persons: persons, Preset: seed.TelefonbuchPreset()}})
}

var docKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,60}$`)

func (s *Server) apiMasterSave(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	kind := r.PathValue("kind")
	var keys []string
	var vals []any
	dup := map[string]bool{}
	check := func(k string) error {
		if !docKeyRe.MatchString(k) {
			return fmt.Errorf("Schlüssel %q ungültig (Kleinbuchstaben, Ziffern, . _ -)", k)
		}
		if dup[k] {
			return fmt.Errorf("Schlüssel %q doppelt", k)
		}
		dup[k] = true
		return nil
	}
	var err error
	switch kind {
	case "catalog":
		var lst []flow.Catalog
		if err = decodeJSON(r, &lst); err == nil {
			for _, c := range lst {
				if err = check(c.Key); err != nil {
					break
				}
				ek := map[string]bool{}
				for _, e := range c.Entries {
					if strings.TrimSpace(e.Key) == "" || ek[e.Key] {
						err = fmt.Errorf("Katalog %s: Eintragsschlüssel %q leer oder doppelt", c.Name, e.Key)
						break
					}
					ek[e.Key] = true
				}
				if err != nil {
					break
				}
				keys, vals = append(keys, c.Key), append(vals, c)
			}
		}
	case "stelle":
		var lst []flow.Stelle
		if err = decodeJSON(r, &lst); err == nil {
			for _, c := range lst {
				if err = check(c.Key); err != nil {
					break
				}
				keys, vals = append(keys, c.Key), append(vals, c)
			}
		}
	case "var":
		var lst []store.Var
		if err = decodeJSON(r, &lst); err == nil {
			for _, c := range lst {
				if err = check(c.Key); err != nil {
					break
				}
				keys, vals = append(keys, c.Key), append(vals, c)
			}
		}
	case "source":
		var lst []orgsync.Source
		if err = decodeJSON(r, &lst); err == nil {
			for _, c := range lst {
				if err = check(c.Key); err != nil {
					break
				}
				if c.Kind == "url" && c.URL != "" && !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
					err = fmt.Errorf("Datenquelle %s: URL muss mit http:// oder https:// beginnen", c.Name)
					break
				}
				keys, vals = append(keys, c.Key), append(vals, c)
			}
		}
	case "snippet":
		var lst []flow.Snippet
		if err = decodeJSON(r, &lst); err == nil {
			for _, c := range lst {
				if err = check(c.Key); err != nil {
					break
				}
				keys, vals = append(keys, c.Key), append(vals, c)
			}
		}
	default:
		s.jsonErr(w, 404, "unbekannt")
		return
	}
	if err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	if err := s.st.ReplaceDocs(r.Context(), kind, keys, vals, se.Username); err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "stammdaten_" + kind, Detail: fmt.Sprintf("%d Einträge", len(keys))})
	s.jsonOut(w, 200, map[string]any{"ok": true})
}

// ---------- Postausgang & Protokoll ----------

func (s *Server) outbox(w http.ResponseWriter, r *http.Request) {
	ms, _ := s.st.Mails(r.Context(), 0, 300)
	s.render(w, r, "outbox", page{Title: "Postausgang", Nav: "outbox", Data: map[string]any{"Items": ms, "SMTP": s.app.Mailer.Enabled()}})
}

func (s *Server) mailView(w http.ResponseWriter, r *http.Request) {
	m, err := s.st.Mail(r.Context(), pathID(r, "id"))
	if err != nil {
		s.fail(w, r, 404, "Mail nicht gefunden.")
		return
	}
	s.render(w, r, "mail", page{Title: m.Subject, Nav: "outbox", Data: m})
}

func (s *Server) auditPage(w http.ResponseWriter, r *http.Request) {
	items, _ := s.st.AuditLog(r.Context(), 500)
	s.render(w, r, "audit", page{Title: "Protokoll", Nav: "audit", Data: items})
}

// ---------- Datenquellen ----------

func (s *Server) apiSourceTest(w http.ResponseWriter, r *http.Request) {
	var src orgsync.Source
	if err := decodeJSON(r, &src); err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	res, err := orgsync.Run(r.Context(), s.st, src, false, sess(r).Username)
	if err != nil {
		s.jsonErr(w, 502, "Abruf fehlgeschlagen: "+err.Error())
		return
	}
	s.jsonOut(w, 200, map[string]any{"results": res})
}

func (s *Server) apiSourceSync(w http.ResponseWriter, r *http.Request) {
	for _, src := range orgsync.Sources(r.Context(), s.st) {
		if src.Key == r.PathValue("key") {
			res, st := orgsync.Sync(r.Context(), s.st, src, sess(r).Username)
			s.jsonOut(w, 200, map[string]any{"results": res, "status": st})
			return
		}
	}
	s.jsonErr(w, 404, "Datenquelle nicht gefunden – bitte zuerst speichern.")
}

func mapPath(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = mm[k]
	}
	return v
}
