// Package ui verwaltet die anpassbaren Oberflächen-Einstellungen: Texte, Farbschema, Startseite.
package ui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Settings sind die globalen Oberflächen-Einstellungen (gespeichert als Dokument "setting/ui").
type Settings struct {
	AppName string            `json:"app_name"`
	Org     string            `json:"org"`
	Theme   Theme             `json:"theme"`
	Home    Home              `json:"home"`
	Texts   map[string]string `json:"texts"`
}

// Theme ist das Farbschema.
type Theme struct {
	Preset    string `json:"preset"`     // Schlüssel aus Presets oder "eigen"
	Accent    string `json:"accent"`     // eigene Akzentfarbe (#rrggbb) bei Preset "eigen"
	Mode      string `json:"mode"`       // auto, light, dark
	AllowUser bool   `json:"allow_user"` // Benutzer dürfen Farbschema und Hell/Dunkel selbst wählen
	Radius    string `json:"radius"`     // eckig, normal, rund
	Density   string `json:"density"`    // kompakt, normal
}

// Home legt die Startseite fest.
type Home struct {
	Default map[string]string `json:"default"` // Rolle → Ansicht
	Allowed []string          `json:"allowed"` // für alle wählbare Ansichten
	Welcome string            `json:"welcome"` // Willkommenstext (alle Ansichten)
	Banner  string            `json:"banner"`  // Hinweisbanner auf allen Seiten
	Tone    string            `json:"tone"`    // info, warn, ok
}

// Views sind die verfügbaren Startseiten-Ansichten.
var Views = []struct {
	Key, Label, Icon, Hint string
}{
	{"standard", "Übersicht", "▦", "Aufgaben, eigene Anträge, Schnellstart und Aktivität"},
	{"kacheln", "Antragsportal", "▤", "Große Kacheln zum Antragstellen – ideal für Antragstellende"},
	{"aufgaben", "Arbeitskorb", "☑", "Offene Schritte nach Stelle gruppiert, Überfälliges zuerst"},
	{"kanban", "Kanban", "▥", "Vorgänge als Karten nach Phase: Freigabe, Umsetzung, Rückfrage, erledigt"},
	{"leitstand", "Leitstand", "📊", "Kennzahlen: Durchlaufzeiten, Engpässe, Fristen, Entwicklung"},
}

// ViewLabel liefert die Bezeichnung einer Ansicht.
func ViewLabel(k string) string {
	for _, v := range Views {
		if v.Key == k {
			return v.Label
		}
	}
	return k
}

// Defaults liefert die Grundeinstellungen.
func Defaults() Settings {
	return Settings{AppName: "Antragswerk", Org: "",
		Theme: Theme{Preset: "blau", Mode: "auto", AllowUser: true, Radius: "normal", Density: "normal"},
		Home: Home{Default: map[string]string{"nutzer": "standard", "bearbeiter": "aufgaben", "admin": "standard"},
			Allowed: []string{"standard", "kacheln", "aufgaben", "kanban", "leitstand"}, Tone: "info"},
		Texts: map[string]string{}}
}

var (
	mu  sync.RWMutex
	cur = Defaults()
)

// Set übernimmt neue Einstellungen (ergänzt fehlende Werte).
func Set(s Settings) {
	d := Defaults()
	if s.AppName == "" {
		s.AppName = d.AppName
	}
	if s.Theme.Preset == "" {
		s.Theme.Preset = d.Theme.Preset
	}
	if s.Theme.Mode == "" {
		s.Theme.Mode = d.Theme.Mode
	}
	if s.Home.Default == nil {
		s.Home.Default = d.Home.Default
	}
	if s.Home.Allowed == nil {
		s.Home.Allowed = d.Home.Allowed
	}
	if s.Texts == nil {
		s.Texts = map[string]string{}
	}
	for k, v := range s.Texts {
		if strings.TrimSpace(v) == "" {
			delete(s.Texts, k)
		}
	}
	mu.Lock()
	cur = s
	mu.Unlock()
}

// Get liefert eine Kopie der aktuellen Einstellungen.
func Get() Settings {
	mu.RLock()
	defer mu.RUnlock()
	s := cur
	s.Texts = make(map[string]string, len(cur.Texts))
	for k, v := range cur.Texts {
		s.Texts[k] = v
	}
	return s
}

// T liefert einen (ggf. angepassten) Text.
func T(key string) string {
	mu.RLock()
	v, ok := cur.Texts[key]
	mu.RUnlock()
	if ok {
		return v
	}
	if d, ok := textIndex[key]; ok {
		return d.Default
	}
	return key
}

// Tf ersetzt %s/%d-Platzhalter.
func Tf(key string, a ...any) string { return fmt.Sprintf(T(key), a...) }

// All liefert alle Texte (für die Oberfläche und JS).
func All() map[string]string {
	out := make(map[string]string, len(TextDefs))
	for _, d := range TextDefs {
		out[d.Key] = T(d.Key)
	}
	return out
}

// HomeView ermittelt die Ansicht für eine Rolle und einen Wunsch.
func HomeView(role, wish string) (string, []string) {
	s := Get()
	allowed := s.Home.Allowed
	if wish != "" {
		for _, a := range allowed {
			if a == wish {
				return wish, allowed
			}
		}
	}
	v := s.Home.Default[role]
	if v == "" {
		v = "standard"
	}
	return v, allowed
}

// ---------- Farben ----------

