package orgsync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"antragswerk/internal/flow"
	"antragswerk/internal/store"
)

// Allgemeine Quelle mit verschachtelter Hierarchie (children) – unabhängig vom Telefonbuch-Format.
func TestNestedSync(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	file := filepath.Join(dir, "org.json")
	_ = os.WriteFile(file, []byte(`{"units":[{"id":"III","name":"Dezernat III","children":[
		{"id":"32","name":"Bürgerservice","code":"32","head":{"mail":"m.schulz@x"}},
		{"id":"40","name":"Schule","code":"40"}]}],
		"people":[{"login":"n.vogel","display_name":"Nina Vogel","phone":"-4000"},{"display_name":"Pforte"}]}`), 0o644)
	_ = st.PutDoc(ctx, "catalog", "abteilungen", flow.Catalog{Key: "abteilungen", Name: "OE", Attrs: []flow.AttrDef{{Key: "ou"}, {Key: "leitung"}},
		Entries: []flow.Entry{{Key: "32", Label: "alt", Attrs: map[string]string{"ou": "OU=32", "leitung": "alt@x"}}, {Key: "99", Label: "manuell"}}}, "t", 0)
	src := Source{Key: "tb", Name: "TB", Kind: "file", URL: file,
		Targets: []Target{
			{Kind: "catalog", Catalog: "abteilungen", Path: "units", Children: "children", Key: "{{.id}}", Label: "{{.name}} ({{.code}})",
				Group: "{{.uebergeordnet.name}}", Filter: "{{if .code}}ja{{end}}", Attrs: map[string]string{"leitung": "{{.head.mail}}"}},
			{Kind: "persons", Path: "people", Filter: "{{if .login}}ja{{end}}", Attrs: map[string]string{"sam": "{{.login}}", "name": "{{.display_name}}", "phone": "{{.phone}}"}},
		}}
	res, stt := Sync(ctx, st, src, "test")
	if !stt.OK || res[0].Count != 2 || res[0].Added != 1 || res[0].Updated != 1 || res[1].Count != 1 {
		t.Fatalf("%+v %+v", stt, res)
	}
	ab := catalog(t, st, "abteilungen")
	for _, e := range ab.Entries {
		switch e.Key {
		case "32":
			if e.Label != "Bürgerservice (32)" || e.Attrs["ou"] != "OU=32" || e.Attrs["leitung"] != "m.schulz@x" || e.Group != "Dezernat III" {
				t.Fatalf("32: %+v", e)
			}
		case "40":
			if e.Attrs["leitung"] != "" {
				t.Fatalf("40 ohne Leitung: %+v", e)
			}
		case "99":
			if e.Src != "" || e.Inactive {
				t.Fatalf("manueller Eintrag verändert: %+v", e)
			}
		}
	}
	if p := st.SearchPersons(ctx, "vogel", 5); len(p) != 1 || p[0]["phone"] != "-4000" {
		t.Fatalf("Suche: %v", p)
	}
	src.Targets[0].Filter = `{{if and .code (ne .id "40")}}ja{{end}}`
	if res, _ = Sync(ctx, st, src, "test"); res[0].Retired != 1 {
		t.Fatalf("Retired: %+v", res[0])
	}
	// ohne Label-Vorlage bleibt die hier gepflegte Bezeichnung
	src.Targets[0].Label = ""
	Sync(ctx, st, src, "test")
	for _, e := range catalog(t, st, "abteilungen").Entries {
		if e.Key == "32" && e.Label != "Bürgerservice (32)" {
			t.Fatalf("Bezeichnung überschrieben: %+v", e)
		}
	}
}

func catalog(t *testing.T, st *store.Store, key string) flow.Catalog {
	t.Helper()
	cats, _ := st.Catalogs(context.Background())
	for _, c := range cats {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("Katalog %s fehlt", key)
	return flow.Catalog{}
}
