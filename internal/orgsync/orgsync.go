// Package orgsync übernimmt Organisationsdaten (Einheiten, Personen, Räume) aus externen JSON-Quellen –
// z. B. dem internen Telefonbuch – in Kataloge und das Personenverzeichnis. So entfällt doppelte Datenpflege.
package orgsync

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"antragswerk/internal/flow"
	"antragswerk/internal/store"
)

// Source ist eine Datenquelle (gespeichert als Dokument "source").
type Source struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Kind     string   `json:"kind"`   // url, file
	URL      string   `json:"url"`    // URL oder Dateipfad
	Header   string   `json:"header"` // zusätzliche Kopfzeilen "Name: Wert" (eine je Zeile), z. B. Authorization
	Insecure bool     `json:"insecure"`
	Interval int      `json:"interval"` // Minuten, 0 = nur manuell
	Targets  []Target `json:"targets"`
}

// Target bildet einen Teil der Quelle auf einen Katalog oder das Personenverzeichnis ab.
type Target struct {
	Kind     string            `json:"kind"`     // catalog, persons
	Catalog  string            `json:"catalog"`  // Zielkatalog
	Path     string            `json:"path"`     // Pfad zur Liste im JSON, z. B. "units" oder "data.items"; leer = Wurzel
	Children string            `json:"children"` // Feld mit Unterelementen (Hierarchie), z. B. "children"
	Key      string            `json:"key"`      // Vorlage für den Schlüssel, z. B. {{.id}}
	Label    string            `json:"label"`    // Vorlage für die Bezeichnung (leer: neue Einträge = Schlüssel, vorhandene behalten ihre)
	Group    string            `json:"group"`    // Vorlage für die Gruppe, z. B. {{.uebergeordnet.name}}
	Attrs    map[string]string `json:"attrs"`    // Attribut → Vorlage
	Filter   string            `json:"filter"`   // Vorlage; Ergebnis leer/"false"/"0" = Datensatz überspringen
	Missing  string            `json:"missing"`  // inactive (Standard), delete, keep
}

// PersonFields sind die Felder des Personenverzeichnisses.
var PersonFields = []struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}{
	{"sam", "Anmeldename"}, {"name", "Anzeigename"}, {"given", "Vorname"}, {"surname", "Nachname"}, {"mail", "Mail"},
	{"phone", "Telefon"}, {"department", "Abteilung"}, {"title", "Funktion"}, {"office", "Raum"}, {"company", "Organisation"}, {"manager", "Vorgesetzte/r"},
}

// Status ist das Ergebnis des letzten Laufs.
type Status struct {
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Message string    `json:"message"`
	Counts  []string  `json:"counts"`
}

// Row ist ein abgebildeter Datensatz (für die Vorschau).
type Row struct {
	Key   string            `json:"key"`
	Label string            `json:"label"`
	Group string            `json:"group"`
	Attrs map[string]string `json:"attrs"`
}

// TargetResult ist das Ergebnis je Ziel.
type TargetResult struct {
	Target  string   `json:"target"`
	Count   int      `json:"count"`
	Skipped int      `json:"skipped"`
	Rows    []Row    `json:"rows"`
	Fields  []string `json:"fields"` // Felder des ersten Datensatzes (Hilfe für die Zuordnung)
	Sample  string   `json:"sample"`
	Errors  []string `json:"errors"`
	Added   int      `json:"added"`
	Updated int      `json:"updated"`
	Retired int      `json:"retired"`
}

var client = &http.Client{Timeout: 25 * time.Second}

// Fetch lädt die Rohdaten.
func Fetch(ctx context.Context, s Source) (any, error) {
	var raw []byte
	switch s.Kind {
	case "file":
		b, err := os.ReadFile(s.URL)
		if err != nil {
			return nil, err
		}
		raw = b
	default:
		if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
			return nil, errors.New("URL muss mit http:// oder https:// beginnen")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Antragswerk-Orgsync")
		for _, l := range strings.Split(s.Header, "\n") {
			if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) != "" {
				req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
			}
		}
		c := client
		if s.Insecure {
			c = &http.Client{Timeout: client.Timeout, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // bewusst konfigurierbar
		}
		res, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d", res.StatusCode)
		}
		raw, err = io.ReadAll(io.LimitReader(res.Body, 30<<20))
		if err != nil {
			return nil, err
		}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("kein gültiges JSON: %w", err)
	}
	return v, nil
}

func at(v any, path string) any {
	if path == "" {
		return v
	}
	for _, p := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

type rec struct {
	m      map[string]any
	parent map[string]any
	path   []string
	level  int
}

// records sammelt die Datensätze (rekursiv über Unterelemente).
func records(v any, children string) []rec {
	lst, _ := v.([]any)
	var out []rec
	var walk func(items []any, parent map[string]any, path []string, level int)
	walk = func(items []any, parent map[string]any, path []string, level int) {
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, rec{m: m, parent: parent, path: path, level: level})
			if children != "" {
				if kids, ok := m[children].([]any); ok {
					name := fmt.Sprint(firstOf(m, "name", "label", "title", "id"))
					walk(kids, m, append(append([]string{}, path...), name), level+1)
				}
			}
		}
	}
	walk(lst, nil, nil, 0)
	return out
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return ""
}

