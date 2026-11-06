package flow

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"
)

var translit = strings.NewReplacer(
	"ä", "ae", "ö", "oe", "ü", "ue", "Ä", "Ae", "Ö", "Oe", "Ü", "Ue", "ß", "ss",
	"á", "a", "à", "a", "â", "a", "ã", "a", "å", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "õ", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u", "ç", "c", "ñ", "n", "ś", "s", "š", "s", "ž", "z", "ł", "l", "ć", "c", "č", "c", "ğ", "g", "ı", "i", "ş", "s",
	"Á", "A", "À", "A", "Â", "A", "É", "E", "È", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ç", "C", "Ñ", "N", "Š", "S", "Ž", "Z", "Ł", "L", "Č", "C", "Ş", "S",
)

// ASCII transliteriert Umlaute/Akzente und entfernt alles außer Buchstaben, Ziffern, Punkt, Bindestrich.
func ASCII(s string) string {
	s = translit.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func psQuote(v any) string { return "'" + strings.ReplaceAll(toS(v), "'", "''") + "'" }

func toS(v any) string {
	switch x := v.(type) {
	case CatVal:
		return x.String()
	case UserVal:
		return x.String()
	case nil:
		return ""
	}
	return str(v)
}

func toList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case nil:
		return nil
	case string:
		if x == "" {
			return nil
		}
		out := []any{}
		for _, s := range splitList(x) {
			out = append(out, s)
		}
		return out
	}
	return []any{v}
}

