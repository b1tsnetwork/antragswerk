package web

import "testing"

func TestTemplatesParse(t *testing.T) {
	s := &Server{}
	if err := s.parse(); err != nil {
		t.Fatal(err)
	}
	if len(s.tpl) < 15 {
		t.Fatalf("nur %d Seiten", len(s.tpl))
	}
}
