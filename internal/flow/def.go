// Package flow beschreibt Antragsabläufe (Formular + Schritte) und wertet sie aus.
// Es ist frei von Datenbank und HTTP, damit Designer-Vorschau und Laufzeit dieselbe Logik nutzen.
package flow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Def ist eine vollständige Ablaufdefinition (eine Version).
type Def struct {
	Key         string     `json:"key" yaml:"key"`
	Name        string     `json:"name" yaml:"name"`
	Description string     `json:"description" yaml:"description"`
	Category    string     `json:"category" yaml:"category"`
	Icon        string     `json:"icon" yaml:"icon"`
	Color       string     `json:"color" yaml:"color"`
	Prefix      string     `json:"prefix" yaml:"prefix"`       // Nummernkreis, z. B. EIN → EIN-2026-0001
	TitleTpl    string     `json:"title_tpl" yaml:"title_tpl"` // Betreff des Vorgangs
	Audience    []string   `json:"audience" yaml:"audience"`   // Stellen, die den Antrag stellen dürfen (leer = alle)
	Intro       string     `json:"intro" yaml:"intro"`         // Hinweistext über dem Formular
	Form        Form       `json:"form" yaml:"form"`           // Eingabemaske
	Computed    []Computed `json:"computed" yaml:"computed"`   // berechnete Werte (.c.key)
	Steps       []Step     `json:"steps" yaml:"steps"`         // Ablauf
	Done        MailTpl    `json:"done" yaml:"done"`           // Abschlussmail an Antragsteller
}

// Form ist die Eingabemaske aus Eingabegruppen.
type Form struct {
	Sections []Section `json:"sections" yaml:"sections"`
}

// Section ist eine Eingabegruppe (Karte im Formular).
type Section struct {
	ID          string  `json:"id" yaml:"id"`
	Title       string  `json:"title" yaml:"title"`
	Description string  `json:"description" yaml:"description"`
	Icon        string  `json:"icon" yaml:"icon"`
	Show        *Cond   `json:"show,omitempty" yaml:"show,omitempty"`
	Fields      []Field `json:"fields" yaml:"fields"`
}

// Option ist ein Auswahlwert.
type Option struct {
	Value string `json:"value" yaml:"value"`
	Label string `json:"label" yaml:"label"`
}

// Prefill übernimmt Werte aus einer gewählten Person (Verzeichnis) in ein Feld.
type Prefill struct {
	From string `json:"from" yaml:"from"` // Schlüssel eines Personenfelds
	Attr string `json:"attr" yaml:"attr"` // name, mail, sam, department, title, phone, office, company
}

