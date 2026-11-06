// Package web stellt Oberfläche und API bereit.
package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"antragswerk/internal/app"
	"antragswerk/internal/dir"
	"antragswerk/internal/flow"
	"antragswerk/internal/glpi"
	"antragswerk/internal/seed"
	"antragswerk/internal/store"
	"antragswerk/internal/ui"
)

//go:embed templates/*.html
var tplFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	app     *app.App
	st      *store.Store
	dir     *dir.Directory
	tpl     map[string]*template.Template
	version string
	live    *broker
	limiter *limiter
	assetV  string

	glpiDemo *glpi.Mock // Test-GLPI unter /demo-glpi (nur Demo-Modus)
}

type ctxKey int

const sessKey ctxKey = 1

const cookieName = "aw_session"

func New(a *app.App, d *dir.Directory, version string) (*Server, error) {
	s := &Server{app: a, st: a.Store, dir: d, version: version, live: newBroker(), limiter: newLimiter(), assetV: strconv.FormatInt(time.Now().Unix(), 36)}
	a.OnChange = func(id int64) { s.live.publish(fmt.Sprintf(`{"type":"request","id":%d}`, id)) }
	if d.DemoMode() {
		s.glpiDemo = glpi.NewMock("/demo-glpi")
		s.glpiDemo.Webhook = strings.TrimRight(a.Cfg.BaseURL, "/") + "/api/glpi/webhook/" + seed.DemoGLPIKey
		s.glpiDemo.Secret = seed.DemoGLPIWebhookSecret
	}
	d.Extra = func(q string, limit int) []dir.Person {
		var out []dir.Person
		for _, m := range a.Store.SearchPersons(context.Background(), q, limit) {
			out = append(out, dir.Person{Sam: m["sam"], Name: m["name"], Given: m["given"], Surname: m["surname"], Mail: m["mail"], Phone: m["phone"],
				Department: m["department"], Title: m["title"], Office: m["office"], Company: m["company"], Manager: m["manager"]})
		}
		return out
	}
	var st ui.Settings
	if a.Store.GetDoc(context.Background(), "setting", "ui", &st) {
		ui.Set(st)
	} else {
		d := ui.Defaults()
		d.Org = a.Cfg.Org
		ui.Set(d)
	}
	if err := s.parse(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) parse() error {
	funcs := template.FuncMap{
		"dt": func(t any) string {
			switch v := t.(type) {
			case time.Time:
				if v.IsZero() {
					return ""
				}
				return v.Local().Format("02.01.2006 15:04")
			case sql.NullTime:
				if v.Valid {
					return v.Time.Local().Format("02.01.2006 15:04")
				}
				return ""
			case *sql.NullTime:
				if v != nil && v.Valid {
					return v.Time.Local().Format("02.01.2006 15:04")
				}
				return ""
			case interface{ Value() (any, error) }:
				x, _ := v.Value()
				if tt, ok := x.(time.Time); ok {
					return tt.Local().Format("02.01.2006 15:04")
				}
			}
			return ""
		},
		"d":          func(t time.Time) string { return t.Local().Format("02.01.2006") },
		"rel":        relTime,
		"stateLabel": stateLabel, "stateClass": stateClass, "statusLabel": statusLabel,
		"kindLabel": kindLabel, "kindIcon": kindIcon,
		"json":     func(v any) template.JS { b, _ := json.Marshal(v); return template.JS(b) },
		"md":       md,
		"initials": initials,
		"add":      func(a, b int) int { return a + b },
		"pct": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a * 100 / b
		},
		"t":         ui.T,
		"tf":        ui.Tf,
		"tmd":       func(k string) template.HTML { return md(ui.T(k)) },
		"has":       func(se *store.Session, role string) bool { return se != nil && roleAtLeast(se.Role, role) },
		"roleLabel": roleLabel,
		"join":      strings.Join,
		"safeColor": safeColor,
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i
			}
			return out
		},
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
		"checked": func(lst []bool, i int) bool { return i < len(lst) && lst[i] },
		"str": func(v any) string {
			if v == nil {
				return ""
			}
			return fmt.Sprint(v)
		},
	}
	s.tpl = map[string]*template.Template{}
	pages, err := fs.Glob(tplFS, "templates/*.html")
	if err != nil {
		return err
	}
	for _, p := range pages {
		name := strings.TrimSuffix(strings.TrimPrefix(p, "templates/"), ".html")
		if name == "layout" || name == "partials" {
			continue
		}
		t, err := template.New("layout").Funcs(funcs).ParseFS(tplFS, "templates/layout.html", "templates/partials.html", p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		s.tpl[name] = t
	}
	return nil
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

func safeColor(c string) template.CSS {
	if colorRe.MatchString(c) {
		return template.CSS(c)
	}
	return "#1f5fbf"
}

// page ist der Rahmen jeder Seite.
type page struct {
	Title     string
	Nav       string
	Session   *store.Session
	CSRF      string
	Msg       string
	Err       string
	Data      any
	Version   string
	AssetV    string
	Demo      bool
	Wide      bool
	Inbox     int
	Org       string
	App       string
	Theme     template.CSS
	Mode      string
	UserTheme bool
	Presets   template.JS
	Banner    string
	Tone      string
	TextsJS   template.JS
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	t, ok := s.tpl[name]
	if !ok {
		http.Error(w, "Vorlage fehlt: "+name, 500)
		return
	}
	p.Session = sess(r)
	if p.Session != nil {
		p.CSRF = p.Session.CSRF
		if items, err := s.st.Inbox(r.Context(), s.visibility(p.Session), 200); err == nil {
			p.Inbox = len(items)
		}
	}
	if p.Msg == "" {
		p.Msg = flashMsg(r.URL.Query().Get("msg"))
	}
	st := ui.Get()
	p.Version, p.AssetV, p.Demo = s.version, s.assetV, s.dir.DemoMode()
	p.Org, p.App = st.Org, st.AppName
	p.Theme = template.CSS(ui.ThemeCSS())
	p.Mode, p.UserTheme = st.Theme.Mode, st.Theme.AllowUser
	pv, _ := json.Marshal(ui.PresetVars())
	p.Presets = template.JS(pv)
	p.Banner, p.Tone = st.Home.Banner, st.Home.Tone
	tj, _ := json.Marshal(ui.All())
	p.TextsJS = template.JS(tj)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", p); err != nil {
		slog.Error("render", "tpl", name, "err", err)
	}
}

