// Package glpi spricht die moderne GLPI-API (High-Level-API v2, ab GLPI 11) mit OAuth2 an.
// Die alte REST-API (apirest.php, Session-Token) wird bewusst nicht verwendet.
//
// Endpunkte (relativ zu <URL><APIPath>, Standard /api.php):
//
//	POST /token                                   OAuth2 (password oder client_credentials)
//	POST /Assistance/{Ticket|Change}              anlegen, GET/PATCH /{id}
//	POST /Assistance/{type}/{id}/TeamMember       {type: User|Group|Supplier, id, role: requester|assigned|observer}
//	POST /Assistance/{type}/{id}/Timeline/{Followup|Task|Validation|Solution}
//	GET  /Administration/{User|Group|Entity}, /Management/Supplier, /Dropdowns/{ITILCategory|Location|…}  (filter= RSQL)
package glpi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Conn ist eine GLPI-Verbindung (gespeichert als Dokument "glpi").
type Conn struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	URL           string `json:"url"`      // https://glpi.stadt.local
	APIPath       string `json:"api_path"` // Standard /api.php (optional mit Version, z. B. /api.php/v2.2)
	Grant         string `json:"grant"`    // password (Standard) oder client_credentials
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Scope         string `json:"scope"` // Standard "api"
	Language      string `json:"language"`
	Insecure      bool   `json:"insecure"`
	Entity        string `json:"entity"`         // Standard-Entität (ID oder vollständiger Name)
	WebhookSecret string `json:"webhook_secret"` // Geheimnis der GLPI-Webhooks (X-GLPI-signature)
	PollMinutes   int    `json:"poll_minutes"`   // Status abfragen alle … Minuten (Standard 5, 0 = nur Webhook)
}

// Base liefert die API-Basisadresse.
func (c Conn) Base() string {
	p := c.APIPath
	if p == "" {
		p = "/api.php"
	}
	return strings.TrimRight(c.URL, "/") + "/" + strings.Trim(p, "/")
}

// FormURL ist die Adresse eines Tickets/Changes in der GLPI-Oberfläche.
func (c Conn) FormURL(itemtype string, id int64) string {
	return fmt.Sprintf("%s/front/%s.form.php?id=%d", strings.TrimRight(c.URL, "/"), strings.ToLower(itemtype), id)
}

// Error ist eine Fehlermeldung der API.
type Error struct {
	Status int
	Title  string
	Detail string
	Path   string
}

func (e *Error) Error() string {
	s := fmt.Sprintf("GLPI %d", e.Status)
	if e.Title != "" {
		s += ": " + e.Title
	}
	if e.Detail != "" && e.Detail != e.Title {
		s += " – " + e.Detail
	}
	return s + " (" + e.Path + ")"
}

// Client ist ein Zugang zu einer Verbindung (Token wird zwischengespeichert).
type Client struct {
	Conn  Conn
	HTTP  *http.Client
	mu    sync.Mutex
	token string
	exp   time.Time
	cache map[string]cached
}

type cached struct {
	id  int64
	exp time.Time
}

var (
	clientsMu sync.Mutex
	clients   = map[string]*Client{}
)

// For liefert den (zwischengespeicherten) Client einer Verbindung; geänderte Einstellungen ergeben einen neuen.
func For(c Conn) *Client {
	b, _ := json.Marshal(c)
	k := string(b)
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if cl, ok := clients[k]; ok {
		return cl
	}
	cl := New(c)
	clients[k] = cl
	return cl
}

// New legt einen Client an.
func New(c Conn) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if c.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // bewusst konfigurierbar (interne CA)
	}
	return &Client{Conn: c, HTTP: &http.Client{Timeout: 20 * time.Second, Transport: tr}, cache: map[string]cached{}}
}

// ---------- OAuth2 ----------