func render(tpl string, r rec) (string, error) {
	if tpl == "" {
		return "", nil
	}
	ctx := make(map[string]any, len(r.m)+3)
	for k, v := range r.m {
		ctx[k] = v
	}
	if r.parent != nil {
		ctx["uebergeordnet"] = r.parent
	} else {
		ctx["uebergeordnet"] = map[string]any{}
	}
	ctx["pfad"] = strings.Join(r.path, " › ")
	ctx["ebene"] = r.level
	s, err := flow.Render(tpl, ctx, nil)
	if err != nil && strings.Contains(err.Error(), "nil pointer evaluating") {
		// verschachteltes Feld fehlt im Datensatz (z. B. {{.head.mail}} ohne head) → leer statt Fehler
		return "", nil
	}
	return strings.TrimSpace(s), err
}

// Map bildet die Daten eines Ziels ab.
func Map(data any, t Target) ([]Row, TargetResult) {
	res := TargetResult{Target: targetName(t)}
	list := at(data, t.Path)
	if _, ok := list.([]any); !ok {
		res.Errors = append(res.Errors, fmt.Sprintf("Unter Pfad %q liegt keine Liste.", t.Path))
		if m, ok := data.(map[string]any); ok {
			var keys []string
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			res.Fields = keys
		}
		return nil, res
	}
	rs := records(list, t.Children)
	if len(rs) > 0 {
		var keys []string
		for k := range rs[0].m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		res.Fields = keys
		b, _ := json.MarshalIndent(rs[0].m, "", "  ")
		res.Sample = string(b)
		if len(res.Sample) > 1500 {
			res.Sample = res.Sample[:1500] + "\n…"
		}
	}
	seen := map[string]bool{}
	var rows []Row
	errs := map[string]bool{}
	note := func(e error) {
		if e != nil && len(errs) < 5 && !errs[e.Error()] {
			errs[e.Error()] = true
			res.Errors = append(res.Errors, e.Error())
		}
	}
	for _, r := range rs {
		if t.Filter != "" {
			f, err := render(t.Filter, r)
			note(err)
			if f == "" || f == "false" || f == "0" || f == "<nil>" {
				res.Skipped++
				continue
			}
		}
		keyTpl := t.Key
		if t.Kind == "persons" {
			keyTpl = t.Attrs["sam"]
		}
		key, err := render(keyTpl, r)
		note(err)
		if key == "" || key == "<nil>" || seen[strings.ToLower(key)] {
			res.Skipped++
			continue
		}
		seen[strings.ToLower(key)] = true
		row := Row{Key: key, Attrs: map[string]string{}}
		row.Label, err = render(t.Label, r)
		note(err)
		row.Group, err = render(t.Group, r)
		note(err)
		for k, tpl := range t.Attrs {
			v, err := render(tpl, r)
			note(err)
			if v == "<nil>" {
				v = ""
			}
			row.Attrs[k] = v
		}
		if row.Label == "" {
			row.Label = row.Attrs["name"]
		}
		if row.Label == "" {
			row.Label = key
		}
		rows = append(rows, row)
	}
	res.Count = len(rows)
	return rows, res
}

func targetName(t Target) string {
	if t.Kind == "persons" {
		return "Personenverzeichnis"
	}
	return "Katalog " + t.Catalog
}

// Run lädt die Quelle und übernimmt (apply=true) oder zeigt nur die Vorschau.
func Run(ctx context.Context, st *store.Store, s Source, apply bool, by string) ([]TargetResult, error) {
	data, err := Fetch(ctx, s)
	if err != nil {
		return nil, err
	}
	var out []TargetResult
	for _, t := range s.Targets {
		rows, res := Map(data, t)
		if apply && len(res.Errors) == 0 {
			switch t.Kind {
			case "persons":
				err = applyPersons(ctx, st, s, rows, &res, by)
			default:
				err = applyCatalog(ctx, st, s, t, rows, &res, by)
			}
			if err != nil {
				res.Errors = append(res.Errors, err.Error())
			}
		}
		if len(res.Rows) == 0 {
			n := len(rows)
			if n > 8 {
				n = 8
			}
			res.Rows = rows[:n]
		}
		out = append(out, res)
	}
	return out, nil
}

