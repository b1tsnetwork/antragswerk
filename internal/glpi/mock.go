package glpi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mock ist ein kleines Test-GLPI mit der High-Level-API v2 (Ausschnitt) und einer Bedienoberfläche.
// Es dient den Tests und dem Demo-Modus (eingehängt unter /demo-glpi). Daten nur im Speicher.
type Mock struct {
	mu        sync.Mutex
	Prefix    string // z. B. /demo-glpi
	Webhook   string // Ziel-URL für Webhooks (leer = keine)
	Secret    string
	ClientID  string
	nextID    int64
	items     map[string]map[int64]*mockItem
	Requests  []string // Protokoll „METHODE Pfad“
	FailNext  int      // die nächsten n Anlage-Anfragen mit 500 beantworten
	lookups   map[string][]map[string]any
	webhookCl *http.Client
}

type mockItem struct {
	Type     string
	Data     map[string]any
	Team     []map[string]any
	Timeline map[string][]map[string]any
}

// NewMock legt ein Test-GLPI mit Beispiel-Stammdaten an.
func NewMock(prefix string) *Mock {
	return &Mock{Prefix: strings.TrimRight(prefix, "/"), ClientID: "antragswerk", nextID: 100, items: map[string]map[int64]*mockItem{"Ticket": {}, "Change": {}},
		webhookCl: &http.Client{Timeout: 5 * time.Second},
		lookups: map[string][]map[string]any{
			"Administration/User":    {{"id": 2, "username": "glpi", "name": "glpi"}, {"id": 7, "username": "t.klein", "name": "Klein Thomas"}, {"id": 8, "username": "j.hoffmann", "name": "Hoffmann Julia"}, {"id": 9, "username": "a.becker", "name": "Becker Anna"}},
			"Administration/Group":   {{"id": 3, "name": "IT-Benutzerverwaltung", "completename": "IT > IT-Benutzerverwaltung"}, {"id": 4, "name": "IT-Arbeitsplatz", "completename": "IT > IT-Arbeitsplatz"}, {"id": 5, "name": "Change Advisory Board", "completename": "IT > Change Advisory Board"}},
			"Administration/Entity":  {{"id": 0, "name": "Stammentität", "completename": "Stammentität"}, {"id": 1, "name": "Stadtverwaltung", "completename": "Stammentität > Stadtverwaltung"}},
			"Management/Supplier":    {{"id": 1, "name": "Systemhaus Muster GmbH"}},
			"Dropdowns/ITILCategory": {{"id": 10, "name": "Benutzerkonten", "completename": "IT > Benutzerkonten"}, {"id": 11, "name": "Berechtigungen", "completename": "IT > Berechtigungen"}, {"id": 12, "name": "Hardware", "completename": "IT > Hardware"}, {"id": 13, "name": "Infrastruktur-Änderung", "completename": "IT > Infrastruktur-Änderung"}},
			"Dropdowns/Location":     {{"id": 20, "name": "Rathaus A", "completename": "Rathaus A"}, {"id": 21, "name": "Rathaus B", "completename": "Rathaus B"}},
			"Dropdowns/RequestType":  {{"id": 1, "name": "Helpdesk"}, {"id": 7, "name": "Antragswerk"}},
			"Dropdowns/TaskCategory": {{"id": 30, "name": "Konto", "completename": "Konto"}, {"id": 31, "name": "Hardware", "completename": "Hardware"}},
			"Dropdowns/SolutionType": {{"id": 1, "name": "Umgesetzt"}},
			"Dropdowns/TaskTemplate": {{"id": 1, "name": "Standard"}}, "Dropdowns/ValidationTemplate": {{"id": 1, "name": "Standard"}},
		}}
}

var mockAPI = regexp.MustCompile(`^/api\.php(?:/v[0-9.]+)?(/.*)$`)

func (m *Mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, m.Prefix)
	if mm := mockAPI.FindStringSubmatch(p); mm != nil {
		m.api(w, r, mm[1])
		return
	}
	m.ui(w, r, p)
}

func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiErr(w http.ResponseWriter, code int, title string) {
	jsonOut(w, code, map[string]any{"status": "ERROR", "title": title, "detail": nil})
}