// Field ist ein Eingabeelement. Type "group" ist eine wiederholbare Gruppe mit Unterfeldern.
type Field struct {
	Key         string   `json:"key" yaml:"key"`
	Label       string   `json:"label" yaml:"label"`
	Type        string   `json:"type" yaml:"type"`
	Help        string   `json:"help,omitempty" yaml:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty" yaml:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty" yaml:"required,omitempty"`
	Width       string   `json:"width,omitempty" yaml:"width,omitempty"` // full, half, third
	Options     []Option `json:"options,omitempty" yaml:"options,omitempty"`
	Catalog     string   `json:"catalog,omitempty" yaml:"catalog,omitempty"`
	Filter      *Cond    `json:"filter,omitempty" yaml:"filter,omitempty"` // Katalogeinträge filtern (Attribute)
	Multi       bool     `json:"multi,omitempty" yaml:"multi,omitempty"`
	Default     string   `json:"default,omitempty" yaml:"default,omitempty"`
	Pattern     string   `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	PatternMsg  string   `json:"pattern_msg,omitempty" yaml:"pattern_msg,omitempty"`
	Min         int      `json:"min,omitempty" yaml:"min,omitempty"`
	Max         int      `json:"max,omitempty" yaml:"max,omitempty"`
	AddLabel    string   `json:"add_label,omitempty" yaml:"add_label,omitempty"`
	ItemLabel   string   `json:"item_label,omitempty" yaml:"item_label,omitempty"` // Titel je Eintrag, z. B. "Programm"
	Fields      []Field  `json:"fields,omitempty" yaml:"fields,omitempty"`
	Show        *Cond    `json:"show,omitempty" yaml:"show,omitempty"`
	Prefill     *Prefill `json:"prefill,omitempty" yaml:"prefill,omitempty"`
	Tone        string   `json:"tone,omitempty" yaml:"tone,omitempty"` // info-Feld: info, warn, ok
}

// FieldTypes sind die verfügbaren Feldarten (Reihenfolge = Palette im Designer).
// Kind beschreibt eine Feld- oder Schrittart für die Oberfläche.
type Kind struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
	Hint  string `json:"hint"`
}

var FieldTypes = []Kind{
	{"text", "Text", "T", "Einzeilige Eingabe"},
	{"textarea", "Mehrzeilig", "¶", "Begründung, Hinweise …"},
	{"number", "Zahl", "#", "Ganze Zahl oder Dezimalzahl"},
	{"date", "Datum", "📅", "Kalenderauswahl"},
	{"email", "E-Mail", "@", "Adresse mit Prüfung"},
	{"phone", "Telefon", "☎", "Rufnummer"},
	{"select", "Auswahl", "▾", "Eine Option aus einer Liste"},
	{"radio", "Optionsfelder", "◉", "Eine Option, alle sichtbar"},
	{"checkboxes", "Mehrfachauswahl", "☑", "Mehrere Optionen"},
	{"checkbox", "Ja/Nein", "✓", "Einzelnes Kontrollkästchen"},
	{"catalog", "Katalog", "▤", "Software, Abteilung, Raum, Verzeichnis …"},
	{"user", "Person (AD)", "👤", "Suche im Verzeichnis"},
	{"group", "Wiederholbare Gruppe", "⧉", "n-fach hinzufügbar"},
	{"info", "Hinweistext", "ℹ", "Erklärung ohne Eingabe"},
}

var fieldTypeSet = func() map[string]bool {
	m := map[string]bool{}
	for _, t := range FieldTypes {
		m[t.Key] = true
	}
	return m
}()

// Computed ist ein berechneter Wert, verfügbar als {{.c.key}} in Skripten und Mails.
type Computed struct {
	Key   string `json:"key" yaml:"key"`
	Label string `json:"label" yaml:"label"`
	Tpl   string `json:"tpl" yaml:"tpl"`
}

// Assign beschreibt, wer einen Schritt erledigt.
type Assign struct {
	Type   string `json:"type" yaml:"type"`                         // stelle, feld, katalog, antragsteller, email
	Stelle string `json:"stelle,omitempty" yaml:"stelle,omitempty"` // Stelle (Organisationseinheit/Rolle)
	Field  string `json:"field,omitempty" yaml:"field,omitempty"`   // Personen- oder Katalogfeld (bei "je Eintrag": Feld im Eintrag)
	Attr   string `json:"attr,omitempty" yaml:"attr,omitempty"`     // Katalog-Attribut mit Stelle/Mail/Kennung
	Email  string `json:"email,omitempty" yaml:"email,omitempty"`
}

// Script ist ein Skriptbaustein eines Schritts.
type Script struct {
	Title string `json:"title" yaml:"title"`
	Lang  string `json:"lang" yaml:"lang"` // powershell, cmd, bash, text
	Code  string `json:"code" yaml:"code"`
}

// MailTpl ist eine Mailvorlage (leer = Standardtext).
type MailTpl struct {
	Subject string `json:"subject,omitempty" yaml:"subject,omitempty"`
	Body    string `json:"body,omitempty" yaml:"body,omitempty"`
}

// Step ist ein Schritt im Ablauf.
type Step struct {
	ID           string    `json:"id" yaml:"id"`
	Title        string    `json:"title" yaml:"title"`
	Kind         string    `json:"kind" yaml:"kind"` // freigabe, aufgabe, info, warten
	After        []string  `json:"after" yaml:"after"`
	When         *Cond     `json:"when,omitempty" yaml:"when,omitempty"`
	ForEach      string    `json:"foreach,omitempty" yaml:"foreach,omitempty"` // wiederholbare Gruppe oder Mehrfach-Katalog
	Assign       Assign    `json:"assign" yaml:"assign"`
	DueHours     int       `json:"due_hours,omitempty" yaml:"due_hours,omitempty"`
	Instructions string    `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Why          string    `json:"why,omitempty" yaml:"why,omitempty"`
	Checklist    []string  `json:"checklist,omitempty" yaml:"checklist,omitempty"`
	Scripts      []Script  `json:"scripts,omitempty" yaml:"scripts,omitempty"`
	Outputs      []Field   `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	Mail         MailTpl   `json:"mail,omitempty" yaml:"mail,omitempty"`
	WaitField    string    `json:"wait_field,omitempty" yaml:"wait_field,omitempty"`
	WaitOffset   int       `json:"wait_offset,omitempty" yaml:"wait_offset,omitempty"` // Tage relativ zum Datum (negativ = vorher)
	Lane         string    `json:"lane,omitempty" yaml:"lane,omitempty"`               // Spur/Bereich im Graphen (nur Anzeige)
	GLPI         *GLPIStep `json:"glpi,omitempty" yaml:"glpi,omitempty"`               // nur Schrittart glpi
	X            float64   `json:"x" yaml:"x,omitempty"`
	Y            float64   `json:"y" yaml:"y,omitempty"`
}

// StepKinds mit Beschriftung.
var StepKinds = []Kind{
	{"freigabe", "Freigabe", "✔", "Genehmigen, ablehnen oder Rückfrage"},
	{"aufgabe", "Aufgabe", "▢", "Arbeitsschritt mit Checkliste und Skripten"},
	{"info", "Benachrichtigung", "✉", "Mail versenden, läuft automatisch weiter"},
	{"warten", "Termin abwarten", "⏱", "Wartet bis zu einem Datum aus dem Antrag"},
	{"glpi", "GLPI-Ticket/Change", "🎫", "Übergibt an GLPI und wartet auf die Lösung"},
}

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)

// Issue ist ein Prüfergebnis.
type Issue struct {
	Level string `json:"level"` // error, warn
	Ref   string `json:"ref"`   // step:<id> | field:<key> | section:<id>
	Msg   string `json:"msg"`
}

// AllFields liefert alle Felder (auch in Gruppen) mit Pfad.
func (d *Def) AllFields() []FieldRef {
	var out []FieldRef
	for _, s := range d.Form.Sections {
		for i := range s.Fields {
			f := &s.Fields[i]
			out = append(out, FieldRef{Path: f.Key, Field: f, Section: s.ID})
			for j := range f.Fields {
				c := &f.Fields[j]
				out = append(out, FieldRef{Path: f.Key + "." + c.Key, Field: c, Section: s.ID, Parent: f})
			}
		}
	}
	return out
}

// FieldRef verweist auf ein Feld mit Pfad.
type FieldRef struct {
	Path    string
	Field   *Field
	Parent  *Field
	Section string
}

// FieldByKey sucht ein Feld der obersten Ebene.
func (d *Def) FieldByKey(k string) *Field {
	for si := range d.Form.Sections {
		for i := range d.Form.Sections[si].Fields {
			if d.Form.Sections[si].Fields[i].Key == k {
				return &d.Form.Sections[si].Fields[i]
			}
		}
	}
	return nil
}

// StepByID sucht einen Schritt.
func (d *Def) StepByID(id string) *Step {
	for i := range d.Steps {
		if d.Steps[i].ID == id {
			return &d.Steps[i]
		}
	}
	return nil
}

// Order liefert die Schritte topologisch sortiert (stabil nach Definition).
func (d *Def) Order() ([]*Step, error) {
	idx := map[string]int{}
	for i, s := range d.Steps {
		idx[s.ID] = i
	}
	state := map[string]int{}
	var out []*Step
	var visit func(id string, path []string) error
	visit = func(id string, path []string) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("Zyklus: %s", strings.Join(append(path, id), " → "))
		case 2:
			return nil
		}
		state[id] = 1
		s := &d.Steps[idx[id]]
		for _, p := range s.After {
			if _, ok := idx[p]; !ok {
				continue
			}
			if err := visit(p, append(path, id)); err != nil {
				return err
			}
		}
		state[id] = 2
		out = append(out, s)
		return nil
	}
	for _, s := range d.Steps {
		if err := visit(s.ID, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Validate prüft die Definition. Fehler verhindern das Veröffentlichen.
func (d *Def) Validate(stellen map[string]bool, catalogs map[string]bool) []Issue {
	var out []Issue
	add := func(level, ref, msg string, a ...any) { out = append(out, Issue{level, ref, fmt.Sprintf(msg, a...)}) }
	if strings.TrimSpace(d.Name) == "" {
		add("error", "settings", "Der Ablauf braucht einen Namen.")
	}
	keys := map[string]bool{}
	if len(d.Form.Sections) == 0 {
		add("error", "form", "Das Formular hat keine Eingabegruppe.")
	}
	secIDs := map[string]bool{}
	for _, s := range d.Form.Sections {
		if secIDs[s.ID] {
			add("error", "section:"+s.ID, "Eingabegruppe-ID %q doppelt.", s.ID)
		}
		secIDs[s.ID] = true
		if strings.TrimSpace(s.Title) == "" {
			add("warn", "section:"+s.ID, "Eingabegruppe ohne Überschrift.")
		}
		for _, f := range s.Fields {
			checkField(&f, "", add)
			if f.Type != "info" {
				if keys[f.Key] {
					add("error", "field:"+f.Key, "Feldschlüssel %q ist doppelt vergeben.", f.Key)
				}
				keys[f.Key] = true
			}
			if f.Type == "catalog" && catalogs != nil && !catalogs[f.Catalog] {
				add("error", "field:"+f.Key, "Feld %q: Katalog %q existiert nicht.", f.Label, f.Catalog)
			}
			sub := map[string]bool{}
			for _, c := range f.Fields {
				checkField(&c, f.Key+".", add)
				if c.Type == "group" {
					add("error", "field:"+f.Key+"."+c.Key, "Gruppen lassen sich nicht verschachteln.")
				}
				if sub[c.Key] && c.Type != "info" {
					add("error", "field:"+f.Key+"."+c.Key, "Unterfeld %q doppelt.", c.Key)
				}
				sub[c.Key] = true
				if c.Type == "catalog" && catalogs != nil && !catalogs[c.Catalog] {
					add("error", "field:"+f.Key+"."+c.Key, "Feld %q: Katalog %q existiert nicht.", c.Label, c.Catalog)
				}
			}
			if f.Type == "group" && len(f.Fields) == 0 {
				add("error", "field:"+f.Key, "Gruppe %q hat keine Unterfelder.", f.Label)
			}
		}
	}
	ids := map[string]bool{}
	for _, s := range d.Steps {
		ref := "step:" + s.ID
		if !keyRe.MatchString(s.ID) {
			add("error", ref, "Schritt-ID %q ungültig (a–z, 0–9, _).", s.ID)
		}
		if ids[s.ID] {
			add("error", ref, "Schritt-ID %q doppelt.", s.ID)
		}
		ids[s.ID] = true
		if strings.TrimSpace(s.Title) == "" {
			add("error", ref, "Schritt ohne Titel.")
		}
		switch s.Kind {
		case "freigabe", "aufgabe", "info":
			switch s.Assign.Type {
			case "":
				add("error", ref, "„%s“: Zuständigkeit fehlt.", s.Title)
			case "stelle":
				if s.Assign.Stelle == "" || (stellen != nil && !stellen[s.Assign.Stelle]) {
					add("error", ref, "„%s“: Stelle %q unbekannt.", s.Title, s.Assign.Stelle)
				}
			case "feld":
				if s.Assign.Field == "" {
					add("error", ref, "„%s“: Personenfeld für die Zuständigkeit fehlt.", s.Title)
				}
			case "katalog":
				if s.Assign.Field == "" || s.Assign.Attr == "" {
					add("error", ref, "„%s“: Katalogfeld und Attribut für die Zuständigkeit angeben.", s.Title)
				}
			case "email":
				for _, p := range splitList(s.Assign.Email) {
					if !strings.Contains(p, "@") && (stellen == nil || !stellen[p]) {
						add("warn", ref, "„%s“: %q ist weder Mail-Adresse noch Stelle (wird als Anmeldename behandelt).", s.Title, p)
					}
				}
				if len(splitList(s.Assign.Email)) == 0 {
					add("error", ref, "„%s“: Empfänger angeben.", s.Title)
				}
			}
		case "warten":
			if s.WaitField == "" {
				add("error", ref, "„%s“: Datumsfeld zum Abwarten fehlt.", s.Title)
			} else if f := d.FieldByKey(s.WaitField); f == nil || f.Type != "date" {
				add("error", ref, "„%s“: %q ist kein Datumsfeld.", s.Title, s.WaitField)
			}
		case "glpi":
			validateGLPI(d, &s, add)
			if s.Assign.Type == "stelle" && stellen != nil && !stellen[s.Assign.Stelle] {
				add("error", ref, "„%s“: Stelle %q unbekannt.", s.Title, s.Assign.Stelle)
			}
		default:
			add("error", ref, "„%s“: unbekannte Schrittart %q.", s.Title, s.Kind)
		}
		if s.ForEach != "" {
			f := d.FieldByKey(s.ForEach)
			if f == nil || !(f.Type == "group" || (f.Multi && (f.Type == "catalog" || f.Type == "checkboxes"))) {
				add("error", ref, "„%s“: „je Eintrag“ braucht eine wiederholbare Gruppe oder Mehrfachauswahl (%q).", s.Title, s.ForEach)
			}
		}
		if s.Kind == "aufgabe" && strings.TrimSpace(s.Instructions) == "" && len(s.Checklist) == 0 && len(s.Scripts) == 0 {
			add("warn", ref, "„%s“: keine Anleitung, Checkliste oder Skripte – die Bearbeitung weiß nicht, was zu tun ist.", s.Title)
		}
		if s.Kind == "freigabe" && strings.TrimSpace(s.Why) == "" {
			add("warn", ref, "„%s“: Begründung der Freigabe (Warum?) fehlt.", s.Title)
		}
	}
	for _, s := range d.Steps {
		for _, p := range s.After {
			if !ids[p] {
				add("error", "step:"+s.ID, "„%s“ wartet auf unbekannten Schritt %q.", s.Title, p)
			}
		}
		for _, r := range condRules(s.When) {
			root := strings.SplitN(r.Field, ".", 2)[0]
			if root != "item" && root != "c" && !keys[root] {
				add("warn", "step:"+s.ID, "„%s“: Bedingung bezieht sich auf unbekanntes Feld %q.", s.Title, r.Field)
			}
			if root == "item" && s.ForEach == "" {
				add("error", "step:"+s.ID, "„%s“: Bedingung auf „item.…“ nur bei „je Eintrag“ möglich.", s.Title)
			}
		}
	}
	if _, err := d.Order(); err != nil {
		add("error", "flow", "%s", err.Error())
	}
	if len(d.Steps) == 0 {
		add("warn", "flow", "Der Ablauf hat noch keine Schritte – Anträge würden sofort abgeschlossen.")
	}
	ck := map[string]bool{}
	for _, c := range d.Computed {
		if !keyRe.MatchString(c.Key) || ck[c.Key] {
			add("error", "computed", "Berechneter Wert %q: Schlüssel ungültig oder doppelt.", c.Key)
		}
		ck[c.Key] = true
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level == "error" && out[j].Level != "error" })
	return out
}

func checkField(f *Field, prefix string, add func(level, ref, msg string, a ...any)) {
	ref := "field:" + prefix + f.Key
	if f.Type == "info" {
		return
	}
	if !keyRe.MatchString(f.Key) {
		add("error", ref, "Feldschlüssel %q ungültig (Kleinbuchstaben, Ziffern, _; beginnt mit Buchstabe).", f.Key)
	}
	if !fieldTypeSet[f.Type] {
		add("error", ref, "Feld %q: unbekannte Feldart %q.", f.Key, f.Type)
	}
	if strings.TrimSpace(f.Label) == "" {
		add("warn", ref, "Feld %q ohne Beschriftung.", f.Key)
	}
	if (f.Type == "select" || f.Type == "radio" || f.Type == "checkboxes") && len(f.Options) == 0 {
		add("error", ref, "„%s“: keine Auswahlwerte.", f.Label)
	}
	if f.Type == "catalog" && f.Catalog == "" {
		add("error", ref, "„%s“: Katalog wählen.", f.Label)
	}
	if f.Pattern != "" {
		if _, err := regexp.Compile(f.Pattern); err != nil {
			add("error", ref, "„%s“: Muster ungültig: %v", f.Label, err)
		}
	}
	if f.Type == "group" && f.Max > 0 && f.Min > f.Max {
		add("error", ref, "„%s“: Minimum größer als Maximum.", f.Label)
	}
}

func condRules(c *Cond) []Rule {
	if c == nil {
		return nil
	}
	return c.Rules
}