func flashMsg(k string) string {
	switch k {
	case "gestellt", "erledigt", "freigegeben", "abgelehnt", "rueckfrage", "beantwortet":
		return ui.T("flash." + k)
	case "notiz":
		return "Notiz gespeichert."
	case "weitergeleitet":
		return "Schritt weitergeleitet."
	case "zurueckgezogen":
		return "Vorgang zurückgezogen."
	case "loggedout":
		return "Sie wurden abgemeldet."
	case "gespeichert":
		return "Gespeichert."
	case "glpi_abfragen":
		return "GLPI-Status abgefragt."
	case "glpi_erneut":
		return "An GLPI übergeben."
	case "glpi_manuell":
		return "Schritt manuell übernommen – er steht jetzt bei den Zuständigen."
	}
	return ""
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, code int, msg string) {
	w.WriteHeader(code)
	s.render(w, r, "error", page{Title: "Fehler", Err: msg, Data: code})
}

func (s *Server) jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) jsonErr(w http.ResponseWriter, code int, msg string) {
	s.jsonOut(w, code, map[string]string{"error": msg})
}

// ---------- Routing ----------

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(staticFS, "static")
	static := http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		static.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("GET /a/{token}", s.tokenLink)
	mux.HandleFunc("POST /a/{token}", s.tokenAct)
	// GLPI: Rückmeldung (Token im Ticket) und Webhook – ohne Anmeldung
	mux.HandleFunc("GET /glpi/r/{token}", s.glpiFeedbackPage)
	mux.HandleFunc("POST /glpi/r/{token}", s.glpiFeedbackPost)
	mux.HandleFunc("POST /api/glpi/r/{token}", s.glpiFeedbackAPI)
	mux.HandleFunc("POST /api/glpi/webhook/{conn}", s.glpiWebhook)
	if s.glpiDemo != nil {
		mux.Handle("/demo-glpi/", s.glpiDemo)
	}

	auth := func(role string, h http.HandlerFunc) http.Handler { return s.requireAuth(role, h) }
	mux.Handle("POST /logout", auth("", s.logout))
	mux.Handle("GET /{$}", auth("", s.dashboard))
	mux.Handle("GET /neu", auth("", s.newCatalog))
	mux.Handle("GET /neu/{key}", auth("", s.newForm))
	mux.Handle("POST /api/antrag/{key}", auth("", s.apiSubmit))
	mux.Handle("POST /api/simulate/{key}", auth("", s.apiSimulate))
	mux.Handle("GET /api/users", auth("", s.apiUsers))
	mux.Handle("GET /live", auth("", s.liveHandler))
	mux.Handle("GET /vorgaenge", auth("", s.list))
	mux.Handle("GET /aufgaben", auth("", s.inbox))
	mux.Handle("GET /v/{id}", auth("", s.detail))
	mux.Handle("GET /v/{id}/druck", auth("", s.printView))
	mux.Handle("GET /v/{id}/bearbeiten", auth("", s.editForm))
	mux.Handle("POST /api/v/{id}/resubmit", auth("", s.apiResubmit))
	mux.Handle("POST /v/{id}/notiz", auth("", s.note))
	mux.Handle("POST /v/{id}/zurueckziehen", auth("", s.withdraw))
	mux.Handle("POST /v/{id}/s/{iid}", auth("", s.act))
	mux.Handle("POST /v/{id}/s/{iid}/weiterleiten", auth("", s.reassign))
	mux.Handle("GET /v/{id}/s/{iid}/skript/{n}", auth("", s.downloadScript))
	mux.Handle("POST /v/{id}/glpi/{iid}", auth("", s.glpiStepAction))

	// Gestaltung (Admin)
	mux.Handle("GET /designer", auth("admin", s.designerList))
	mux.Handle("POST /designer/neu", auth("admin", s.designerCreate))
	mux.Handle("GET /designer/{key}", auth("admin", s.designer))
	mux.Handle("GET /designer/{key}/export.yaml", auth("admin", s.designerExport))
	mux.Handle("POST /designer/import", auth("admin", s.designerImport))
	mux.Handle("POST /designer/{key}/publish", auth("admin", s.designerPublish))
	mux.Handle("POST /designer/{key}/discard", auth("admin", s.designerDiscard))
	mux.Handle("POST /designer/{key}/archive", auth("admin", s.designerArchive))
	mux.Handle("POST /designer/{key}/copy", auth("admin", s.designerCopy))
	mux.Handle("POST /designer/sort", auth("admin", s.designerSort))
	mux.Handle("PUT /api/designer/{key}", auth("admin", s.apiDesignerSave))
	mux.Handle("POST /api/designer/validate", auth("admin", s.apiDesignerValidate))
	mux.Handle("POST /api/designer/render", auth("admin", s.apiDesignerRender))
	mux.Handle("POST /api/designer/simulate", auth("admin", s.apiDesignerSimulate))
	mux.Handle("GET /einstellungen", auth("admin", s.settingsPage))
	mux.Handle("PUT /api/einstellungen", auth("admin", s.apiSettingsSave))
	mux.Handle("GET /stammdaten", auth("admin", s.masterData))
	mux.Handle("PUT /api/stammdaten/{kind}", auth("admin", s.apiMasterSave))
	mux.Handle("POST /api/datenquellen/test", auth("admin", s.apiSourceTest))
	mux.Handle("PUT /api/glpi", auth("admin", s.apiGLPISave))
	mux.Handle("POST /api/glpi/test", auth("admin", s.apiGLPITest))
	mux.Handle("POST /api/glpi/nachschlagen", auth("admin", s.apiGLPILookup))
	mux.Handle("POST /api/glpi/vorschau", auth("admin", s.apiGLPIPreview))
	mux.Handle("POST /api/datenquellen/{key}/sync", auth("admin", s.apiSourceSync))
	mux.Handle("GET /postausgang", auth("bearbeiter", s.outbox))
	mux.Handle("GET /postausgang/{id}", auth("bearbeiter", s.mailView))
	mux.Handle("GET /protokoll", auth("admin", s.auditPage))
	return s.secure(mux)
}