func (m *Mock) api(w http.ResponseWriter, r *http.Request, p string) {
	m.mu.Lock()
	m.Requests = append(m.Requests, r.Method+" "+p)
	m.mu.Unlock()
	if p == "/token" && r.Method == http.MethodPost {
		_ = r.ParseForm()
		if r.FormValue("client_id") != m.ClientID {
			jsonOut(w, 401, map[string]any{"error": "invalid_client", "error_description": "Client authentication failed"})
			return
		}
		jsonOut(w, 200, map[string]any{"token_type": "Bearer", "expires_in": 3600, "access_token": "mock-" + strconv.FormatInt(time.Now().UnixNano(), 36)})
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer mock-") {
		apiErr(w, 401, "You are not authenticated")
		return
	}
	if p == "/status" {
		jsonOut(w, 200, map[string]any{"glpi": map[string]any{"status": "OK"}, "mock": true})
		return
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 2 && parts[0] != "Assistance" && r.Method == http.MethodGet {
		m.search(w, r, parts[0]+"/"+parts[1])
		return
	}
	if len(parts) < 2 || parts[0] != "Assistance" || (parts[1] != "Ticket" && parts[1] != "Change") {
		apiErr(w, 404, "Endpoint not found")
		return
	}
	typ := parts[1]
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(parts) == 2 && r.Method == http.MethodPost {
		if m.FailNext > 0 {
			m.FailNext--
			apiErr(w, 500, "Simulierter Fehler")
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || str(body["name"]) == "" {
			apiErr(w, 400, "Invalid parameters: name is required")
			return
		}
		m.nextID++
		id := m.nextID
		body["id"] = id
		st := int(toID(body["status"]))
		if st == 0 {
			st = 1
		}
		body["status"] = map[string]any{"id": st, "name": StatusName[typ][st]}
		body["date_creation"] = time.Now().Format("2006-01-02 15:04:05")
		m.items[typ][id] = &mockItem{Type: typ, Data: body, Timeline: map[string][]map[string]any{}}
		w.Header().Set("Location", fmt.Sprintf("/api.php/Assistance/%s/%d", typ, id))
		jsonOut(w, 201, map[string]any{"id": id, "href": fmt.Sprintf("/Assistance/%s/%d", typ, id)})
		return
	}
	if len(parts) < 3 {
		apiErr(w, 405, "Method not allowed")
		return
	}
	id, _ := strconv.ParseInt(parts[2], 10, 64)
	it := m.items[typ][id]
	if it == nil {
		apiErr(w, 404, "Item not found")
		return
	}
	switch {
	case len(parts) == 3 && r.Method == http.MethodGet:
		out := map[string]any{}
		for k, v := range it.Data {
			out[k] = v
		}
		out["team"] = it.Team
		jsonOut(w, 200, out)
	case len(parts) == 3 && r.Method == http.MethodPatch:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for k, v := range body {
			it.Data[k] = v
		}
		if s, ok := body["status"]; ok {
			st := int(toID(s))
			it.Data["status"] = map[string]any{"id": st, "name": StatusName[typ][st]}
		}
		jsonOut(w, 200, map[string]any{"id": id})
	case len(parts) == 4 && parts[3] == "TeamMember" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		it.Team = append(it.Team, body)
		jsonOut(w, 201, map[string]any{"id": toID(body["id"])})
	case len(parts) == 5 && parts[3] == "Timeline":
		sub := parts[4]
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.nextID++
			body["id"] = m.nextID
			body["date_creation"] = time.Now().Format("2006-01-02 15:04:05")
			it.Timeline[sub] = append(it.Timeline[sub], body)
			jsonOut(w, 201, map[string]any{"id": m.nextID})
			return
		}
		lst := it.Timeline[sub]
		if lst == nil {
			lst = []map[string]any{}
		}
		jsonOut(w, 200, lst)
	default:
		apiErr(w, 404, "Endpoint not found")
	}
}

var mockFilter = regexp.MustCompile(`(\w+)(==|=like=)"((?:[^"\\]|\\.)*)"`)

func (m *Mock) search(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	list, ok := m.lookups[path]
	m.mu.Unlock()
	if !ok {
		apiErr(w, 404, "Endpoint not found")
		return
	}
	f := r.URL.Query().Get("filter")
	out := []map[string]any{}
	for _, o := range list {
		if f == "" {
			out = append(out, o)
			continue
		}
		for _, c := range mockFilter.FindAllStringSubmatch(f, -1) {
			v := strings.ToLower(str(o[c[1]]))
			q := strings.ToLower(strings.ReplaceAll(c[3], `\"`, `"`))
			if (c[2] == "==" && v == q) || (c[2] == "=like=" && strings.Contains(v, strings.Trim(q, "*"))) {
				out = append(out, o)
				break
			}
		}
	}
	jsonOut(w, 200, out)
}

// Resolve löst ein Ticket/Change (Status, Lösung) – wie ein Techniker in GLPI.
func (m *Mock) Resolve(typ string, id int64, status int, solution string) error {
	m.mu.Lock()
	it := m.items[typ][id]
	if it == nil {
		m.mu.Unlock()
		return fmt.Errorf("%s %d unbekannt", typ, id)
	}
	if solution != "" {
		m.nextID++
		it.Timeline["Solution"] = append(it.Timeline["Solution"], map[string]any{"id": m.nextID, "content": solution, "date_creation": time.Now().Format("2006-01-02 15:04:05")})
	}
	it.Data["status"] = map[string]any{"id": status, "name": StatusName[typ][status]}
	payload := map[string]any{"event": "update", "item": it.Data}
	hook, secret := m.Webhook, m.Secret
	m.mu.Unlock()
	if hook != "" {
		go m.sendWebhook(hook, secret, payload)
	}
	return nil
}

// Followup schreibt einen Folgeeintrag (z. B. Rückfrage des Technikers).
func (m *Mock) Followup(typ string, id int64, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if it := m.items[typ][id]; it != nil {
		m.nextID++
		it.Timeline["Followup"] = append(it.Timeline["Followup"], map[string]any{"id": m.nextID, "content": text, "date_creation": time.Now().Format("2006-01-02 15:04:05")})
	}
}

func (m *Mock) sendWebhook(url, secret string, payload any) {
	b, _ := json.MarshalIndent(payload, "", "  ")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GLPI-timestamp", ts)
	if secret != "" {
		h := hmac.New(sha256.New, []byte(secret))
		h.Write(append(b, ts...))
		req.Header.Set("X-GLPI-signature", hex.EncodeToString(h.Sum(nil)))
	}
	if res, err := m.webhookCl.Do(req); err == nil {
		res.Body.Close()
	}
}

// Item liefert eine Kopie (Tests).
func (m *Mock) Item(typ string, id int64) (map[string]any, []map[string]any, map[string][]map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it := m.items[typ][id]
	if it == nil {
		return nil, nil, nil
	}
	return it.Data, it.Team, it.Timeline
}

// Last liefert die zuletzt angelegte ID je Art.
func (m *Mock) Last(typ string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best int64
	for id := range m.items[typ] {
		if id > best {
			best = id
		}
	}
	return best
}

// ---------- Oberfläche ----------

func (m *Mock) ui(w http.ResponseWriter, r *http.Request, p string) {
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		typ := r.FormValue("type")
		id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
		switch p {
		case "/resolve":
			st, _ := strconv.Atoi(r.FormValue("status"))
			_ = m.Resolve(typ, id, st, r.FormValue("solution"))
		case "/followup":
			m.Followup(typ, id, r.FormValue("text"))
		}
		http.Redirect(w, r, m.Prefix+"/", http.StatusSeeOther)
		return
	}
	m.mu.Lock()
	type row struct {
		typ string
		it  *mockItem
	}
	var rows []row
	for typ, mm := range m.items {
		for _, it := range mm {
			rows = append(rows, row{typ, it})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return toID(rows[i].it.Data["id"]) > toID(rows[j].it.Data["id"]) })
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Test-GLPI</title><style>
body{font:14px/1.5 system-ui,sans-serif;margin:0;background:#f2f4f7;color:#1d2433}header{background:#2f3f64;color:#fff;padding:12px 24px}header b{font-size:17px}main{max-width:1000px;margin:0 auto;padding:20px}
.it{background:#fff;border:1px solid #d9dee8;border-radius:10px;padding:16px 18px;margin-bottom:16px}.it h2{margin:0 0 6px;font-size:16px}.meta{color:#5b6475;font-size:12.5px;margin-bottom:10px}
.content{border-left:3px solid #c9d2e3;padding:4px 12px;margin:8px 0;background:#fafbfd}.tag{display:inline-block;background:#e8edf7;border-radius:6px;padding:1px 7px;margin-right:4px;font-size:12px}
form{display:flex;gap:8px;flex-wrap:wrap;align-items:flex-start;margin-top:8px}textarea{flex:1;min-width:260px;font:inherit;padding:6px;border:1px solid #c5ccd9;border-radius:6px}select,button{font:inherit;padding:6px 10px;border-radius:6px;border:1px solid #c5ccd9;background:#fff}button{background:#2f3f64;color:#fff;border:0;cursor:pointer}
.empty{color:#5b6475;text-align:center;padding:40px}code{background:#eef1f6;padding:1px 4px;border-radius:4px}</style></head><body><header><b>Test-GLPI</b> · nur Demo – simuliert die GLPI-API v2</header><main>`)
	if len(rows) == 0 {
		b.WriteString(`<div class="empty">Noch keine Tickets oder Changes. Stelle im Antragswerk einen Antrag mit GLPI-Schritt.</div>`)
	}
	for _, x := range rows {
		d := x.it.Data
		id := toID(d["id"])
		st, sn := ItemStatus(x.typ, d)
		fmt.Fprintf(&b, `<div class="it"><h2>%s #%d – %s</h2><div class="meta"><span class="tag">%s</span>`, x.typ, id, html.EscapeString(str(d["name"])), html.EscapeString(sn))
		for _, k := range []string{"type", "urgency", "impact", "priority", "category", "entity", "location", "request_type", "external_id", "resolution_date"} {
			if v, ok := d[k]; ok && v != nil && str(v) != "" {
				vv := v
				if mm, ok := v.(map[string]any); ok {
					vv = firstOf(mm, "id", "completename")
				}
				fmt.Fprintf(&b, `<span class="tag">%s: %s</span>`, k, html.EscapeString(str(vv)))
			}
		}
		for _, t := range x.it.Team {
			fmt.Fprintf(&b, `<span class="tag">%s %s #%d</span>`, html.EscapeString(str(t["role"])), html.EscapeString(str(t["type"])), toID(t["id"]))
		}
		b.WriteString(`</div><div class="content">` + str(d["content"]) + `</div>`)
		for _, sub := range []string{"Task", "Validation", "Followup", "Solution"} {
			for _, e := range x.it.Timeline[sub] {
				txt := str(firstOf(e, "content", "submission_comment"))
				fmt.Fprintf(&b, `<div class="meta"><b>%s</b> %s</div>`, sub, txt)
			}
		}
		if extra := extraFields(d); extra != "" {
			fmt.Fprintf(&b, `<div class="meta">Weitere Felder: <code>%s</code></div>`, html.EscapeString(extra))
		}
		done := DefaultDone[x.typ]
		fmt.Fprintf(&b, `<form method="post" action="%s/resolve"><input type="hidden" name="type" value="%s"><input type="hidden" name="id" value="%d"><textarea name="solution" rows="2" placeholder="Lösung, z. B. Benutzername: jmueller"></textarea><select name="status">`, m.Prefix, x.typ, id)
		for _, s := range StatusOrder[x.typ] {
			sel := ""
			if s == done[0] {
				sel = " selected"
			}
			fmt.Fprintf(&b, `<option value="%d"%s>%s</option>`, s, sel, StatusName[x.typ][s])
		}
		_ = st
		b.WriteString(`</select><button>Status setzen</button></form>`)
		fmt.Fprintf(&b, `<form method="post" action="%s/followup"><input type="hidden" name="type" value="%s"><input type="hidden" name="id" value="%d"><textarea name="text" rows="1" placeholder="Folgeeintrag"></textarea><button>Folgeeintrag</button></form></div>`, m.Prefix, x.typ, id)
	}
	m.mu.Unlock()
	b.WriteString(`</main></body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func extraFields(d map[string]any) string {
	known := map[string]bool{"id": true, "name": true, "content": true, "status": true, "date_creation": true, "type": true, "urgency": true, "impact": true, "priority": true,
		"category": true, "entity": true, "location": true, "request_type": true, "external_id": true, "resolution_date": true}
	var keys []string
	for k := range d {
		if !known[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(d[k])
		s := strings.TrimSpace(buf.String())
		if len(s) > 60 {
			s = s[:60] + "…"
		}
		parts = append(parts, k+"="+s)
	}
	return strings.Join(parts, " · ")
}
