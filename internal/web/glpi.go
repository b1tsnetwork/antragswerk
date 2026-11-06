package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"antragswerk/internal/app"
	"antragswerk/internal/flow"
	"antragswerk/internal/glpi"
	"antragswerk/internal/store"
)

const secretMask = "••••••••"

// ---------- Rückmeldung aus GLPI (ohne Anmeldung, Token im Ticket) ----------

type glpiFeedbackData struct {
	Req     *store.Request
	Inst    *store.Instance
	Step    *flow.Step
	Token   string
	Done    string
	Ticket  string
	URL     string
	Fields  []flow.Field
	Values  map[string]string
	Summary []viewSection
}

func (s *Server) glpiFeedbackPage(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	in, req, st, err := s.app.GLPIByToken(r.Context(), tok)
	if err != nil || in.State != "extern" {
		w.WriteHeader(404)
		s.render(w, r, "glpifeedback", page{Title: "Link nicht mehr gültig", Data: glpiFeedbackData{Done: "Dieser Rückmeldelink ist nicht mehr gültig – der Schritt ist bereits erledigt oder der Vorgang abgeschlossen."}})
		return
	}
	_, env, _, err := s.app.Load(r.Context(), req.ID)
	if err != nil {
		s.fail(w, r, 404, "Vorgang nicht gefunden.")
		return
	}
	ins, _ := s.st.Instances(r.Context(), req.ID)
	d := glpiFeedbackData{Req: req, Inst: in, Step: st, Token: tok, Fields: st.Outputs, Values: map[string]string{}, Summary: viewSections(env, s.app.Context(env, req, ins))}
	if id := glpi.ID(in.Outputs["glpi_id"]); id > 0 {
		d.Ticket = fmt.Sprintf("%v #%d", in.Outputs["glpi_type"], id)
		d.URL = fmt.Sprint(in.Outputs["glpi_url"])
	}
	s.render(w, r, "glpifeedback", page{Title: st.Title, Data: d, Err: r.URL.Query().Get("err")})
}

func (s *Server) glpiFeedbackPost(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	_ = r.ParseForm()
	fb := app.GLPIFeedback{Status: r.FormValue("status"), Comment: r.FormValue("kommentar"), By: r.FormValue("von"), Outputs: map[string]any{}}
	for k, v := range r.Form {
		if strings.HasPrefix(k, "out_") && len(v) > 0 {
			fb.Outputs[strings.TrimPrefix(k, "out_")] = strings.TrimSpace(v[0])
		}
	}
	err := s.app.GLPIRespond(r.Context(), tok, fb)
	s.st.Audit(r.Context(), store.AuditEntry{Actor: "glpi-link:" + fb.By, IP: s.ip(r), Action: "glpi_rueckmeldung", Detail: fb.Status})
	if err != nil {
		http.Redirect(w, r, "/glpi/r/"+urlEsc(tok)+"?err="+urlEsc(err.Error()), http.StatusSeeOther)
		return
	}
	msg := "Danke – die Rückmeldung ist im Antragswerk eingetragen, der Vorgang läuft weiter."
	if fb.Status == "problem" {
		msg = "Danke – das Problem ist im Antragswerk vermerkt, die Zuständigen kümmern sich."
	}
	s.render(w, r, "glpifeedback", page{Title: "Rückmeldung erhalten", Data: glpiFeedbackData{Done: msg}})
}

