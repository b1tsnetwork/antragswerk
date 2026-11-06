package flow

import (
	"strconv"
	"strings"
)

// Cond ist eine Bedingung aus Regeln, verknüpft mit und (all) bzw. oder (any).
type Cond struct {
	Mode  string `json:"mode" yaml:"mode"`
	Rules []Rule `json:"rules" yaml:"rules"`
}

// Rule vergleicht ein Feld (Pfad) mit Werten.
type Rule struct {
	Field  string   `json:"field" yaml:"field"`
	Op     string   `json:"op" yaml:"op"`
	Values []string `json:"values,omitempty" yaml:"values,omitempty"`
}

// Ops sind die verfügbaren Vergleiche (identisch in form.js / designer.js).
var Ops = []struct {
	Key, Label string
	Values     int
}{
	{"eq", "ist", 1}, {"ne", "ist nicht", 1}, {"in", "ist eines von", 2}, {"nin", "ist keines von", 2},
	{"set", "ist ausgefüllt", 0}, {"unset", "ist leer", 0}, {"true", "ist angehakt", 0}, {"false", "ist nicht angehakt", 0},
	{"contains", "enthält", 1}, {"ncontains", "enthält nicht", 1},
	{"count_gte", "Anzahl mindestens", 1}, {"count_lt", "Anzahl weniger als", 1},
	{"gte", "≥", 1}, {"lte", "≤", 1},
	{"same", "gleich wie Feld", 1}, {"changed", "weicht ab von Feld", 1},
}

// Match wertet die Bedingung gegen den Kontext aus (nil/leer = immer wahr).
func (c *Cond) Match(ctx map[string]any, item any) bool {
	if c == nil || len(c.Rules) == 0 {
		return true
	}
	any := c.Mode == "any"
	for _, r := range c.Rules {
		ok := r.match(ctx, item)
		if any && ok {
			return true
		}
		if !any && !ok {
			return false
		}
	}
	return !any
}

// norm macht aus einem Kontextwert vergleichbare Zeichenketten.
func norm(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		var out []string
		for _, y := range x {
			out = append(out, norm(y)...)
		}
		return out
	case map[string]any:
		var out []string
		for k, y := range x {
			if k == "nr" {
				continue
			}
			out = append(out, norm(y)...)
		}
		return out
	}
	s := str(v)
	if s == "" {
		return nil
	}
	return []string{s}
}

func (r Rule) match(ctx map[string]any, item any) bool {
	raw := pick(ctx, item, r.Field)
	vals := norm(raw)
	first := ""
	if len(r.Values) > 0 {
		first = r.Values[0]
	}
	has := func(x string) bool {
		for _, v := range vals {
			if strings.EqualFold(v, x) {
				return true
			}
		}
		return false
	}
	count := len(vals)
	if lst, ok := raw.([]any); ok {
		count = len(lst)
	}
	switch r.Op {
	case "eq":
		return has(first)
	case "ne":
		return !has(first)
	case "in":
		for _, x := range r.Values {
			if has(x) {
				return true
			}
		}
		return false
	case "nin":
		for _, x := range r.Values {
			if has(x) {
				return false
			}
		}
		return true
	case "set":
		return len(vals) > 0 && !(len(vals) == 1 && vals[0] == "false")
	case "unset":
		return len(vals) == 0 || (len(vals) == 1 && vals[0] == "false")
	case "true":
		return has("true")
	case "false":
		return !has("true")
	case "contains":
		if has(first) {
			return true
		}
		for _, v := range vals {
			if strings.Contains(strings.ToLower(v), strings.ToLower(first)) {
				return true
			}
		}
		return false
	case "ncontains":
		return !Rule{r.Field, "contains", r.Values}.match(ctx, item)
	case "count_gte":
		n, _ := strconv.Atoi(first)
		return count >= n
	case "count_lt":
		n, _ := strconv.Atoi(first)
		return count < n
	case "gte", "lte":
		if len(vals) == 0 {
			return false
		}
		a, e1 := strconv.ParseFloat(strings.ReplaceAll(vals[0], ",", "."), 64)
		b, e2 := strconv.ParseFloat(strings.ReplaceAll(first, ",", "."), 64)
		if e1 != nil || e2 != nil { // Datum / Text lexikografisch
			if r.Op == "gte" {
				return vals[0] >= first
			}
			return vals[0] <= first
		}
		if r.Op == "gte" {
			return a >= b
		}
		return a <= b
	case "same":
		other := norm(pick(ctx, item, first))
		return len(vals) > 0 && strings.Join(vals, "|") == strings.Join(other, "|")
	case "changed":
		other := norm(pick(ctx, item, first))
		return strings.Join(vals, "|") != strings.Join(other, "|")
	}
	return false
}

// Text beschreibt die Bedingung lesbar.
func (c *Cond) Text(label func(string) string) string {
	if c == nil || len(c.Rules) == 0 {
		return ""
	}
	parts := make([]string, 0, len(c.Rules))
	for _, r := range c.Rules {
		op := r.Op
		for _, o := range Ops {
			if o.Key == r.Op {
				op = o.Label
			}
		}
		p := label(r.Field) + " " + op
		if len(r.Values) > 0 {
			p += " " + strings.Join(r.Values, ", ")
		}
		parts = append(parts, p)
	}
	j := " und "
	if c.Mode == "any" {
		j = " oder "
	}
	return strings.Join(parts, j)
}
