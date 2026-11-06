package flow

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Catalog ist eine Stammdatenliste (Software, Abteilungen, Verzeichnisse, Räume …) mit frei definierbaren Attributen.
type Catalog struct {
	Key         string    `json:"key" yaml:"key"`
	Name        string    `json:"name" yaml:"name"`
	Description string    `json:"description" yaml:"description"`
	Icon        string    `json:"icon" yaml:"icon"`
	Attrs       []AttrDef `json:"attrs" yaml:"attrs"`
	Entries     []Entry   `json:"entries" yaml:"entries"`
}

// AttrDef beschreibt ein Attribut eines Katalogs.
type AttrDef struct {
	Key   string `json:"key" yaml:"key"`
	Label string `json:"label" yaml:"label"`
	Hint  string `json:"hint,omitempty" yaml:"hint,omitempty"`
}

// Entry ist ein Katalogeintrag.
type Entry struct {
	Key      string            `json:"key" yaml:"key"`
	Label    string            `json:"label" yaml:"label"`
	Group    string            `json:"group,omitempty" yaml:"group,omitempty"`
	Inactive bool              `json:"inactive,omitempty" yaml:"inactive,omitempty"`
	Attrs    map[string]string `json:"attrs" yaml:"attrs"`
	Src      string            `json:"src,omitempty" yaml:"src,omitempty"` // Datenquelle, aus der der Eintrag stammt
}