// glpiFeedbackAPI: POST /api/glpi/r/{token} – JSON {"status":"erledigt|problem","kommentar":"…","von":"…","felder":{…}}
// oder Formular (status, kommentar, von, weitere Felder = Rückmeldefelder).
func (s *Server) glpiFeedbackAPI(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	var fb app.GLPIFeedback
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&fb); err != nil {
			s.jsonErr(w, 400, "JSON nicht lesbar: "+err.Error())
			return
		}
	} else {
		_ = r.ParseForm()
		fb = app.GLPIFeedback{Status: r.FormValue("status"), Comment: r.FormValue("kommentar"), By: r.FormValue("von"), Outputs: map[string]any{}}
		for k, v := range r.Form {
			if k != "status" && k != "kommentar" && k != "von" && len(v) > 0 {
				fb.Outputs[k] = v[0]
			}
		}
	}
	if fb.Outputs == nil {
		fb.Outputs = map[string]any{}
	}
	err := s.app.GLPIRespond(r.Context(), tok, fb)
	s.st.Audit(r.Context(), store.AuditEntry{Actor: "glpi-api:" + fb.By, IP: s.ip(r), Action: "glpi_rueckmeldung", Detail: fb.Status})
	if err != nil {
		s.jsonErr(w, 409, err.Error())
		return
	}
	s.jsonOut(w, 200, map[string]any{"ok": true})
}

// glpiWebhook: Ziel der GLPI-Webhooks (Ticket/Change/Folgeeintrag/Lösung, Ereignis „Aktualisierung“).
// Mit Geheimnis wird X-GLPI-signature = HMAC-SHA256(Body + X-GLPI-timestamp) geprüft.
func (s *Server) glpiWebhook(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("conn")
	conn, err := s.app.GLPIConn(r.Context(), key)
	if err != nil {
		s.jsonErr(w, 404, "Verbindung unbekannt")
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if conn.WebhookSecret != "" {
		ts := r.Header.Get("X-GLPI-timestamp")
		mac := hmac.New(sha256.New, []byte(conn.WebhookSecret))
		mac.Write(raw)
		mac.Write([]byte(ts))
		want := hex.EncodeToString(mac.Sum(nil))
		got := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GLPI-signature")))
		t, _ := strconv.ParseInt(ts, 10, 64)
		if !hmac.Equal([]byte(got), []byte(want)) {
			s.jsonErr(w, 401, "Signatur ungültig")
			return
		}
		if d := time.Since(time.Unix(t, 0)); d > 24*time.Hour || d < -10*time.Minute {
			s.jsonErr(w, 401, "Zeitstempel abgelaufen")
			return
		}
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	go s.app.GLPIWebhook(context.Background(), conn.Key, payload, raw)
	s.jsonOut(w, 202, map[string]any{"ok": true})
}

// glpiStepAction: Knöpfe auf der Vorgangsseite.
func (s *Server) glpiStepAction(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	id, iid := pathID(r, "id"), pathID(r, "iid")
	inst, err := s.st.Instance(r.Context(), iid)
	if err != nil || inst.RequestID != id {
		s.fail(w, r, 404, "Schritt nicht gefunden.")
		return
	}
	action := r.FormValue("action")
	err = s.app.GLPIAction(r.Context(), iid, action, s.actor(se))
	if errors.Is(err, app.ErrForbidden) {
		err = errors.New("Sie sind für diesen Schritt nicht zuständig.")
	}
	if err == nil {
		s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "glpi_" + action, Detail: fmt.Sprintf("Vorgang %d · %s", id, inst.Title)})
	}
	back(w, r, id, iid, "glpi_"+action, err)
}

// ---------- Verwaltung ----------

func (s *Server) glpiConnsMasked(ctx context.Context) []glpi.Conn {
	cs := s.app.GLPIConns(ctx)
	for i := range cs {
		for _, p := range []*string{&cs[i].ClientSecret, &cs[i].Password, &cs[i].WebhookSecret} {
			if *p != "" {
				*p = secretMask
			}
		}
	}
	if cs == nil {
		cs = []glpi.Conn{}
	}
	return cs
}