// pluck sammelt einen (gepunkteten) Pfad aus allen Einträgen, zerlegt Mehrfachwerte (; , Zeilen) und entfernt Dubletten.
func pluck(list any, path string) []any {
	seen := map[string]bool{}
	var out []any
	for _, it := range toList(list) {
		var v any
		switch m := it.(type) {
		case map[string]any:
			v = Path(m, path)
		case CatVal:
			v = Path(map[string]any(m), path)
		case UserVal:
			v = Path(map[string]any(m), path)
		}
		for _, x := range toList(v) {
			s := toS(x)
			for _, p := range splitList(s) {
				if !seen[strings.ToLower(p)] {
					seen[strings.ToLower(p)] = true
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func parseDate(v any) (time.Time, bool) {
	s := toS(v)
	for _, l := range []string{"2006-01-02", "02.01.2006", time.RFC3339} {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Funcs liefert die Vorlagenfunktionen. vars stellt z. B. das Benutzernamen-Schema bereit.
func Funcs(vars map[string]string) template.FuncMap {
	return template.FuncMap{
		"ps": psQuote,
		"psarr": func(v any) string {
			l := toList(v)
			p := make([]string, 0, len(l))
			for _, x := range l {
				p = append(p, psQuote(x))
			}
			return "@(" + strings.Join(p, ", ") + ")"
		},
		"ascii": func(v any) string { return ASCII(toS(v)) },
		"lower": func(v any) string { return strings.ToLower(toS(v)) },
		"upper": func(v any) string { return strings.ToUpper(toS(v)) },
		"trim":  func(v any) string { return strings.TrimSpace(toS(v)) },
		"left": func(n int, v any) string {
			r := []rune(toS(v))
			if len(r) > n {
				r = r[:n]
			}
			return string(r)
		},
		"replace": func(old, nw string, v any) string { return strings.ReplaceAll(toS(v), old, nw) },
		"sam": func(vorname, nachname any) string {
			schema := vars["benutzername_schema"]
			if schema == "" {
				schema = "{v1}{n}"
			}
			v, n := ASCII(strings.ReplaceAll(toS(vorname), " ", "")), ASCII(strings.ReplaceAll(toS(nachname), " ", ""))
			first := func(s string, k int) string {
				if len(s) > k {
					return s[:k]
				}
				return s
			}
			r := strings.NewReplacer("{v}", v, "{n}", n, "{v1}", first(v, 1), "{v2}", first(v, 2), "{n1}", first(n, 1)).Replace(schema)
			r = strings.ToLower(r)
			if len(r) > 20 {
				r = r[:20]
			}
			return r
		},
		"datum": func(v any) string {
			if t, ok := parseDate(v); ok {
				return t.Format("02.01.2006")
			}
			return toS(v)
		},
		"date": func(layout string, v any) string {
			if t, ok := parseDate(v); ok {
				return t.Format(layout)
			}
			return toS(v)
		},
		"addDays": func(n int, v any) string {
			if t, ok := parseDate(v); ok {
				return t.AddDate(0, 0, n).Format("2006-01-02")
			}
			return toS(v)
		},
		"psdate": func(v any) string { // Get-Date-taugliches Format
			if t, ok := parseDate(v); ok {
				return "'" + t.Format("2006-01-02") + "'"
			}
			return "$null"
		},
		"today": func() string { return time.Now().Format("2006-01-02") },
		"join": func(sep string, v any) string {
			l := toList(v)
			p := make([]string, 0, len(l))
			for _, x := range l {
				if s := toS(x); s != "" {
					p = append(p, s)
				}
			}
			return strings.Join(p, sep)
		},
		"split": func(v any) []any { return toList(toS(v)) },
		"pluck": pluck,
		"uniq": func(v any) []any {
			seen := map[string]bool{}
			var out []any
			for _, x := range toList(v) {
				if s := toS(x); !seen[s] {
					seen[s] = true
					out = append(out, x)
				}
			}
			return out
		},
		"sortStr": func(v any) []any {
			l := append([]any(nil), toList(v)...)
			sort.Slice(l, func(i, j int) bool { return toS(l[i]) < toS(l[j]) })
			return l
		},
		"concat": func(vs ...any) []any {
			var out []any
			for _, v := range vs {
				out = append(out, toList(v)...)
			}
			return out
		},
		"default": func(def any, v any) any {
			if toS(v) == "" || toS(v) == "false" {
				return def
			}
			return v
		},
		"has": func(v any, x any) bool {
			for _, y := range toList(v) {
				if strings.EqualFold(str(y), str(x)) || strings.EqualFold(toS(y), toS(x)) {
					return true
				}
			}
			return false
		},
		"count": func(v any) int { return len(toList(v)) },
		"add":   func(a, b int) int { return a + b },
		"first": func(v any) any {
			if l := toList(v); len(l) > 0 {
				return l[0]
			}
			return ""
		},
		"lines": func(v any) []any { return toList(toS(v)) },
		"indent": func(n int, v any) string {
			pad := strings.Repeat(" ", n)
			return pad + strings.ReplaceAll(toS(v), "\n", "\n"+pad)
		},
		"ja": func(v any) string {
			if truthy(v) {
				return "Ja"
			}
			return "Nein"
		},
		"pstrue": func(v any) string {
			if truthy(v) {
				return "$true"
			}
			return "$false"
		},
		"num": func(v any) int { n, _ := strconv.Atoi(strings.TrimSpace(toS(v))); return n },
	}
}

var tplCache sync.Map

// Render führt eine Vorlage aus. Fehlende Werte werden zu leeren Zeichenketten.
func Render(src string, ctx map[string]any, vars map[string]string) (string, error) {
	if !strings.Contains(src, "{{") {
		return src, nil
	}
	key := src + "\x00" + vars["benutzername_schema"]
	var t *template.Template
	if c, ok := tplCache.Load(key); ok {
		t = c.(*template.Template)
	} else {
		var err error
		t, err = template.New("t").Option("missingkey=zero").Funcs(Funcs(vars)).Parse(src)
		if err != nil {
			return src, fmt.Errorf("Vorlage: %s", cleanErr(err))
		}
		tplCache.Store(key, t)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, ctx); err != nil {
		return b.String(), fmt.Errorf("Ausführung: %s", cleanErr(err))
	}
	return strings.ReplaceAll(b.String(), "<no value>", ""), nil
}

func cleanErr(err error) string {
	s := err.Error()
	s = strings.TrimPrefix(s, "template: t:")
	return s
}
