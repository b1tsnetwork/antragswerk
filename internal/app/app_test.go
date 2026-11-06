package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"antragswerk/internal/config"
	"antragswerk/internal/mail"
	"antragswerk/internal/seed"
	"antragswerk/internal/store"
)

func setup(t *testing.T) (*App, context.Context) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := seed.Apply(ctx, st, true); err != nil {
		t.Fatal(err)
	}
	return &App{Cfg: &config.Config{BaseURL: "http://x"}, Store: st, Mailer: mail.New(config.SMTP{})}, ctx
}

func states(t *testing.T, a *App, ctx context.Context, id int64) map[string][]string {
	ins, _ := a.Store.Instances(ctx, id)
	m := map[string][]string{}
	for _, in := range ins {
		m[in.StepID] = append(m[in.StepID], in.State)
	}
	return m
}

func find(t *testing.T, a *App, ctx context.Context, id int64, step string) *store.Instance {
	ins, _ := a.Store.Instances(ctx, id)
	for _, in := range ins {
		if in.StepID == step && in.State == "offen" {
			return in
		}
	}
	t.Fatalf("kein offener Schritt %s: %v", step, states(t, a, ctx, id))
	return nil
}

var admin = Actor{User: "admin", Name: "Admin", Role: "admin"}

func TestAustrittWithPastDate(t *testing.T) {
	a, ctx := setup(t)
	wf, _ := a.Store.Workflow(ctx, "austritt")
	r, errs, err := a.Submit(ctx, wf, map[string]any{
		"person": map[string]any{"sam": "p.neumann", "name": "Petra Neumann", "mail": "p.neumann@demo-stadt.de"}, "letzter_tag": time.Now().AddDate(0, 0, -10).Format("2006-01-02"), "grund": "ruhestand",
		"vorgesetzter": map[string]any{"sam": "m.schulz", "name": "Markus Schulz", "mail": "m.schulz@demo-stadt.de"},
		"nachfolge":    map[string]any{"sam": "m.schulz", "name": "Markus Schulz", "mail": "m.schulz@demo-stadt.de"}, "postfach": "shared", "home_uebergabe": true,
		"fachverfahren": []any{"ewo", "office"}, "rueckgabe": []any{"notebook", "schluessel"},
	}, Actor{User: "a.becker", Name: "Anna Becker", Mail: "a.becker@demo-stadt.de"})
	if err != nil || len(errs) > 0 {
		t.Fatal(err, errs)
	}
	if !strings.Contains(r.Title, "Petra Neumann") {
		t.Errorf("Titel %q", r.Title)
	}
	// Personal bestätigt → Stichtag liegt in der Vergangenheit → Sperren sofort offen
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "fg_personal").ID, ActInput{Action: "freigeben"}, Actor{User: "a.becker", Stellen: []string{"personal"}}); err != nil {
		t.Fatal(err)
	}
	s := states(t, a, ctx, r.ID)
	if s["stichtag"][0] != "versendet" || s["sperren"][0] != "offen" || s["sperren_sofort"][0] != "uebersprungen" || s["hardware"][0] != "offen" {
		t.Fatalf("Zustände: %v", s)
	}
	// Nicht zuständige Person darf nicht
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "sperren").ID, ActInput{Action: "erledigt"}, Actor{User: "x"}); err != ErrForbidden {
		t.Fatalf("erwartet ErrForbidden, %v", err)
	}
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "sperren").ID, ActInput{Action: "erledigt"}, Actor{User: "t.klein", Stellen: []string{"it_benutzer"}}); err != nil {
		t.Fatal(err)
	}
	s = states(t, a, ctx, r.ID)
	if len(s["fv_sperren"]) != 2 || s["fv_sperren"][0] != "offen" || s["fv_sperren"][1] != "uebersprungen" {
		t.Fatalf("fv_sperren: %v", s["fv_sperren"])
	}
	for _, st := range []string{"postfach", "daten", "fv_sperren", "hardware", "zutritt", "telefon"} {
		if err := a.Act(ctx, find(t, a, ctx, r.ID, st).ID, ActInput{Action: "erledigt"}, admin); err != nil {
			t.Fatal(st, err)
		}
	}
	s = states(t, a, ctx, r.ID)
	if s["frist"][0] != "geplant" {
		t.Fatalf("Löschfrist: %v", s)
	}
	mails, _ := a.Store.Mails(ctx, r.ID, 100)
	if len(mails) < 5 {
		t.Errorf("nur %d Mails", len(mails))
	}
}