func (s *Server) apiGLPISave(w http.ResponseWriter, r *http.Request) {
	var in []glpi.Conn
	if err := decodeJSON(r, &in); err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	old := map[string]glpi.Conn{}
	for _, c := range s.app.GLPIConns(r.Context()) {
		old[c.Key] = c
	}
	keys := make([]string, 0, len(in))
	vals := make([]any, 0, len(in))
	seen := map[string]bool{}
	for _, c := range in {
		c.Key = strings.TrimSpace(c.Key)
		if c.Key == "" || seen[c.Key] {
			s.jsonErr(w, 400, "Jede Verbindung braucht einen eindeutigen Schlüssel.")
			return
		}
		seen[c.Key] = true
		o := old[c.Key]
		if c.ClientSecret == secretMask {
			c.ClientSecret = o.ClientSecret
		}
		if c.Password == secretMask {
			c.Password = o.Password
		}
		if c.WebhookSecret == secretMask {
			c.WebhookSecret = o.WebhookSecret
		}
		c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
		keys = append(keys, c.Key)
		vals = append(vals, c)
	}
	se := sess(r)
	if err := s.st.ReplaceDocs(r.Context(), "glpi", keys, vals, se.Username); err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "glpi_verbindungen", Detail: strings.Join(keys, ", ")})
	s.jsonOut(w, 200, map[string]any{"ok": true, "conns": s.glpiConnsMasked(r.Context())})
}

func (s *Server) glpiConnFromRequest(r *http.Request, c glpi.Conn) glpi.Conn {
	for _, o := range s.app.GLPIConns(r.Context()) {
		if o.Key == c.Key {
			if c.ClientSecret == secretMask {
				c.ClientSecret = o.ClientSecret
			}
			if c.Password == secretMask {
				c.Password = o.Password
			}
		}
	}
	return c
}

func (s *Server) apiGLPITest(w http.ResponseWriter, r *http.Request) {
	var c glpi.Conn
	if err := decodeJSON(r, &c); err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	c = s.glpiConnFromRequest(r, c)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cl := glpi.New(c)
	st, err := cl.Status(ctx)
	if err != nil {
		s.jsonOut(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	res := map[string]any{"ok": true, "status": st}
	if l, err := cl.Search(ctx, "Entity", "", 20); err == nil {
		res["entities"] = len(l)
	} else {
		res["warn"] = "Anmeldung klappt, aber Entitäten nicht lesbar: " + err.Error()
	}
	s.jsonOut(w, 200, res)
}

func (s *Server) apiGLPILookup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Conn glpi.Conn `json:"conn"`
		Kind string    `json:"kind"`
		Q    string    `json:"q"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.jsonErr(w, 400, err.Error())
		return
	}
	c := body.Conn
	if c.URL == "" {
		var err error
		if c, err = s.app.GLPIConn(r.Context(), c.Key); err != nil {
			s.jsonErr(w, 400, err.Error())
			return
		}
	} else {
		c = s.glpiConnFromRequest(r, c)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	list, err := glpi.For(c).Search(ctx, body.Kind, body.Q, 50)
	if err != nil {
		s.jsonErr(w, 502, err.Error())
		return
	}
	type row struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Full string `json:"full,omitempty"`
	}
	out := []row{}
	for _, o := range list {
		x := row{ID: glpi.ID(o["id"]), Name: fmt.Sprint(mapFirst(o, "name", "username"))}
		if full := fmt.Sprint(mapFirst(o, "completename", "realname")); full != "" && full != x.Name {
			x.Full = full
		}
		out = append(out, x)
	}
	s.jsonOut(w, 200, map[string]any{"results": out})
}

func mapFirst(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil && fmt.Sprint(v) != "" {
			return v
		}
	}
	return ""
}

// apiGLPIPreview: Designer – was würde an GLPI geschickt?
func (s *Server) apiGLPIPreview(w http.ResponseWriter, r *http.Request) {
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
	st := body.Def.StepByID(body.Step)
	if st == nil || st.GLPI == nil {
		s.jsonErr(w, 400, "kein GLPI-Schritt")
		return
	}
	var item any
	if st.ForEach != "" {
		if items := env.Items(ctx, st.ForEach); len(items) > 0 {
			item = items[0]
		} else {
			item = env.EmptyItem(st.ForEach)
		}
	}
	out, errs := s.app.GLPIPreview(r.Context(), env, ctx, body.Data, st, item)
	s.jsonOut(w, 200, map[string]any{"requests": out, "errors": errs})
}
