package seed

import (
	"strings"
	"testing"

	"antragswerk/internal/flow"
)

func env(t *testing.T, d *flow.Def) *flow.Env {
	b, _, err := load()
	if err != nil {
		t.Fatal(err)
	}
	e := &flow.Env{Def: d, Catalogs: map[string]*flow.Catalog{}, Stellen: map[string]*flow.Stelle{}, Vars: map[string]string{}}
	for i := range b.Catalogs {
		e.Catalogs[b.Catalogs[i].Key] = &b.Catalogs[i]
	}
	for i := range b.Stellen {
		e.Stellen[b.Stellen[i].Key] = &b.Stellen[i]
	}
	for _, v := range b.Vars {
		e.Vars[v.Key] = v.Value
	}
	return e
}

func TestSeedsValid(t *testing.T) {
	_, defs, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) < 20 {
		t.Fatalf("nur %d Abläufe", len(defs))
	}
	for i := range defs {
		e := env(t, &defs[i])
		st, cat := map[string]bool{}, map[string]bool{}
		for k := range e.Stellen {
			st[k] = true
		}
		for k := range e.Catalogs {
			cat[k] = true
		}
		for _, is := range defs[i].Validate(st, cat) {
			if is.Level == "error" {
				t.Errorf("%s: %s %s", defs[i].Key, is.Ref, is.Msg)
			} else {
				t.Logf("%s: WARN %s %s", defs[i].Key, is.Ref, is.Msg)
			}
		}
		// Alle Skripte müssen mit leeren Daten fehlerfrei rendern
		ctx := e.Context(map[string]any{}, map[string]any{"nummer": "X-1"}, nil)
		for _, s := range defs[i].Steps {
			ctx["item"] = map[string]any{}
			if s.ForEach != "" {
				ctx["item"] = e.EmptyItem(s.ForEach)
			}
			for _, sc := range s.Scripts {
				if _, err := flow.Render(sc.Code, ctx, e.Vars); err != nil {
					t.Errorf("%s/%s/%s: %v", defs[i].Key, s.ID, sc.Title, err)
				}
			}
			for _, x := range []string{s.Instructions, s.Mail.Body, s.Mail.Subject} {
				if _, err := flow.Render(x, ctx, e.Vars); err != nil {
					t.Errorf("%s/%s text: %v", defs[i].Key, s.ID, err)
				}
			}
		}
	}
}

func TestEintrittSimulation(t *testing.T) {
	_, defs, _ := load()
	var d *flow.Def
	for i := range defs {
		if defs[i].Key == "eintritt" {
			d = &defs[i]
		}
	}
	e := env(t, d)
	data := map[string]any{
		"vorname": "Jürgen", "nachname": "Müller-Lüdenscheidt", "eintritt": "2026-12-01", "beschaeftigung": "tb", "abteilung": "32",
		"funktion": "Sachbearbeitung", "vorgesetzter": map[string]any{"sam": "m.schulz", "name": "Markus Schulz", "mail": "m.schulz@demo-stadt.de"},
		"telefon": "neu", "postfach": true, "geraet": "notebook", "postfaecher": []any{"buergerservice"},
		"programme":     []any{map[string]any{"software": "ewo", "rolle": "Sachbearbeitung"}, map[string]any{"software": "office"}},
		"verzeichnisse": []any{map[string]any{"verzeichnis": "buergerservice-vorlagen", "zugriff": "aendern"}},
	}
	plan := e.Simulate(data, map[string]any{"nummer": "EIN-2026-0001"})
	var lines []string
	for _, p := range plan {
		lines = append(lines, p.StepID+" "+p.Item+" skip="+map[bool]string{true: "j", false: "n"}[p.Skipped]+" → "+p.Assignee.Label)
	}
	t.Log("\n" + strings.Join(lines, "\n"))
	ctx := e.Context(data, map[string]any{"nummer": "EIN-2026-0001"}, nil)
	if c := ctx["c"].(map[string]any); c["sam"] != "j.mueller-luedensche" {
		t.Errorf("sam = %v", c["sam"])
	}
	s := d.StepByID("ad_konto")
	out, err := flow.Render(s.Scripts[1].Code, ctx, e.Vars)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + out)
	if !strings.Contains(out, "'FV-EWO-Benutzer'") || !strings.Contains(out, "'DL-FS01-32-Vorlagen-M'") || !strings.Contains(out, "'MB-Buergerservice-FA'") {
		t.Error("Gruppen fehlen im Skript")
	}
	vv := 0
	for _, p := range plan {
		if p.StepID == "fg_verfahren" && !p.Skipped {
			vv++
			if p.Assignee.Label != "l.weber@demo-stadt.de" {
				t.Errorf("Verfahrensverantwortung: %+v", p.Assignee)
			}
		}
	}
	if vv != 1 {
		t.Errorf("erwartet 1 Freigabe Verfahrensverantwortung, %d", vv)
	}
}