func TestRueckfrageUndAblehnung(t *testing.T) {
	a, ctx := setup(t)
	wf, _ := a.Store.Workflow(ctx, "software")
	data := map[string]any{
		"person":       map[string]any{"sam": "p.neumann", "name": "Petra Neumann", "mail": "p.neumann@demo-stadt.de"},
		"vorgesetzter": map[string]any{"sam": "m.schulz", "name": "Markus Schulz", "mail": "m.schulz@demo-stadt.de"},
		"programme":    []any{map[string]any{"software": "ewo", "begruendung": "x"}, map[string]any{"software": "acrobat", "begruendung": "y"}},
	}
	_, errs, _ := a.Submit(ctx, wf, map[string]any{"programme": []any{}}, Actor{User: "a"})
	if len(errs) == 0 {
		t.Fatal("Pflichtfelder nicht geprüft")
	}
	r, _, err := a.Submit(ctx, wf, data, Actor{User: "a.becker", Mail: "a.becker@demo-stadt.de"})
	if err != nil {
		t.Fatal(err)
	}
	vg := Actor{User: "m.schulz", Mail: "m.schulz@demo-stadt.de"}
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "fg_vorgesetzt").ID, ActInput{Action: "rueckfrage"}, vg); err == nil {
		t.Fatal("Rückfrage ohne Text erlaubt")
	}
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "fg_vorgesetzt").ID, ActInput{Action: "rueckfrage", Comment: "Wofür Acrobat?"}, vg); err != nil {
		t.Fatal(err)
	}
	r2, _ := a.Store.Request(ctx, r.ID)
	if r2.Status != "rueckfrage" {
		t.Fatal(r2.Status)
	}
	data["programme"] = []any{map[string]any{"software": "ewo", "begruendung": "x"}}
	if errs, err := a.Resubmit(ctx, r.ID, data, "Acrobat doch nicht nötig", Actor{User: "a.becker"}); err != nil || len(errs) > 0 {
		t.Fatal(err, errs)
	}
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "fg_vorgesetzt").ID, ActInput{Action: "freigeben"}, vg); err != nil {
		t.Fatal(err)
	}
	s := states(t, a, ctx, r.ID)
	if len(s["fg_verfahren"]) != 1 {
		t.Fatalf("erwartet 1 Verfahrensfreigabe: %v", s)
	}
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "fg_verfahren").ID, ActInput{Action: "ablehnen", Comment: "Keine Sachbearbeitung im Meldewesen"}, Actor{User: "l.weber", Mail: "l.weber@demo-stadt.de"}); err != nil {
		t.Fatal(err)
	}
	r3, _ := a.Store.Request(ctx, r.ID)
	if r3.Status != "abgelehnt" {
		t.Fatal(r3.Status)
	}
}

func TestRezertifizierungListe(t *testing.T) {
	a, ctx := setup(t)
	wf, _ := a.Store.Workflow(ctx, "rezertifizierung")
	r, errs, err := a.Submit(ctx, wf, map[string]any{"zeitraum": "2026", "frist": "2026-12-01", "verzeichnisse": []any{"kaemmerei-haushalt", "bau-planung"}}, admin)
	if err != nil || len(errs) > 0 {
		t.Fatal(err, errs)
	}
	s := states(t, a, ctx, r.ID)
	if len(s["pruefung_vz"]) != 2 || s["pruefung_pf"][0] != "uebersprungen" {
		t.Fatalf("%v", s)
	}
	for i, txt := range []string{"keine Änderungen", "x.alt entfernen"} {
		in := find(t, a, ctx, r.ID, "pruefung_vz")
		if err := a.Act(ctx, in.ID, ActInput{Action: "erledigt", Outputs: map[string]any{"entfernen": txt}}, admin); err != nil {
			t.Fatal(i, err)
		}
	}
	req, env, ins, _ := a.Load(ctx, r.ID)
	st := env.Def.StepByID("umsetzen")
	v := a.RenderStep(env, a.Context(env, req, ins), st, find(t, a, ctx, r.ID, "umsetzen"))
	if !strings.Contains(v.Instructions, "Kämmerei – Haushalt") || !strings.Contains(v.Instructions, "x.alt entfernen") {
		t.Fatalf("Liste fehlt:\n%s", v.Instructions)
	}
}
