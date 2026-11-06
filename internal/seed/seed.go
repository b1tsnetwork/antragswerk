// Package seed liefert mitgelieferte Abläufe, Kataloge, Stellen, Variablen und Skriptbausteine.
package seed

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"antragswerk/internal/flow"
	"antragswerk/internal/glpi"
	"antragswerk/internal/orgsync"
	"antragswerk/internal/store"
)

//go:embed data/*.yaml data/workflows/*.yaml
var files embed.FS

// Rev erhöhen, wenn sich mitgelieferte Abläufe ändern ("Neuerungen" werden angeboten, nie überschrieben).
const Rev = 1

type base struct {
	Catalogs []flow.Catalog `yaml:"catalogs"`
	Stellen  []flow.Stelle  `yaml:"stellen"`
	Vars     []store.Var    `yaml:"vars"`
	Snippets []flow.Snippet `yaml:"snippets"`
}

func load() (*base, []flow.Def, error) {
	b := &base{}
	for _, n := range []string{"data/catalogs.yaml", "data/stellen.yaml", "data/vars.yaml", "data/snippets.yaml"} {
		raw, err := files.ReadFile(n)
		if err != nil {
			return nil, nil, err
		}
		if err := yaml.Unmarshal(raw, b); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", n, err)
		}
	}
	var defs []flow.Def
	names, _ := fs.Glob(files, "data/workflows/*.yaml")
	sort.Strings(names)
	for _, n := range names {
		raw, _ := files.ReadFile(n)
		var d flow.Def
		if err := yaml.Unmarshal(raw, &d); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", n, err)
		}
		defs = append(defs, d)
	}
	return b, defs, nil
}

// Library liefert die mitgelieferten Abläufe (Vorlagen).
func Library() []flow.Def {
	_, defs, err := load()
	if err != nil {
		slog.Error("seed", "err", err)
	}
	return defs
}

// Apply legt fehlende Stammdaten und Abläufe an. Bestehendes wird nie überschrieben.
func Apply(ctx context.Context, st *store.Store, demo bool, baseURL ...string) error {
	b, defs, err := load()
	if err != nil {
		return err
	}
	if !st.HasDocs(ctx, "catalog") {
		for i, c := range b.Catalogs {
			if err := st.PutDoc(ctx, "catalog", c.Key, c, "system", i); err != nil {
				return err
			}
		}
	}
	// Stellen: fehlende mitgelieferte Stellen ergänzen (einmalig angeboten, gelöschte kommen nicht zurück)
	sts, _ := st.Stellen(ctx)
	offeredSt := map[string]bool{}
	for _, x := range sts {
		offeredSt[x.Key] = true
	}
	for _, k := range st.SeedKeys(ctx, "stellen") {
		offeredSt[k] = true
	}
	var stKeys []string
	for i, s := range b.Stellen {
		stKeys = append(stKeys, s.Key)
		if offeredSt[s.Key] {
			continue
		}
		if !demo { // Demo-Mitglieder nicht in echte Umgebungen übernehmen
			s.Members = nil
			s.Email = strings.ReplaceAll(s.Email, "demo-stadt.de", "example.org")
		}
		if err := st.PutDoc(ctx, "stelle", s.Key, s, "system", i); err != nil {
			return err
		}
	}
	st.AddSeedKeys(ctx, "stellen", stKeys)
	// Variablen und Bausteine: fehlende Schlüssel ergänzen, vorhandene nie überschreiben
	// (bereits angebotene Schlüssel werden gemerkt – gelöschte Variablen kommen nicht zurück)
	vars, _ := st.Vars(ctx)
	offered := map[string]bool{}
	for _, v := range vars {
		offered[v.Key] = true
	}
	for _, k := range st.SeedKeys(ctx, "vars") {
		offered[k] = true
	}
	var keys []string
	for i, v := range b.Vars {
		keys = append(keys, v.Key)
		if !offered[v.Key] {
			if err := st.PutDoc(ctx, "var", v.Key, v, "system", 100+i); err != nil {
				return err
			}
		}
	}
	st.AddSeedKeys(ctx, "vars", keys)
	if !st.HasDocs(ctx, "snippet") {
		for i, s := range b.Snippets {
			if err := st.PutDoc(ctx, "snippet", s.Key, s, "system", i); err != nil {
				return err
			}
		}
	}
	// Demo: Beispiel-Datenquelle „Telefonbuch“ (einmalig; die frühere Beispielquelle im alten Format wird ersetzt,
	// solange sie noch auf die Beispieldatei zeigt)
	if demo && len(baseURL) > 0 && !hasKey(st.SeedKeys(ctx, "sources"), "telefonbuch-org/1") {
		var old orgsync.Source
		had := st.GetDoc(ctx, "source", "telefonbuch", &old)
		if !had || strings.HasSuffix(old.URL, "/static/telefonbuch-beispiel.json") {
			src := TelefonbuchPreset()
			src.URL = strings.TrimRight(baseURL[0], "/") + "/static/telefonbuch-beispiel.json"
			src.Name = "Telefonbuch (Beispieldaten)"
			src.Header = ""
			_ = st.PutDoc(ctx, "source", src.Key, src, "system", 0)
		}
		st.AddSeedKeys(ctx, "sources", []string{"telefonbuch", "telefonbuch-org/1"})
	}
	// Demo: Verbindung zum eingebauten Test-GLPI (/demo-glpi) – einmalig
	if demo && len(baseURL) > 0 && !hasKey(st.SeedKeys(ctx, "glpi"), DemoGLPIKey) {
		c := glpi.Conn{Key: DemoGLPIKey, Name: "Test-GLPI (Demo)", URL: strings.TrimRight(baseURL[0], "/") + "/demo-glpi", ClientID: "antragswerk", ClientSecret: "demo",
			Username: "glpi", Password: "demo", Language: "de_DE", Entity: "Stadtverwaltung", WebhookSecret: DemoGLPIWebhookSecret, PollMinutes: 1}
		_ = st.PutDoc(ctx, "glpi", c.Key, c, "system", 0)
		st.AddSeedKeys(ctx, "glpi", []string{DemoGLPIKey})
	}
	for _, d := range defs {
		if _, err := st.Workflow(ctx, d.Key); err == nil {
			continue
		}
		if _, err := st.CreateWorkflow(ctx, d, "system", true, Rev); err != nil {
			return fmt.Errorf("workflow %s: %w", d.Key, err)
		}
		slog.Info("Ablauf installiert", "key", d.Key)
	}
	return nil
}

