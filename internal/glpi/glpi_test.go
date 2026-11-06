package glpi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAgainstMock(t *testing.T) {
	m := NewMock("")
	srv := httptest.NewServer(m)
	defer srv.Close()
	ctx := context.Background()
	cl := New(Conn{URL: srv.URL, ClientID: "antragswerk", ClientSecret: "x", Username: "u", Password: "p"})
	if _, err := cl.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Conn{URL: srv.URL, ClientID: "falsch"}).Status(ctx); err == nil || !strings.Contains(err.Error(), "Client authentication failed") {
		t.Fatalf("falscher Client: %v", err)
	}
	id, err := cl.Resolve(ctx, "Group", "IT-Benutzerverwaltung")
	if err != nil || id != 3 {
		t.Fatalf("Gruppe: %d %v", id, err)
	}
	if id, _ := cl.Resolve(ctx, "ITILCategory", "IT > Benutzerkonten"); id != 10 {
		t.Fatalf("Kategorie: %d", id)
	}
	if id, _ := cl.Resolve(ctx, "User", "#42"); id != 42 {
		t.Fatalf("ID direkt: %d", id)
	}
	if _, err := cl.Resolve(ctx, "User", "gibtsnicht"); err == nil {
		t.Fatal("unbekannter Benutzer ohne Fehler")
	}
	if l, _ := cl.Search(ctx, "Location", "rathaus", 10); len(l) != 2 {
		t.Fatalf("Suche: %v", l)
	}
	tid, err := cl.Create(ctx, "Ticket", map[string]any{"name": "Test", "content": "<p>x</p>", "type": 2, "category": map[string]any{"id": 10}})
	if err != nil || tid == 0 {
		t.Fatalf("Create: %d %v", tid, err)
	}
	if err := cl.AddTeam(ctx, "Ticket", tid, "Group", 3, "assigned"); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.AddTimeline(ctx, "Ticket", tid, "Task", map[string]any{"content": "a", "state": 1}); err != nil {
		t.Fatal(err)
	}
	_ = m.Resolve("Ticket", tid, 5, "Benutzername: jmueller")
	it, err := cl.Get(ctx, "Ticket", tid)
	if err != nil {
		t.Fatal(err)
	}
	if st, name := ItemStatus("Ticket", it); st != 5 || name != "Gelöst" {
		t.Fatalf("Status %d %s", st, name)
	}
	sol, _ := cl.Timeline(ctx, "Ticket", tid, "Solution")
	if len(sol) != 1 {
		t.Fatalf("Lösung: %v", sol)
	}
	if _, err := cl.Create(ctx, "Ticket", map[string]any{"content": "ohne Titel"}); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("Fehlermeldung: %v", err)
	}
}