func (s *Server) secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		h.ServeHTTP(w, r)
	})
}

// ---------- Sitzung ----------

func sess(r *http.Request) *store.Session {
	se, _ := r.Context().Value(sessKey).(*store.Session)
	return se
}

func hashTok(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func roleRank(r string) int {
	switch r {
	case "admin":
		return 3
	case "bearbeiter":
		return 2
	case "nutzer":
		return 1
	}
	return 0
}

func roleAtLeast(have, need string) bool { return roleRank(have) >= roleRank(need) }

func roleLabel(r string) string {
	switch r {
	case "admin":
		return "Administration"
	case "bearbeiter":
		return "Bearbeitung"
	}
	return "Antragstellung"
}

func (s *Server) current(r *http.Request) *store.Session {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	se, err := s.st.GetSession(r.Context(), hashTok(c.Value))
	if err != nil {
		return nil
	}
	return se
}

func (s *Server) requireAuth(role string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		se := s.current(r)
		if se == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/live" {
				s.jsonErr(w, 401, "Sitzung abgelaufen – bitte neu anmelden.")
				return
			}
			http.Redirect(w, r, "/login?next="+urlEsc(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if r.Method != http.MethodGet {
			tok := r.Header.Get("X-CSRF")
			if tok == "" {
				tok = r.FormValue("csrf")
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(se.CSRF)) != 1 {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					s.jsonErr(w, 403, "CSRF-Token ungültig – bitte Seite neu laden.")
				} else {
					http.Error(w, "CSRF-Token ungültig – bitte Seite neu laden.", 403)
				}
				return
			}
		}
		if role != "" && !roleAtLeast(se.Role, role) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				s.jsonErr(w, 403, "Keine Berechtigung.")
				return
			}
			s.fail(w, r.WithContext(context.WithValue(r.Context(), sessKey, se)), 403, "Für diese Seite fehlt Ihnen die Berechtigung.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessKey, se)))
	})
}