// Demo-Verbindung zum eingebauten Test-GLPI.
const (
	DemoGLPIKey           = "glpi"
	DemoGLPIWebhookSecret = "demo-webhook-geheimnis"
)

// TelefonbuchPreset ist die Vorlage für den Organisations-Export des Telefonbuchs (pbucng, GET /api/export/org,
// Format „telefonbuch-org/1“ – siehe README „Organisationsstruktur aus dem Telefonbuch“).
func TelefonbuchPreset() orgsync.Source {
	return orgsync.Source{Key: "telefonbuch", Name: "Telefonbuch", Enabled: true, Kind: "url", URL: "https://telefonbuch.example/api/export/org", Interval: 60,
		Header: "Authorization: Bearer <Token aus orgExport.token>",
		Targets: []orgsync.Target{
			// Ämter/Abteilungen mit Kürzel (Dezernate bilden die Gruppe); OU, Gruppen, Laufwerk, Kostenstelle bleiben hier gepflegt
			{Kind: "catalog", Catalog: "abteilungen", Path: "units", Key: "{{.key}}", Label: "{{.name}}{{if .short}} ({{.short}}){{end}}",
				Group: "{{.top}}", Missing: "inactive", Filter: "{{if and .short .parent}}ja{{end}}",
				Attrs: map[string]string{"leitung": "{{with .head}}{{or .email .username}}{{end}}", "vertretung": "{{with .deputies}}{{with index . 0}}{{or .email .username}}{{end}}{{end}}",
					"verteiler": "{{.email}}", "ebene": "{{.level}}", "uebergeordnet": "{{.parentKey}}"}},
			{Kind: "persons", Path: "people", Filter: "{{if .username}}ja{{end}}",
				Attrs: map[string]string{"sam": "{{.username}}", "name": "{{.name}}", "given": "{{.firstName}}", "surname": "{{.lastName}}", "mail": "{{.email}}",
					"phone": "{{.phone}}", "department": "{{.unitName}}{{if .unitKey}} ({{.unitKey}}){{end}}", "title": "{{.function}}", "office": "{{.room}}",
					"manager": "{{.managerName}}"}},
			// Räume: Schlüssel = Raumangabe im Telefonbuch; Bezeichnung, Gebäude, Schließgruppe usw. bleiben hier gepflegt
			{Kind: "catalog", Catalog: "raeume", Path: "rooms", Key: "{{.name}}", Missing: "keep",
				Attrs: map[string]string{"abteilung": "{{.unitKey}}", "personen": "{{.people}}"}},
		}}
}

func hasKey(list []string, k string) bool {
	for _, x := range list {
		if x == k {
			return true
		}
	}
	return false
}
