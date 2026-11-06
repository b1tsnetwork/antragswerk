package flow

// Planned ist ein vorhergesagter Schritt (Designer-Vorschau, "Was passiert danach?").
type Planned struct {
	StepID   string   `json:"step"`
	Title    string   `json:"title"`
	Kind     string   `json:"kind"`
	Item     string   `json:"item,omitempty"`
	Skipped  bool     `json:"skipped"`
	Reason   string   `json:"reason,omitempty"`
	Assignee Assignee `json:"assignee"`
	Level    int      `json:"level"`
}

// Levels berechnet die Ebene (längster Weg vom Start) je Schritt – gleiche Ebene = parallel.
func (d *Def) Levels() map[string]int {
	lv := map[string]int{}
	order, err := d.Order()
	if err != nil {
		for i, s := range d.Steps {
			lv[s.ID] = i
		}
		return lv
	}
	for _, s := range order {
		l := 0
		for _, p := range s.After {
			if v, ok := lv[p]; ok && v+1 > l {
				l = v + 1
			}
		}
		lv[s.ID] = l
	}
	return lv
}

// Simulate sagt voraus, welche Schritte für die Daten laufen (alle Freigaben positiv angenommen).
func (e *Env) Simulate(data map[string]any, meta map[string]any) []Planned {
	ctx := e.Context(data, meta, nil)
	order, err := e.Def.Order()
	if err != nil {
		return nil
	}
	lv := e.Def.Levels()
	label := func(f string) string { return e.FieldLabel(f) }
	var out []Planned
	for _, s := range order {
		if s.ForEach != "" {
			items := e.Items(ctx, s.ForEach)
			if len(items) == 0 {
				out = append(out, Planned{StepID: s.ID, Title: s.Title, Kind: s.Kind, Skipped: true, Reason: "keine Einträge in „" + label(s.ForEach) + "“", Level: lv[s.ID]})
				continue
			}
			for _, it := range items {
				p := Planned{StepID: s.ID, Title: s.Title, Kind: s.Kind, Item: ItemLabel(it), Level: lv[s.ID]}
				if !s.When.Match(ctx, it) {
					p.Skipped, p.Reason = true, "Bedingung nicht erfüllt: "+s.When.Text(label)
				} else if s.Kind != "warten" {
					p.Assignee = e.Resolve(s, ctx, it)
				}
				out = append(out, p)
			}
			continue
		}
		p := Planned{StepID: s.ID, Title: s.Title, Kind: s.Kind, Level: lv[s.ID]}
		if !s.When.Match(ctx, nil) {
			p.Skipped, p.Reason = true, "Bedingung nicht erfüllt: "+s.When.Text(label)
		} else if s.Kind != "warten" {
			p.Assignee = e.Resolve(s, ctx, nil)
		}
		out = append(out, p)
	}
	return out
}

// FieldLabel liefert die Beschriftung zu einem Feldpfad.
func (e *Env) FieldLabel(path string) string {
	for _, fr := range e.Def.AllFields() {
		if fr.Path == path {
			return fr.Field.Label
		}
	}
	if len(path) > 5 && path[:5] == "item." {
		for _, fr := range e.Def.AllFields() {
			if fr.Parent != nil && fr.Field.Key == path[5:] {
				return fr.Field.Label
			}
		}
	}
	return path
}