func (cl *Client) accessToken(ctx context.Context, force bool) (string, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if !force && cl.token != "" && time.Now().Before(cl.exp) {
		return cl.token, nil
	}
	c := cl.Conn
	if c.URL == "" || c.ClientID == "" {
		return "", errors.New("GLPI-Verbindung unvollständig: URL und Client-ID angeben")
	}
	f := url.Values{}
	grant := c.Grant
	if grant == "" {
		grant = "password"
	}
	f.Set("grant_type", grant)
	f.Set("client_id", c.ClientID)
	f.Set("client_secret", c.ClientSecret)
	scope := c.Scope
	if scope == "" {
		scope = "api"
	}
	f.Set("scope", scope)
	if grant == "password" {
		f.Set("username", c.Username)
		f.Set("password", c.Password)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base()+"/token", strings.NewReader(f.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := cl.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("GLPI nicht erreichbar: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != 200 {
		return "", apiError(res.StatusCode, body, "/token")
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.AccessToken == "" {
		return "", fmt.Errorf("GLPI /token: unerwartete Antwort")
	}
	if t.ExpiresIn <= 0 {
		t.ExpiresIn = 3600
	}
	cl.token = t.AccessToken
	cl.exp = time.Now().Add(time.Duration(t.ExpiresIn)*time.Second - time.Minute)
	return cl.token, nil
}

func apiError(status int, body []byte, path string) error {
	e := &Error{Status: status, Path: path}
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		e.Title = str(firstOf(m, "title", "error_description", "message", "error", "status"))
		e.Detail = str(firstOf(m, "detail", "hint"))
		if ad, ok := m["additional_messages"].([]any); ok {
			for _, x := range ad {
				if mm, ok := x.(map[string]any); ok {
					e.Detail = strings.TrimSpace(e.Detail + " " + str(mm["message"]))
				}
			}
		}
	} else {
		t := strings.TrimSpace(string(body))
		if len(t) > 200 {
			t = t[:200] + "…"
		}
		e.Title = t
	}
	return e
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil && v != "" {
			return v
		}
	}
	return nil
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

// Do führt eine Anfrage aus (Token wird bei 401 einmal erneuert). out darf nil sein.
func (cl *Client) Do(ctx context.Context, method, path string, in, out any) (http.Header, error) {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := cl.accessToken(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		var rd io.Reader
		if in != nil {
			b, err := json.Marshal(in)
			if err != nil {
				return nil, err
			}
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, cl.Conn.Base()+path, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		if cl.Conn.Language != "" {
			req.Header.Set("Accept-Language", cl.Conn.Language)
		}
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := cl.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GLPI nicht erreichbar: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		if res.StatusCode >= 300 {
			return res.Header, apiError(res.StatusCode, body, method+" "+path)
		}
		if out != nil && len(bytes.TrimSpace(body)) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return res.Header, fmt.Errorf("GLPI %s: Antwort nicht lesbar: %w", path, err)
			}
		}
		return res.Header, nil
	}
	return nil, errors.New("GLPI: Anmeldung abgelehnt")
}

// ---------- Vorgänge ----------

// Itemtype normalisiert ticket/change.
func Itemtype(t string) string {
	switch strings.ToLower(t) {
	case "change":
		return "Change"
	case "problem":
		return "Problem"
	}
	return "Ticket"
}

// Create legt Ticket/Change an und liefert die ID.
func (cl *Client) Create(ctx context.Context, itemtype string, body map[string]any) (int64, error) {
	var out map[string]any
	h, err := cl.Do(ctx, http.MethodPost, "/Assistance/"+itemtype, body, &out)
	if err != nil {
		return 0, err
	}
	if id := toID(out["id"]); id > 0 {
		return id, nil
	}
	if loc := h.Get("Location"); loc != "" {
		if id, err := strconv.ParseInt(loc[strings.LastIndex(loc, "/")+1:], 10, 64); err == nil {
			return id, nil
		}
	}
	return 0, errors.New("GLPI hat keine ID zurückgegeben")
}

// Get lädt Ticket/Change.
func (cl *Client) Get(ctx context.Context, itemtype string, id int64) (map[string]any, error) {
	var out map[string]any
	_, err := cl.Do(ctx, http.MethodGet, fmt.Sprintf("/Assistance/%s/%d", itemtype, id), nil, &out)
	return out, err
}

// Update ändert Felder (PATCH).
func (cl *Client) Update(ctx context.Context, itemtype string, id int64, body map[string]any) error {
	_, err := cl.Do(ctx, http.MethodPatch, fmt.Sprintf("/Assistance/%s/%d", itemtype, id), body, nil)
	return err
}

// AddTeam fügt eine Person/Gruppe/Lieferanten mit Rolle hinzu.
func (cl *Client) AddTeam(ctx context.Context, itemtype string, id int64, memberType string, memberID int64, role string) error {
	_, err := cl.Do(ctx, http.MethodPost, fmt.Sprintf("/Assistance/%s/%d/TeamMember", itemtype, id),
		map[string]any{"type": memberType, "id": memberID, "role": role}, nil)
	return err
}

// AddTimeline legt einen Eintrag im Verlauf an: Followup, Task, Validation, Solution.
func (cl *Client) AddTimeline(ctx context.Context, itemtype string, id int64, sub string, body map[string]any) (int64, error) {
	var out map[string]any
	_, err := cl.Do(ctx, http.MethodPost, fmt.Sprintf("/Assistance/%s/%d/Timeline/%s", itemtype, id, sub), body, &out)
	return toID(out["id"]), err
}

// Timeline liest Einträge eines Typs (Followup, Solution, Task, Validation).
func (cl *Client) Timeline(ctx context.Context, itemtype string, id int64, sub string) ([]map[string]any, error) {
	var out []map[string]any
	_, err := cl.Do(ctx, http.MethodGet, fmt.Sprintf("/Assistance/%s/%d/Timeline/%s", itemtype, id, sub), nil, &out)
	return out, err
}

// Status prüft Erreichbarkeit und Anmeldung.
func (cl *Client) Status(ctx context.Context) (map[string]any, error) {
	if _, err := cl.accessToken(ctx, true); err != nil {
		return nil, err
	}
	var out map[string]any
	if _, err := cl.Do(ctx, http.MethodGet, "/status", nil, &out); err != nil {
		var lst []any
		if _, err2 := cl.Do(ctx, http.MethodGet, "/status", nil, &lst); err2 != nil {
			return nil, err
		}
		return map[string]any{"status": lst}, nil
	}
	return out, nil
}

// ---------- Nachschlagen (Name → ID) ----------

// Lookup beschreibt, wo ein Objekttyp gesucht wird.
type Lookup struct {
	Kind   string   `json:"kind"`
	Label  string   `json:"label"`
	Path   string   `json:"path"`
	Fields []string `json:"-"` // Suchfelder für den Namen
}

// Lookups sind die nachschlagbaren Objekttypen.
var Lookups = []Lookup{
	{"User", "Benutzer", "/Administration/User", []string{"username", "name"}},
	{"Group", "Gruppe", "/Administration/Group", []string{"completename", "name"}},
	{"Supplier", "Lieferant", "/Management/Supplier", []string{"name"}},
	{"Entity", "Entität", "/Administration/Entity", []string{"completename", "name"}},
	{"ITILCategory", "Kategorie", "/Dropdowns/ITILCategory", []string{"completename", "name", "code"}},
	{"Location", "Ort", "/Dropdowns/Location", []string{"completename", "name", "code"}},
	{"RequestType", "Anfragequelle", "/Dropdowns/RequestType", []string{"name"}},
	{"TaskCategory", "Aufgabenkategorie", "/Dropdowns/TaskCategory", []string{"completename", "name"}},
	{"SolutionType", "Lösungsart", "/Dropdowns/SolutionType", []string{"name"}},
	{"TaskTemplate", "Aufgabenvorlage", "/Dropdowns/TaskTemplate", []string{"name"}},
	{"ValidationTemplate", "Genehmigungsvorlage", "/Dropdowns/ValidationTemplate", []string{"name"}},
}

func lookupFor(kind string) (Lookup, bool) {
	for _, l := range Lookups {
		if strings.EqualFold(l.Kind, kind) {
			return l, true
		}
	}
	return Lookup{}, false
}

func rsqlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Search listet Objekte eines Typs (q leer = alle, sonst Teilsuche im Namen).
func (cl *Client) Search(ctx context.Context, kind, q string, limit int) ([]map[string]any, error) {
	l, ok := lookupFor(kind)
	if !ok {
		return nil, fmt.Errorf("unbekannter Objekttyp %q", kind)
	}
	if limit <= 0 {
		limit = 50
	}
	v := url.Values{}
	v.Set("limit", strconv.Itoa(limit))
	if q = strings.TrimSpace(q); q != "" {
		var parts []string
		for _, f := range l.Fields {
			parts = append(parts, f+"=like="+rsqlQuote("*"+q+"*"))
		}
		v.Set("filter", strings.Join(parts, ","))
	}
	var out []map[string]any
	_, err := cl.Do(ctx, http.MethodGet, l.Path+"?"+v.Encode(), nil, &out)
	return out, err
}

// Resolve liefert die ID zu „12“, „#12“ oder einem Namen (exakt; Ergebnis 10 min zwischengespeichert).
func (cl *Client) Resolve(ctx context.Context, kind, value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if id, err := strconv.ParseInt(strings.TrimPrefix(value, "#"), 10, 64); err == nil {
		return id, nil
	}
	ck := strings.ToLower(kind + "\x00" + value)
	cl.mu.Lock()
	if c, ok := cl.cache[ck]; ok && time.Now().Before(c.exp) {
		cl.mu.Unlock()
		return c.id, nil
	}
	cl.mu.Unlock()
	l, ok := lookupFor(kind)
	if !ok {
		return 0, fmt.Errorf("unbekannter Objekttyp %q", kind)
	}
	var parts []string
	for _, f := range l.Fields {
		parts = append(parts, f+"=="+rsqlQuote(value))
	}
	v := url.Values{}
	v.Set("filter", strings.Join(parts, ","))
	v.Set("limit", "5")
	var out []map[string]any
	if _, err := cl.Do(ctx, http.MethodGet, l.Path+"?"+v.Encode(), nil, &out); err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, fmt.Errorf("%s „%s“ in GLPI nicht gefunden", l.Label, value)
	}
	id := toID(out[0]["id"])
	cl.mu.Lock()
	cl.cache[ck] = cached{id: id, exp: time.Now().Add(10 * time.Minute)}
	cl.mu.Unlock()
	return id, nil
}