// Palette ist ein Farbschema.
type Palette struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Light string `json:"light"` // Akzent im hellen Modus
	Dark  string `json:"dark"`  // Akzent im dunklen Modus
}

// Presets sind die vordefinierten Farbschemata.
var Presets = []Palette{
	{"blau", "Verwaltungsblau", "#1f5fbf", "#5b9bf0"},
	{"petrol", "Petrol", "#0e7490", "#38bdf8"},
	{"gruen", "Grün", "#1d7a46", "#4cc283"},
	{"violett", "Violett", "#6d28d9", "#a78bfa"},
	{"rot", "Stadtrot", "#b42318", "#f2786d"},
	{"orange", "Orange", "#c2410c", "#fb923c"},
	{"anthrazit", "Anthrazit", "#334155", "#94a3b8"},
	{"magenta", "Magenta", "#be185d", "#f472b6"},
}

func preset(k string) (Palette, bool) {
	for _, p := range Presets {
		if p.Key == k {
			return p, true
		}
	}
	return Palette{}, false
}

type rgb struct{ r, g, b float64 }

func parseHex(s string) (rgb, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return rgb{}, false
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return rgb{}, false
	}
	return rgb{float64(n >> 16 & 255), float64(n >> 8 & 255), float64(n & 255)}, true
}

func (c rgb) hex() string {
	cl := func(x float64) int { return int(math.Max(0, math.Min(255, math.Round(x)))) }
	return fmt.Sprintf("#%02x%02x%02x", cl(c.r), cl(c.g), cl(c.b))
}

func mix(a, b rgb, t float64) rgb {
	return rgb{a.r*(1-t) + b.r*t, a.g*(1-t) + b.g*t, a.b*(1-t) + b.b*t}
}

func lum(c rgb) float64 {
	f := func(x float64) float64 {
		x /= 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.r) + 0.7152*f(c.g) + 0.0722*f(c.b)
}

// Vars berechnet die CSS-Variablen eines Akzents für hell und dunkel.
func Vars(light, dark string) (lightCSS, darkCSS string) {
	l, ok := parseHex(light)
	if !ok {
		l, _ = parseHex("#1f5fbf")
	}
	d, ok := parseHex(dark)
	if !ok {
		d = mix(l, rgb{255, 255, 255}, 0.35)
	}
	white, black := rgb{255, 255, 255}, rgb{0, 0, 0}
	lt := "#ffffff"
	if lum(l) > 0.45 {
		lt = "#0b1220"
	}
	lightCSS = fmt.Sprintf("--accent:%s;--accent-2:%s;--accent-soft:%s;--accent-text:%s;", l.hex(), mix(l, black, 0.18).hex(), mix(l, white, 0.88).hex(), lt)
	darkCSS = fmt.Sprintf("--accent:%s;--accent-2:%s;--accent-soft:%s;--accent-text:#0b1220;", d.hex(), mix(d, white, 0.15).hex(), mix(d, rgb{0x15, 0x1b, 0x23}, 0.78).hex())
	return
}

// ThemeCSS liefert das CSS für das eingestellte Farbschema.
func ThemeCSS() string {
	s := Get()
	light, dark := "#1f5fbf", "#5b9bf0"
	if p, ok := preset(s.Theme.Preset); ok {
		light, dark = p.Light, p.Dark
	} else if s.Theme.Preset == "eigen" && s.Theme.Accent != "" {
		light, dark = s.Theme.Accent, ""
	}
	l, d := Vars(light, dark)
	css := "html:root{" + l + radius(s.Theme.Radius) + "}" +
		"html:root[data-theme=dark]{" + d + "}" +
		"@media (prefers-color-scheme: dark){html:root:not([data-theme=light]){" + d + "}}"
	if s.Theme.Density == "kompakt" {
		css += "body{font-size:14px}.card{padding:16px}main{padding:16px 20px}"
	}
	return css
}

func radius(r string) string {
	switch r {
	case "eckig":
		return "--radius:4px;--radius-sm:3px;"
	case "rund":
		return "--radius:18px;--radius-sm:12px;"
	}
	return ""
}

// PresetVars liefert die Variablen aller Presets (für die Benutzerauswahl im Browser).
func PresetVars() map[string][2]string {
	out := map[string][2]string{}
	for _, p := range Presets {
		l, d := Vars(p.Light, p.Dark)
		out[p.Key] = [2]string{l, d}
	}
	return out
}

// ---------- Texte ----------

// TextDef beschreibt einen anpassbaren Text.
type TextDef struct {
	Key     string `json:"key"`
	Group   string `json:"group"`
	Label   string `json:"label"`
	Default string `json:"default"`
	Tpl     bool   `json:"tpl,omitempty"` // Go-Vorlage (Mails)
}

var textIndex = map[string]TextDef{}

func init() {
	for _, d := range TextDefs {
		textIndex[d.Key] = d
	}
}

// Groups liefert die Textgruppen in Reihenfolge.
func Groups() []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range TextDefs {
		if !seen[d.Group] {
			seen[d.Group] = true
			out = append(out, d.Group)
		}
	}
	return out
}

// Overridden liefert die Schlüssel geänderter Texte (sortiert).
func Overridden() []string {
	s := Get()
	var out []string
	for k := range s.Texts {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AccentHex liefert die Akzentfarbe (heller Modus) für Mails.
func AccentHex() string {
	s := Get()
	if p, ok := preset(s.Theme.Preset); ok {
		return p.Light
	}
	if c, ok := parseHex(s.Theme.Accent); ok {
		return c.hex()
	}
	return "#1f5fbf"
}