// Stelle ist eine zuständige Organisationseinheit oder Rolle (IT-Benutzerverwaltung, Exchange-Team, Hausverwaltung …).
type Stelle struct {
	Key         string   `json:"key" yaml:"key"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Email       string   `json:"email" yaml:"email"`     // Funktionspostfach
	Members     []string `json:"members" yaml:"members"` // Mail-Adressen oder Anmeldenamen
	Group       string   `json:"group" yaml:"group"`     // AD-Gruppe (CN oder DN) – Mitglieder dürfen bearbeiten
	Color       string   `json:"color" yaml:"color"`
}

// Snippet ist ein wiederverwendbarer Skriptbaustein.
type Snippet struct {
	Key         string `json:"key" yaml:"key"`
	Title       string `json:"title" yaml:"title"`
	Group       string `json:"group" yaml:"group"`
	Description string `json:"description" yaml:"description"`
	Lang        string `json:"lang" yaml:"lang"`
	Code        string `json:"code" yaml:"code"`
}

// Env bündelt alles, was zur Auswertung eines Antrags gebraucht wird.
type Env struct {
	Def      *Def
	Catalogs map[string]*Catalog
	Stellen  map[string]*Stelle
	Vars     map[string]string
}

// CatVal ist ein ausgewählter Katalogeintrag im Vorlagenkontext. Ausgabe = Bezeichnung, Felder = Attribute.
type CatVal map[string]any

func (c CatVal) String() string { return fmt.Sprint(c["label"]) }

// UserVal ist eine Person aus dem Verzeichnis. Ausgabe = Anzeigename.
type UserVal map[string]any

func (u UserVal) String() string { return fmt.Sprint(u["name"]) }

// Lookup sucht einen Katalogeintrag.
func (e *Env) Lookup(catalog, key string) *Entry {
	c := e.Catalogs[catalog]
	if c == nil {
		return nil
	}
	for i := range c.Entries {
		if c.Entries[i].Key == key {
			return &c.Entries[i]
		}
	}
	return nil
}

func (e *Env) catVal(catalog string, raw any) CatVal {
	key := str(raw)
	if key == "" {
		return CatVal{"key": "", "label": ""}
	}
	v := CatVal{"key": key, "label": key}
	c := e.Catalogs[catalog]
	if c != nil {
		for _, a := range c.Attrs {
			v[a.Key] = ""
		}
	}
	if en := e.Lookup(catalog, key); en != nil {
		v["label"] = en.Label
		v["group"] = en.Group
		for k, x := range en.Attrs {
			v[k] = x
		}
	}
	return v
}

func userVal(raw any) UserVal {
	u := UserVal{"sam": "", "name": "", "given": "", "surname": "", "mail": "", "department": "", "title": "", "phone": "", "office": "", "company": "", "dn": "", "manager": ""}
	if m, ok := raw.(map[string]any); ok {
		for k, v := range m {
			u[k] = str(v)
		}
	}
	return u
}

func optLabel(f *Field, v string) string {
	for _, o := range f.Options {
		if o.Value == v {
			if o.Label != "" {
				return o.Label
			}
			return o.Value
		}
	}
	return v
}

// expandField wandelt einen Rohwert in den Vorlagenwert um.
func (e *Env) expandField(f *Field, raw any, into map[string]any) {
	switch f.Type {
	case "info":
		return
	case "checkbox":
		into[f.Key] = truthy(raw)
	case "checkboxes":
		vals := strList(raw)
		lbl := make([]string, 0, len(vals))
		lst := make([]any, 0, len(vals))
		for _, v := range vals {
			lst = append(lst, v)
			lbl = append(lbl, optLabel(f, v))
		}
		into[f.Key] = lst
		into[f.Key+"_text"] = strings.Join(lbl, ", ")
	case "select", "radio":
		into[f.Key] = str(raw)
		into[f.Key+"_text"] = optLabel(f, str(raw))
	case "catalog":
		if f.Multi {
			vals := strList(raw)
			lst := make([]any, 0, len(vals))
			for _, v := range vals {
				lst = append(lst, e.catVal(f.Catalog, v))
			}
			into[f.Key] = lst
		} else {
			into[f.Key] = e.catVal(f.Catalog, raw)
		}
	case "user":
		into[f.Key] = userVal(raw)
	case "group":
		items, _ := raw.([]any)
		lst := make([]any, 0, len(items))
		for i, it := range items {
			m, _ := it.(map[string]any)
			x := map[string]any{"nr": i + 1}
			for ci := range f.Fields {
				e.expandField(&f.Fields[ci], m[f.Fields[ci].Key], x)
			}
			lst = append(lst, x)
		}
		into[f.Key] = lst
	default:
		into[f.Key] = str(raw)
	}
}

// Context baut den Vorlagen- und Bedingungskontext eines Antrags.
// meta: Antragsdaten (nummer, antragsteller …); out: Ergebnisse erledigter Schritte.
func (e *Env) Context(data map[string]any, meta map[string]any, out map[string]any) map[string]any {
	ctx := map[string]any{}
	for _, s := range e.Def.Form.Sections {
		for i := range s.Fields {
			e.expandField(&s.Fields[i], data[s.Fields[i].Key], ctx)
		}
	}
	if meta == nil {
		meta = map[string]any{}
	}
	ctx["antrag"] = meta
	o := map[string]any{}
	for _, st := range e.Def.Steps {
		m := map[string]any{"erledigt": false, "von": "", "kommentar": "", "liste": []any{}}
		for _, of := range st.Outputs {
			m[of.Key] = ""
		}
		o[st.ID] = m
	}
	for k, v := range out {
		if m, ok := v.(map[string]any); ok {
			if base, ok := o[k].(map[string]any); ok {
				for kk, vv := range m {
					base[kk] = vv
				}
				continue
			}
		}
		o[k] = v
	}
	ctx["out"] = o
	vars := map[string]any{}
	for k, v := range e.Vars {
		vars[k] = v
	}
	ctx["var"] = vars
	c := map[string]any{}
	ctx["c"] = c
	for _, cp := range e.Def.Computed {
		v, err := Render(cp.Tpl, ctx, e.Vars)
		if err != nil {
			v = ""
		}
		c[cp.Key] = strings.TrimSpace(v)
	}
	return ctx
}

// Items liefert die Einträge für einen "je Eintrag"-Schritt.
func (e *Env) Items(ctx map[string]any, field string) []any {
	v := ctx[field]
	lst, _ := v.([]any)
	return lst
}

// ItemLabel beschreibt einen Eintrag kurz (für Titel der Aufgabe).
func ItemLabel(item any) string {
	switch v := item.(type) {
	case CatVal:
		return v.String()
	case UserVal:
		return v.String()
	case string:
		return v
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// zuerst Katalogwerte, dann Texte
		for _, k := range keys {
			if c, ok := v[k].(CatVal); ok && c["key"] != "" {
				return c.String()
			}
		}
		for _, k := range keys {
			if s, ok := v[k].(string); ok && s != "" && !strings.HasSuffix(k, "_text") {
				if t, ok := v[k+"_text"].(string); ok && t != "" { // Auswahlfeld: Beschriftung statt Rohwert
					return t
				}
				return s
			}
		}
	}
	return ""
}

// Assignee ist die aufgelöste Zuständigkeit.
type Assignee struct {
	Label   string   `json:"label"`
	Stelle  string   `json:"stelle,omitempty"`
	Stellen []string `json:"stellen,omitempty"`
	Emails  []string `json:"emails,omitempty"`
	Users   []string `json:"users,omitempty"`
}

func (e *Env) stelleAssignee(key string) (Assignee, bool) {
	st := e.Stellen[key]
	if st == nil {
		return Assignee{}, false
	}
	a := Assignee{Label: st.Name, Stelle: st.Key, Stellen: []string{st.Key}}
	if st.Email != "" {
		a.Emails = append(a.Emails, st.Email)
	}
	for _, m := range st.Members {
		if strings.Contains(m, "@") {
			if st.Email == "" {
				a.Emails = append(a.Emails, m)
			}
		} else {
			a.Users = append(a.Users, m)
		}
	}
	return a, true
}

// valueAssignee löst einen Wert (Stellen-Schlüssel, Mail oder Kennung, auch mehrere mit ;) auf.
func (e *Env) valueAssignee(v string, label string) Assignee {
	var a Assignee
	for _, p := range splitList(v) {
		if sa, ok := e.stelleAssignee(p); ok {
			if a.Stelle == "" {
				a.Stelle = sa.Stelle
			}
			a.Emails = append(a.Emails, sa.Emails...)
			a.Users = append(a.Users, sa.Users...)
			a.Stellen = append(a.Stellen, sa.Stellen...)
			if a.Label == "" {
				a.Label = sa.Label
			}
			continue
		}
		if strings.Contains(p, "@") {
			a.Emails = append(a.Emails, p)
		} else {
			a.Users = append(a.Users, p)
		}
		if a.Label == "" {
			a.Label = p
		}
	}
	if label != "" && a.Stelle == "" {
		a.Label = label
	}
	return a
}

// Resolve ermittelt die Zuständigkeit eines Schritts. item ist der aktuelle Eintrag bei "je Eintrag".
func (e *Env) Resolve(s *Step, ctx map[string]any, item any) Assignee {
	switch s.Assign.Type {
	case "stelle":
		if a, ok := e.stelleAssignee(s.Assign.Stelle); ok {
			return a
		}
		return Assignee{Label: "Stelle " + s.Assign.Stelle + " (unbekannt)"}
	case "antragsteller":
		meta, _ := ctx["antrag"].(map[string]any)
		a := Assignee{Label: str(meta["antragsteller_name"]) + " (Antragsteller/in)"}
		if m := str(meta["antragsteller_mail"]); m != "" {
			a.Emails = []string{m}
		}
		if u := str(meta["antragsteller"]); u != "" {
			a.Users = []string{u}
		}
		return a
	case "email":
		return e.valueAssignee(s.Assign.Email, "")
	case "feld":
		v := pick(ctx, item, s.Assign.Field)
		switch u := v.(type) {
		case UserVal:
			a := Assignee{Label: str(u["name"])}
			if m := str(u["mail"]); m != "" {
				a.Emails = []string{m}
			}
			if sam := str(u["sam"]); sam != "" {
				a.Users = []string{sam}
			}
			if a.Label == "" {
				a.Label = "(Person nicht angegeben)"
			}
			return a
		default:
			return e.valueAssignee(str(v), "")
		}
	case "katalog":
		v := pick(ctx, item, s.Assign.Field)
		var vals []CatVal
		switch c := v.(type) {
		case CatVal:
			vals = []CatVal{c}
		case []any:
			for _, x := range c {
				if cv, ok := x.(CatVal); ok {
					vals = append(vals, cv)
				}
			}
		}
		var out Assignee
		for _, cv := range vals {
			a := e.valueAssignee(str(cv[s.Assign.Attr]), "")
			if out.Label == "" {
				out.Label = a.Label
			} else if a.Label != "" && !strings.Contains(out.Label, a.Label) {
				out.Label += ", " + a.Label
			}
			if out.Stelle == "" {
				out.Stelle = a.Stelle
			}
			out.Emails = append(out.Emails, a.Emails...)
			out.Users = append(out.Users, a.Users...)
			out.Stellen = append(out.Stellen, a.Stellen...)
		}
		if out.Label == "" {
			out.Label = "(im Katalog nicht hinterlegt)"
		}
		out.Emails = dedup(out.Emails)
		out.Users = dedup(out.Users)
		out.Stellen = dedup(out.Stellen)
		return out
	}
	return Assignee{Label: "–"}
}

// pick liest ein Feld aus dem Eintrag (falls vorhanden) oder dem Antrag. "item.x" erzwingt den Eintrag.
func pick(ctx map[string]any, item any, path string) any {
	if path == "" || path == "item" {
		return item
	}
	if strings.HasPrefix(path, "item.") {
		return Path(map[string]any{"item": item}, path)
	}
	root := strings.SplitN(path, ".", 2)[0]
	if m, ok := item.(map[string]any); ok {
		if _, ok := m[root]; ok {
			return Path(m, path)
		}
	}
	// Mehrfachauswahl "je Eintrag": Feld = Listenfeld → aktueller Eintrag
	if _, ok := ctx[root].([]any); ok && item != nil && root == path {
		return item
	}
	return Path(ctx, path)
}

// Path liest einen gepunkteten Pfad.
func Path(ctx map[string]any, path string) any {
	var cur any = ctx
	for _, p := range strings.Split(path, ".") {
		switch m := cur.(type) {
		case map[string]any:
			cur = m[p]
		case CatVal:
			cur = m[p]
		case UserVal:
			cur = m[p]
		default:
			return nil
		}
	}
	return cur
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case CatVal:
		return str(x["key"])
	case UserVal:
		return str(x["sam"])
	}
	return fmt.Sprint(v)
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		x = strings.ToLower(x)
		return x == "true" || x == "ja" || x == "1" || x == "on"
	}
	return false
}

func strList(v any) []string {
	switch x := v.(type) {
	case []any:
		out := make([]string, 0, len(x))
		for _, y := range x {
			if s := str(y); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	}
	return nil
}

func splitList(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == '\n' || r == '\r' })
	out := f[:0]
	for _, x := range f {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		k := strings.ToLower(s)
		if s == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// EmptyItem liefert einen leeren, vollständig erweiterten Eintrag für "je Eintrag"-Schritte (Vorschau ohne Daten).
func (e *Env) EmptyItem(field string) any {
	f := e.Def.FieldByKey(field)
	if f == nil {
		return map[string]any{}
	}
	switch f.Type {
	case "group":
		x := map[string]any{"nr": 1}
		for i := range f.Fields {
			e.expandField(&f.Fields[i], nil, x)
		}
		return x
	case "catalog":
		return e.catVal(f.Catalog, "")
	}
	return ""
}