func urlEsc(s string) string {
	return strings.NewReplacer("&", "%26", "?", "%3F", "#", "%23", " ", "%20").Replace(s)
}

func (s *Server) actor(se *store.Session) app.Actor {
	return app.Actor{User: se.Username, Name: se.Display, Mail: se.Mail, Role: se.Role, Stellen: se.Stellen}
}

func (s *Server) visibility(se *store.Session) *store.Visibility {
	return &store.Visibility{Username: se.Username, Mail: se.Mail, Stellen: se.Stellen}
}

func (s *Server) ip(r *http.Request) string {
	if s.app.Cfg.TrustProxy {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			return strings.TrimSpace(strings.Split(f, ",")[0])
		}
	}
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return h
}

type loginData struct {
	User string
	Next string
	Demo []dir.DemoUser
	LDAP bool
}

func safeNext(n string) string {
	if n == "" || !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.HasPrefix(n, "/\\") {
		return "/"
	}
	return n
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if s.current(r) != nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", page{Title: "Anmeldung", Data: loginData{Next: r.URL.Query().Get("next"), Demo: s.dir.Demo(), LDAP: s.dir.LDAP()}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	ip := s.ip(r)
	again := func(msg string) {
		w.WriteHeader(401)
		s.render(w, r, "login", page{Title: "Anmeldung", Err: msg, Data: loginData{User: user, Next: next, Demo: s.dir.Demo(), LDAP: s.dir.LDAP()}})
	}
	key := ip + "|" + strings.ToLower(user)
	if s.limiter.blocked(key) {
		again("Zu viele Fehlversuche – bitte in einigen Minuten erneut versuchen.")
		return
	}
	sts, _ := s.st.Stellen(r.Context())
	var sg []dir.StelleGroup
	for _, st := range sts {
		sg = append(sg, dir.StelleGroup{Key: st.Key, Group: st.Group})
	}
	id, err := s.dir.Authenticate(user, pass, sg)
	if err != nil {
		s.limiter.fail(key)
		s.st.Audit(r.Context(), store.AuditEntry{Actor: user, IP: ip, Action: "login_fehler", Detail: err.Error()})
		switch {
		case errors.Is(err, dir.ErrInvalid):
			again("Benutzername oder Kennwort falsch.")
		case errors.Is(err, dir.ErrNotAuthorized):
			again("Ihr Konto ist für Antragswerk nicht freigeschaltet.")
		default:
			slog.Warn("login", "err", err)
			again("Anmeldung derzeit nicht möglich (Verzeichnisdienst nicht erreichbar).")
		}
		return
	}
	s.limiter.reset(key)
	// Stellen zusätzlich über Mitgliederlisten (Mail/Kennung)
	for _, st := range sts {
		for _, m := range st.Members {
			if strings.EqualFold(m, id.Sam) || (id.Mail != "" && strings.EqualFold(m, id.Mail)) {
				if !contains(id.Stellen, st.Key) {
					id.Stellen = append(id.Stellen, st.Key)
				}
			}
		}
	}
	tok := randTok()
	se := store.Session{TokenHash: hashTok(tok), Username: id.Sam, Display: id.Name, Mail: id.Mail, Role: id.Role, Stellen: id.Stellen, CSRF: randTok(),
		ExpiresAt: time.Now().Add(time.Duration(s.app.Cfg.SessionHours) * time.Hour)}
	if err := s.st.CreateSession(r.Context(), se); err != nil {
		again("Interner Fehler.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", HttpOnly: true, Secure: s.app.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
		MaxAge: s.app.Cfg.SessionHours * 3600})
	s.st.Audit(r.Context(), store.AuditEntry{Actor: id.Sam, IP: ip, Action: "login", Detail: roleLabel(id.Role) + " · Stellen: " + strings.Join(id.Stellen, ", ")})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func contains(l []string, x string) bool {
	for _, y := range l {
		if y == x {
			return true
		}
	}
	return false
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if se := sess(r); se != nil {
		_ = s.st.DeleteSession(r.Context(), se.TokenHash)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/login?msg=loggedout", http.StatusSeeOther)
}

// ---------- Live (Server-Sent Events) ----------

type broker struct {
	mu   sync.Mutex
	subs map[chan string]bool
}

func newBroker() *broker { return &broker{subs: map[chan string]bool{}} }

func (b *broker) publish(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.subs {
		select {
		case c <- msg:
		default:
		}
	}
}

func (s *Server) liveHandler(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "kein Streaming", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	c := make(chan string, 16)
	s.live.mu.Lock()
	s.live.subs[c] = true
	s.live.mu.Unlock()
	defer func() {
		s.live.mu.Lock()
		delete(s.live.subs, c)
		s.live.mu.Unlock()
	}()
	fmt.Fprint(w, "retry: 5000\n\n")
	fl.Flush()
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-c:
			fmt.Fprintf(w, "data: %s\n\n", m)
			fl.Flush()
		case <-t.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// ---------- Anmeldebremse ----------

type limiter struct {
	mu sync.Mutex
	m  map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{m: map[string][]time.Time{}} }

func (l *limiter) blocked(k string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keep []time.Time
	for _, t := range l.m[k] {
		if time.Since(t) < 10*time.Minute {
			keep = append(keep, t)
		}
	}
	l.m[k] = keep
	return len(keep) >= 8
}
func (l *limiter) fail(k string)  { l.mu.Lock(); l.m[k] = append(l.m[k], time.Now()); l.mu.Unlock() }
func (l *limiter) reset(k string) { l.mu.Lock(); delete(l.m, k); l.mu.Unlock() }

// ---------- Anzeigehilfen ----------

func relTime(t time.Time) string {
	d := time.Since(t)
	neg := d < 0
	if neg {
		d = -d
	}
	var s string
	switch {
	case d < time.Minute:
		return "gerade eben"
	case d < time.Hour:
		s = fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		s = fmt.Sprintf("%d h", int(d.Hours()))
	default:
		s = fmt.Sprintf("%d Tagen", int(d.Hours()/24))
	}
	if neg {
		return "in " + s
	}
	return "vor " + s
}

func stateLabel(st string) string {
	if st == "versendet" {
		st = "erledigt"
	}
	return ui.T("state." + st)
}

func stateClass(st string) string {
	switch st {
	case "erledigt", "freigegeben", "versendet":
		return "ok"
	case "abgelehnt", "abgebrochen":
		return "bad"
	case "rueckfrage", "geplant", "extern":
		return "warn"
	case "offen":
		return "open"
	}
	return "muted"
}

func statusLabel(st string) string { return ui.T("status." + st) }

func kindLabel(k string) string { return ui.T("kind." + k) }

func kindIcon(k string) string {
	for _, x := range flow.StepKinds {
		if x.Key == k {
			return x.Icon
		}
	}
	return "•"
}

func initials(n string) string {
	n = strings.TrimSpace(strings.Split(n, "(")[0])
	if strings.Contains(n, ",") {
		p := strings.SplitN(n, ",", 2)
		n = strings.TrimSpace(p[1]) + " " + strings.TrimSpace(p[0])
	}
	f := strings.Fields(n)
	out := ""
	for i, x := range f {
		if i == 0 || i == len(f)-1 {
			r := []rune(x)
			out += strings.ToUpper(string(r[0]))
		}
	}
	if out == "" {
		return "?"
	}
	return out
}

var boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
var codeRe = regexp.MustCompile("`([^`]+)`")

// md wandelt einfachen Text (Zeilen, **fett**, `code`) in sicheres HTML.
func md(s string) template.HTML {
	s = template.HTMLEscapeString(strings.TrimSpace(s))
	s = boldRe.ReplaceAllString(s, "<b>$1</b>")
	s = codeRe.ReplaceAllString(s, "<code>$1</code>")
	s = strings.ReplaceAll(s, "\n", "<br>")
	return template.HTML(s)
}
