package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"antragswerk/internal/flow"
	"antragswerk/internal/glpi"
	"antragswerk/internal/store"
)

func glpiSetup(t *testing.T, steps []flow.Step) (*App, context.Context, *glpi.Mock, *store.Workflow) {
	a, ctx := setup(t)
	a.GLPISync = true
	m := glpi.NewMock("")
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	_ = a.Store.PutDoc(ctx, "glpi", "glpi", glpi.Conn{Key: "glpi", Name: "Test", URL: srv.URL, ClientID: "antragswerk", Username: "u", Password: "p", Entity: "Stadtverwaltung", Language: "de_DE"}, "t", 0)
	d := flow.Def{Key: "glpitest", Name: "GLPI-Test", Prefix: "GT", TitleTpl: "Test {{.anliegen}}",
		Form:  flow.Form{Sections: []flow.Section{{ID: "s", Title: "S", Fields: []flow.Field{{Key: "anliegen", Label: "Anliegen", Type: "text", Required: true}}}}},
		Steps: steps}
	if is := d.Validate(nil, nil); len(is) > 0 {
		for _, i := range is {
			if i.Level == "error" {
				t.Fatalf("Definition: %+v", is)
			}
		}
	}
	wf, err := a.Store.CreateWorkflow(ctx, d, "t", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	return a, ctx, m, wf
}

func inst(a *App, ctx context.Context, reqID int64, step string) *store.Instance {
	ins, _ := a.Store.Instances(ctx, reqID)
	for _, in := range ins {
		if in.StepID == step {
			return in
		}
	}
	return nil
}

var requester = Actor{User: "a.becker", Name: "Anna Becker", Mail: "a.becker@demo-stadt.de"}

func TestGLPITicketRoundTrip(t *testing.T) {
	steps := []flow.Step{
		{ID: "tk", Title: "Konto anlegen", Kind: "glpi", Assign: flow.Assign{Type: "stelle", Stelle: "it_benutzer"},
			Outputs: []flow.Field{{Key: "benutzername", Label: "Benutzername", Type: "text", Required: true}},
			GLPI: &flow.GLPIStep{Type: "ticket", Name: "Antrag {{.antrag.nummer}}: {{.anliegen}}", TicketType: "2", Urgency: "4", Category: "IT > Benutzerkonten",
				RequestType: "Antragswerk", Location: "21",
				Content: "Bitte {{.anliegen}} erledigen.\nRückmeldung: {{.glpi.rueckmeldung}}",
				Team:    []flow.GLPITeam{{Role: "assigned", Type: "Group", Value: "IT-Benutzerverwaltung"}, {Role: "observer", Type: "User", Value: "a.becker; unbekannt"}},
				Tasks:   []flow.GLPITask{{Content: "Konto für {{.anliegen}}", Duration: "30", GroupTech: "IT-Benutzerverwaltung", Category: "Konto"}},
			}},
		{ID: "fu", Title: "Info an GLPI", Kind: "glpi", After: []string{"tk"}, Assign: flow.Assign{Type: "stelle", Stelle: "it_benutzer"},
			GLPI: &flow.GLPIStep{Action: "followup", Ref: "tk", Content: "Danke, Konto {{.out.tk.benutzername}} übernommen."}},
		{ID: "ende", Title: "Abschluss", Kind: "aufgabe", After: []string{"fu"}, Instructions: "Ticket #{{.out.tk.glpi_id}}", Assign: flow.Assign{Type: "stelle", Stelle: "it_benutzer"}},
	}
	a, ctx, m, wf := glpiSetup(t, steps)
	r, errs, err := a.Submit(ctx, wf, map[string]any{"anliegen": "Konto Müller"}, requester)
	if err != nil || len(errs) > 0 {
		t.Fatal(err, errs)
	}
	in := inst(a, ctx, r.ID, "tk")
	if in.State != "extern" || in.Outputs["glpi_phase"] != "offen" {
		t.Fatalf("nach Übergabe: %s %v", in.State, in.Outputs)
	}
	id := glpi.ID(in.Outputs["glpi_id"])
	data, team, tl := m.Item("Ticket", id)
	if data["name"] != "Antrag "+r.Number+": Konto Müller" || glpi.ID(data["type"]) != 2 || glpi.ID(data["urgency"]) != 4 {
		t.Fatalf("Ticket: %v", data)
	}
	for k, want := range map[string]int64{"category": 10, "request_type": 7, "location": 21, "entity": 1} {
		if glpi.ID(data[k]) != want {
			t.Fatalf("%s = %v, erwartet %d", k, data[k], want)
		}
	}
	if data["external_id"] != r.Number+"/tk" {
		t.Fatalf("external_id %v", data["external_id"])
	}
	content := data["content"].(string)
	if !strings.Contains(content, `<a href="http://x/glpi/r/`+in.Token+`">`) || !strings.Contains(content, "Bitte Konto Müller erledigen.") {
		t.Fatalf("Inhalt: %s", content)
	}
	if len(team) != 2 || team[0]["role"] != "assigned" || glpi.ID(team[0]["id"]) != 3 || glpi.ID(team[1]["id"]) != 9 {
		t.Fatalf("Team: %v", team)
	}
	if !strings.Contains(str(in.Outputs["glpi_warn"]), "unbekannt") {
		t.Fatalf("Warnung zum unbekannten Teammitglied fehlt: %v", in.Outputs)
	}
	task := tl["Task"][0]
	if glpi.ID(task["duration"]) != 1800 || glpi.ID(task["group_tech"]) != 3 || glpi.ID(task["category"]) != 30 {
		t.Fatalf("Aufgabe: %v", task)
	}
	// noch nicht gelöst → bleibt offen
	a.GLPIProcess(ctx, in.ID)
	if in = inst(a, ctx, r.ID, "tk"); in.State != "extern" {
		t.Fatalf("vor Lösung: %s", in.State)
	}
	// Techniker löst mit Angabe im Lösungstext → Abfrage übernimmt Status und Rückmeldefeld
	_ = m.Resolve("Ticket", id, 5, "<p>Erledigt.<br>Benutzername: kmueller</p>")
	a.GLPIProcess(ctx, in.ID)
	in = inst(a, ctx, r.ID, "tk")
	if in.State != "erledigt" || in.Outputs["benutzername"] != "kmueller" || !strings.Contains(in.DoneByName, "Gelöst") {
		t.Fatalf("nach Lösung: %s %q %v", in.State, in.DoneByName, in.Outputs)
	}
	// Folgeeintrag geschrieben, nächster Schritt offen mit Ticketnummer
	if fu := inst(a, ctx, r.ID, "fu"); fu.State != "erledigt" {
		t.Fatalf("Folgeeintrag: %s %v", fu.State, fu.Outputs)
	}
	_, _, tl = m.Item("Ticket", id)
	if len(tl["Followup"]) != 1 || !strings.Contains(str(tl["Followup"][0]["content"]), "Konto kmueller übernommen") {
		t.Fatalf("Followup: %v", tl["Followup"])
	}
	if e := inst(a, ctx, r.ID, "ende"); e == nil || e.State != "offen" {
		t.Fatalf("Abschluss-Schritt: %v", states(t, a, ctx, r.ID))
	}
}

func TestGLPIFeedbackLinkAndWebhook(t *testing.T) {
	steps := []flow.Step{{ID: "tk", Title: "Hardware", Kind: "glpi", Assign: flow.Assign{Type: "stelle", Stelle: "it_client"},
		Outputs: []flow.Field{{Key: "inventar", Label: "Inventarnummer", Type: "text", Required: true}},
		GLPI:    &flow.GLPIStep{Name: "HW {{.anliegen}}", Content: "{{.glpi.rueckmeldung}}"}}}
	a, ctx, _, wf := glpiSetup(t, steps)
	r, _, _ := a.Submit(ctx, wf, map[string]any{"anliegen": "Notebook"}, requester)
	in := inst(a, ctx, r.ID, "tk")
	if err := a.GLPIRespond(ctx, in.Token, GLPIFeedback{Status: "erledigt"}); err == nil || !strings.Contains(err.Error(), "Inventarnummer") {
		t.Fatalf("Pflichtfeld: %v", err)
	}
	if err := a.GLPIRespond(ctx, in.Token, GLPIFeedback{Status: "erledigt", By: "Thomas Klein", Comment: "ausgegeben", Outputs: map[string]any{"inventar": "NB-4711"}}); err != nil {
		t.Fatal(err)
	}
	in = inst(a, ctx, r.ID, "tk")
	if in.State != "erledigt" || in.Outputs["inventar"] != "NB-4711" || in.DoneByName != "Thomas Klein (über GLPI)" {
		t.Fatalf("Rückmeldung: %s %s %v", in.State, in.DoneByName, in.Outputs)
	}
	if err := a.GLPIRespond(ctx, in.Token, GLPIFeedback{Status: "erledigt"}); err == nil {
		t.Fatal("Token nach Erledigung noch gültig")
	}
	r2, _, _ := a.Submit(ctx, wf, map[string]any{"anliegen": "Monitor"}, requester)
	in2 := inst(a, ctx, r2.ID, "tk")
	if n := a.GLPIWebhook(ctx, "glpi", map[string]any{"event": "update", "item": map[string]any{"id": in2.Outputs["glpi_id"]}}, nil); n != 1 {
		t.Fatalf("Webhook traf %d Schritte", n)
	}
}

func TestGLPIChangeRejectedAndRetry(t *testing.T) {
	steps := []flow.Step{{ID: "ch", Title: "Change", Kind: "glpi", Assign: flow.Assign{Type: "stelle", Stelle: "it_benutzer"},
		GLPI: &flow.GLPIStep{Type: "change", Name: "Change {{.anliegen}}", OnFail: "ablehnen", RolloutPlan: "1. Schritt\n2. Schritt",
			Validation: &flow.GLPIValidation{ApproverType: "Group", Approver: "Change Advisory Board", Comment: "Bitte prüfen"}, Extra: `{"time_to_own": "2030-01-01 10:00:00"}`}}}
	a, ctx, m, wf := glpiSetup(t, steps)
	m.FailNext = 1
	r, _, _ := a.Submit(ctx, wf, map[string]any{"anliegen": "Firewall"}, requester)
	in := inst(a, ctx, r.ID, "ch")
	if in.Outputs["glpi_phase"] != "senden" || glpi.ID(in.Outputs["glpi_tries"]) != 1 || !strings.Contains(str(in.Outputs["glpi_error"]), "Simulierter Fehler") {
		t.Fatalf("Fehlversuch: %v", in.Outputs)
	}
	if err := a.GLPIAction(ctx, in.ID, "erneut", admin); err != nil {
		t.Fatal(err)
	}
	in = inst(a, ctx, r.ID, "ch")
	id := glpi.ID(in.Outputs["glpi_id"])
	data, _, tl := m.Item("Change", id)
	if data == nil || !strings.Contains(str(data["rolloutplancontent"]), "1. Schritt<br>2. Schritt") || data["time_to_own"] != "2030-01-01 10:00:00" || data["external_id"] != nil {
		t.Fatalf("Change: %v", data)
	}
	if v := tl["Validation"]; len(v) != 1 || v[0]["requested_approver_type"] != "Group" || glpi.ID(v[0]["requested_approver_id"]) != 5 {
		t.Fatalf("Genehmigung: %v", tl["Validation"])
	}
	_ = m.Resolve("Change", id, 13, "CAB: Risiko zu hoch")
	a.GLPIProcess(ctx, in.ID)
	got, _ := a.Store.Request(ctx, r.ID)
	if got.Status != "abgelehnt" {
		t.Fatalf("Vorgang: %s", got.Status)
	}
}

func TestGLPIUnreachableFallsBackToTask(t *testing.T) {
	a, ctx := setup(t)
	a.GLPISync = true
	_ = a.Store.PutDoc(ctx, "glpi", "glpi", glpi.Conn{Key: "glpi", URL: "http://127.0.0.1:1", ClientID: "x"}, "t", 0)
	d := flow.Def{Key: "g2", Name: "G2", TitleTpl: "x", Form: flow.Form{Sections: []flow.Section{{ID: "s", Title: "S", Fields: []flow.Field{{Key: "a", Label: "A", Type: "text"}}}}},
		Steps: []flow.Step{{ID: "tk", Title: "Ticket", Kind: "glpi", Assign: flow.Assign{Type: "stelle", Stelle: "it_benutzer"}, GLPI: &flow.GLPIStep{Name: "{{.a}}"}}}}
	wf, _ := a.Store.CreateWorkflow(ctx, d, "t", true, 0)
	r, _, _ := a.Submit(ctx, wf, map[string]any{"a": "x"}, requester)
	in := inst(a, ctx, r.ID, "tk")
	for i := 0; i < maxGLPITries-1; i++ {
		in.Outputs["glpi_next"] = ""
		_ = a.Store.UpdateInstance(ctx, in)
		a.GLPIProcess(ctx, in.ID)
		in = inst(a, ctx, r.ID, "tk")
	}
	if in.State != "offen" || in.Outputs["glpi_phase"] != "fehler" || !strings.Contains(in.Comment, "nicht erreichbar") {
		t.Fatalf("Rückfall: %s %v %q", in.State, in.Outputs, in.Comment)
	}
	if err := a.Act(ctx, in.ID, ActInput{Action: "erledigt", Comment: "manuell angelegt"}, Actor{User: "t.klein", Stellen: []string{"it_benutzer"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.Store.Request(ctx, r.ID); got.Status != "abgeschlossen" {
		t.Fatalf("Vorgang: %s", got.Status)
	}
}

func TestGLPISeedWorkflows(t *testing.T) {
	a, ctx := setup(t)
	a.GLPISync = true
	m := glpi.NewMock("")
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	_ = a.Store.PutDoc(ctx, "glpi", "glpi", glpi.Conn{Key: "glpi", URL: srv.URL, ClientID: "antragswerk", Entity: "Stadtverwaltung"}, "t", 0)
	itl := Actor{User: "admin", Role: "admin", Stellen: []string{"it_leitung"}}
	// Change
	wf, _ := a.Store.Workflow(ctx, "change")
	r, errs, err := a.Submit(ctx, wf, map[string]any{"titel": "Firewall-Regel Bürgerportal", "system": "netz", "termin": "2030-03-10", "beschreibung": "Port 443 freigeben",
		"begruendung": "Neues Portal", "auswirkung": "3", "ausfall": true, "ausfall_info": "10 Minuten", "plan": "Regel anlegen\nTesten", "rueckfall": "Regel löschen"}, requester)
	if err != nil || len(errs) > 0 {
		t.Fatal(err, errs)
	}
	if err := a.Act(ctx, find(t, a, ctx, r.ID, "fg_leitung").ID, ActInput{Action: "freigeben"}, itl); err != nil {
		t.Fatal(err)
	}
	in := inst(a, ctx, r.ID, "change")
	if in.State != "extern" || str(in.Outputs["glpi_warn"]) != "" || str(in.Outputs["glpi_error"]) != "" {
		t.Fatalf("Change: %s %v", in.State, in.Outputs)
	}
	id := glpi.ID(in.Outputs["glpi_id"])
	data, team, tl := m.Item("Change", id)
	if glpi.ID(data["impact"]) != 3 || data["resolution_date"] != "2030-03-10 23:59:00" || !strings.Contains(str(data["backoutplancontent"]), "Regel löschen") ||
		!strings.Contains(str(data["content"]), "freigegeben von admin") || len(team) != 2 || len(tl["Task"]) != 1 || len(tl["Validation"]) != 1 {
		t.Fatalf("Change-Daten: %v team=%v tl=%v", data, team, tl)
	}
	if tl["Task"][0]["planned_begin"] != "2030-03-10 08:00:00" {
		t.Fatalf("Aufgabe Beginn: %v", tl["Task"][0])
	}
	_ = m.Resolve("Change", id, 5, "Umgesetzt am: 10.03.2030\nHinweis: ohne Ausfall")
	a.GLPIProcess(ctx, in.ID)
	if got, _ := a.Store.Request(ctx, r.ID); got.Status != "abgeschlossen" {
		t.Fatalf("Change-Vorgang: %s %v", got.Status, states(t, a, ctx, r.ID))
	}
	mails, _ := a.Store.Mails(ctx, r.ID, 20)
	found := false
	for _, ml := range mails {
		if strings.Contains(ml.Subject, "Change umgesetzt") && strings.Contains(ml.Body, "am 10.03.2030") && strings.Contains(ml.Body, "Hinweis: ohne Ausfall") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Abschlussmail fehlt/unvollständig")
	}
	// Geräte je Position
	wf2, _ := a.Store.Workflow(ctx, "hardware_glpi")
	schulz := map[string]any{"sam": "m.schulz", "name": "Markus Schulz", "mail": "m.schulz@demo-stadt.de"}
	r2, errs, err := a.Submit(ctx, wf2, map[string]any{"person": map[string]any{"sam": "p.neumann", "name": "Petra Neumann", "department": "Bürgerservice (32)"}, "vorgesetzter": schulz,
		"bis": "2030-01-15", "geraete": []any{map[string]any{"art": "notebook"}, map[string]any{"art": "monitor", "hinweis": "2 Stück"}}}, requester)
	if err != nil || len(errs) > 0 {
		t.Fatal(err, errs)
	}
	if err := a.Act(ctx, find(t, a, ctx, r2.ID, "fg_vorgesetzt").ID, ActInput{Action: "freigeben"}, Actor{User: "m.schulz", Mail: "m.schulz@demo-stadt.de"}); err != nil {
		t.Fatal(err)
	}
	ins, _ := a.Store.Instances(ctx, r2.ID)
	var tickets []*store.Instance
	for _, x := range ins {
		if x.StepID == "ticket" {
			tickets = append(tickets, x)
		}
	}
	if len(tickets) != 2 {
		t.Fatalf("Tickets: %v", states(t, a, ctx, r2.ID))
	}
	d1, _, _ := m.Item("Ticket", glpi.ID(tickets[1].Outputs["glpi_id"]))
	if d1["name"] != "Monitor für Petra Neumann ("+r2.Number+")" || !strings.Contains(str(d1["content"]), "Hinweis: 2 Stück") || d1["external_id"] != r2.Number+"/ticket#2" {
		t.Fatalf("Ticket 2: %v", d1)
	}
	_ = a.GLPIRespond(ctx, tickets[0].Token, GLPIFeedback{Outputs: map[string]any{"inventar": "NB-1"}})
	_ = m.Resolve("Ticket", glpi.ID(tickets[1].Outputs["glpi_id"]), 6, "Inventarnummer: MON-7")
	a.GLPIProcess(ctx, tickets[1].ID)
	if got, _ := a.Store.Request(ctx, r2.ID); got.Status != "abgeschlossen" {
		t.Fatalf("Geräte-Vorgang: %s %v", got.Status, states(t, a, ctx, r2.ID))
	}
	mails, _ = a.Store.Mails(ctx, r2.ID, 20)
	ok := false
	for _, ml := range mails {
		if strings.Contains(ml.Body, "Notebook: Inventarnummer NB-1") && strings.Contains(ml.Body, "Monitor: Inventarnummer MON-7") {
			ok = true
		}
	}
	if !ok {
		for _, ml := range mails {
			t.Logf("MAIL %s\n%s", ml.Subject, ml.Body)
		}
		t.Fatal("Mail mit Inventarnummern fehlt")
	}
}
