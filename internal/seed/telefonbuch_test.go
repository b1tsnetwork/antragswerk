package seed

import (
	"context"
	"path/filepath"
	"testing"

	"antragswerk/internal/flow"
	"antragswerk/internal/orgsync"
	"antragswerk/internal/store"
)

// Vorlage „Telefonbuch“ gegen die Beispieldatei, die mit dem Export des Telefonbuchs (pbucng,
// GET /api/export/org) erzeugt wurde – und gegen die mitgelieferten Kataloge.
func TestTelefonbuchPreset(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := Apply(ctx, st, false); err != nil {
		t.Fatal(err)
	}
	src := TelefonbuchPreset()
	src.Kind, src.URL = "file", filepath.Join("..", "web", "static", "telefonbuch-beispiel.json")
	res, stt := orgsync.Sync(ctx, st, src, "test")
	if !stt.OK {
		t.Fatalf("%+v", stt)
	}
	t.Log(stt.Counts)
	if res[0].Count != 9 || res[0].Updated < 6 { // 9 Ämter/Abteilungen, die meisten gibt es schon
		t.Fatalf("Einheiten: %+v", res[0])
	}
	if res[1].Count != 17 { // Pforte ohne Anmeldename fehlt
		t.Fatalf("Personen: %+v", res[1])
	}
	cats, _ := st.Catalogs(ctx)
	get := func(cat, key string) flow.Entry {
		for _, c := range cats {
			if c.Key == cat {
				for _, e := range c.Entries {
					if e.Key == key {
						return e
					}
				}
			}
		}
		t.Fatalf("%s/%s fehlt", cat, key)
		return flow.Entry{}
	}
	if e := get("abteilungen", "20"); e.Label != "Kämmerei (20)" || e.Group != "II – Finanzen" || e.Attrs["leitung"] != "s.wagner@demo-stadt.de" ||
		e.Attrs["vertretung"] != "f.krause@demo-stadt.de" || e.Attrs["ou"] == "" || e.Attrs["kostenstelle"] != "2000" || e.Src != "telefonbuch" {
		t.Fatalf("20: %+v", e)
	}
	if e := get("abteilungen", "10.3"); e.Attrs["uebergeordnet"] != "10" || e.Attrs["ebene"] != "Abteilung" {
		t.Fatalf("10.3: %+v", e)
	}
	if e := get("raeume", "Rathaus B 0.04"); e.Label != "Rathaus B 0.04 (Bürgerbüro)" || e.Attrs["schliessgruppe"] == "" || e.Attrs["abteilung"] != "32" || e.Attrs["personen"] != "2" {
		t.Fatalf("Raum: %+v", e)
	}
	if e := get("raeume", "Haus Jugend 2.01"); e.Label != "Haus Jugend 2.01" || e.Attrs["abteilung"] != "51" {
		t.Fatalf("neuer Raum: %+v", e)
	}
	p := st.SearchPersons(ctx, "krause", 5)
	if len(p) != 1 || p[0]["department"] != "Kämmerei (20)" || p[0]["title"] != "Stellvertretung Amt" || p[0]["manager"] != "Sabine Wagner" || p[0]["office"] != "Rathaus A 2.08" {
		t.Fatalf("Person: %v", p)
	}
}