func applyCatalog(ctx context.Context, st *store.Store, s Source, t Target, rows []Row, res *TargetResult, by string) error {
	cats, err := st.Catalogs(ctx)
	if err != nil {
		return err
	}
	var cat *flow.Catalog
	idx := 0
	for i := range cats {
		if cats[i].Key == t.Catalog {
			cat, idx = &cats[i], i
		}
	}
	if cat == nil {
		return fmt.Errorf("Katalog %q existiert nicht", t.Catalog)
	}
	if len(rows) == 0 {
		return errors.New("Quelle lieferte keine Datensätze – Katalog unverändert gelassen")
	}
	// Attribute der Zuordnung im Katalog bekannt machen
	for k := range t.Attrs {
		found := false
		for _, a := range cat.Attrs {
			found = found || a.Key == k
		}
		if !found {
			cat.Attrs = append(cat.Attrs, flow.AttrDef{Key: k, Label: k, Hint: "aus " + s.Name})
		}
	}
	pos := map[string]int{}
	for i, e := range cat.Entries {
		pos[strings.ToLower(e.Key)] = i
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[strings.ToLower(r.Key)] = true
		if i, ok := pos[strings.ToLower(r.Key)]; ok {
			e := &cat.Entries[i]
			if t.Label != "" { // ohne Vorlage bleibt die hier gepflegte Bezeichnung
				e.Label = r.Label
			}
			if t.Group != "" {
				e.Group = r.Group
			}
			if e.Attrs == nil {
				e.Attrs = map[string]string{}
			}
			for k, v := range r.Attrs {
				e.Attrs[k] = v
			}
			e.Src, e.Inactive = s.Key, false
			res.Updated++
			continue
		}
		e := flow.Entry{Key: r.Key, Label: r.Label, Group: r.Group, Attrs: map[string]string{}, Src: s.Key}
		for k, v := range r.Attrs {
			e.Attrs[k] = v
		}
		cat.Entries = append(cat.Entries, e)
		res.Added++
	}
	var keep []flow.Entry
	for _, e := range cat.Entries {
		if e.Src == s.Key && !got[strings.ToLower(e.Key)] {
			switch t.Missing {
			case "delete":
				res.Retired++
				continue
			case "keep":
			default:
				if !e.Inactive {
					e.Inactive = true
					res.Retired++
				}
			}
		}
		keep = append(keep, e)
	}
	cat.Entries = keep
	return st.PutDoc(ctx, "catalog", cat.Key, cat, by, idx)
}

func applyPersons(ctx context.Context, st *store.Store, s Source, rows []Row, res *TargetResult, by string) error {
	if len(rows) == 0 {
		return errors.New("Quelle lieferte keine Personen – Verzeichnis unverändert gelassen")
	}
	keys := make([]string, 0, len(rows))
	vals := make([]any, 0, len(rows))
	for _, r := range rows {
		p := map[string]string{"sam": r.Key}
		for k, v := range r.Attrs {
			p[k] = v
		}
		if p["name"] == "" {
			p["name"] = r.Label
		}
		keys = append(keys, strings.ToLower(r.Key))
		vals = append(vals, p)
	}
	before := st.CountDocs(ctx, "person:"+s.Key)
	if err := st.ReplaceDocs(ctx, "person:"+s.Key, keys, vals, by); err != nil {
		return err
	}
	res.Updated = len(rows)
	if before > len(rows) {
		res.Retired = before - len(rows)
	}
	return nil
}

// ---------- Speicher & Zeitplan ----------

func Sources(ctx context.Context, st *store.Store) []Source {
	var out []Source
	for _, raw := range st.RawDocs(ctx, "source") {
		var s Source
		if json.Unmarshal([]byte(raw), &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

func LastStatus(ctx context.Context, st *store.Store, key string) *Status {
	var s Status
	if st.GetDoc(ctx, "sourcestate", key, &s) {
		return &s
	}
	return nil
}

var running sync.Mutex

// Sync führt eine Quelle aus und speichert den Status.
func Sync(ctx context.Context, st *store.Store, s Source, by string) ([]TargetResult, Status) {
	running.Lock()
	defer running.Unlock()
	res, err := Run(ctx, st, s, true, by)
	stt := Status{At: time.Now(), OK: err == nil}
	if err != nil {
		stt.Message = err.Error()
	}
	for _, r := range res {
		if len(r.Errors) > 0 {
			stt.OK = false
			stt.Message = strings.Join(r.Errors, "; ")
		}
		stt.Counts = append(stt.Counts, fmt.Sprintf("%s: %d (neu %d, aktualisiert %d, entfernt/inaktiv %d)", r.Target, r.Count, r.Added, r.Updated, r.Retired))
	}
	_ = st.PutDoc(ctx, "sourcestate", s.Key, stt, by, 0)
	st.Audit(ctx, store.AuditEntry{Actor: by, Action: "datenquelle_sync", Detail: s.Name + ": " + strings.Join(stt.Counts, " · ") + " " + stt.Message})
	return res, stt
}

// Tick startet fällige Quellen (minütlich aufgerufen).
func Tick(ctx context.Context, st *store.Store) {
	for _, s := range Sources(ctx, st) {
		if !s.Enabled || s.Interval <= 0 {
			continue
		}
		last := LastStatus(ctx, st, s.Key)
		if last != nil && time.Since(last.At) < time.Duration(s.Interval)*time.Minute {
			continue
		}
		Sync(ctx, st, s, "zeitplan")
	}
}