func toID(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	case map[string]any:
		return toID(x["id"])
	}
	return 0
}

// ID liest eine ID aus einem Wert (Zahl, Text oder Objekt mit id).
func ID(v any) int64 { return toID(v) }

// ---------- Status ----------

// StatusName sind die GLPI-Status (deutsche Bezeichnungen) je Art.
var StatusName = map[string]map[int]string{
	"Ticket": {1: "Neu", 2: "In Bearbeitung (zugewiesen)", 3: "In Bearbeitung (geplant)", 4: "Wartend", 5: "Gelöst", 6: "Geschlossen"},
	"Change": {1: "Neu", 9: "Bewertung", 10: "Genehmigung", 7: "Akzeptiert", 4: "Wartend", 11: "Test", 12: "Qualifizierung",
		5: "Angewendet", 8: "Überprüfung", 6: "Geschlossen", 13: "Abgelehnt", 14: "Abgebrochen"},
}

// StatusOrder ist die Reihenfolge für Auswahllisten.
var StatusOrder = map[string][]int{
	"Ticket": {1, 2, 3, 4, 5, 6},
	"Change": {1, 9, 10, 7, 4, 11, 12, 5, 8, 6, 13, 14},
}

// DefaultDone sind die Status, bei denen „bis gelöst“ als erledigt gilt.
var DefaultDone = map[string][]int{"Ticket": {5, 6}, "Change": {5, 8, 6}}

// DefaultFail sind Status, die als gescheitert gelten.
var DefaultFail = map[string][]int{"Ticket": {}, "Change": {13, 14}}

// ItemStatus liest den Status (id, Name) aus einem Ticket/Change.
func ItemStatus(itemtype string, item map[string]any) (int, string) {
	var id int
	var name string
	switch s := item["status"].(type) {
	case map[string]any:
		id = int(toID(s["id"]))
		name = str(s["name"])
	default:
		id = int(toID(s))
	}
	if name == "" {
		name = StatusName[itemtype][id]
	}
	return id, name
}
