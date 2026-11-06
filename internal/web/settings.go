package web

import (
	"antragswerk/internal/glpi"
	"net/http"
	"strconv"
	"strings"

	"antragswerk/internal/store"
	"antragswerk/internal/ui"
)

type settingsPayload struct {
	Settings ui.Settings          `json:"settings"`
	Texts    []ui.TextDef         `json:"texts"`
	Presets  []ui.Palette         `json:"presets"`
	Vars     map[string][2]string `json:"vars"`
	Views    any                  `json:"views"`
	Roles    []map[string]string  `json:"roles"`
	GLPI     []glpi.Conn          `json:"glpi"`
	Lookups  []glpi.Lookup        `json:"lookups"`
	BaseURL  string               `json:"base_url"`
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	views := []map[string]string{}
	for _, v := range ui.Views {
		views = append(views, map[string]string{"key": v.Key, "label": v.Label, "icon": v.Icon, "hint": v.Hint})
	}
	p := settingsPayload{Settings: ui.Get(), Texts: ui.TextDefs, Presets: ui.Presets, Vars: ui.PresetVars(), Views: views,
		Roles: []map[string]string{{"key": "nutzer", "label": "Antragstellende"}, {"key": "bearbeiter", "label": "Bearbeitung (IT & Stellen)"}, {"key": "admin", "label": "Administration"}},
		GLPI:  s.glpiConnsMasked(r.Context()), Lookups: glpi.Lookups, BaseURL: strings.TrimRight(s.app.Cfg.BaseURL, "/")}
	s.render(w, r, "einstellungen", page{Title: "Einstellungen", Nav: "settings", Wide: true, Data: p})
}

func (s *Server) apiSettingsSave(w http.ResponseWriter, r *http.Request) {
	se := sess(r)
	var st ui.Settings
	if err := decodeJSON(r, &st); err != nil {
		s.jsonErr(w, 400, "Ungültige Daten: "+err.Error())
		return
	}
	st.AppName = strings.TrimSpace(st.AppName)
	if len(st.AppName) > 60 {
		st.AppName = st.AppName[:60]
	}
	switch st.Theme.Mode {
	case "auto", "light", "dark":
	default:
		st.Theme.Mode = "auto"
	}
	if st.Theme.Preset == "eigen" && !colorRe.MatchString(st.Theme.Accent) {
		s.jsonErr(w, 400, "Eigene Farbe bitte als #rrggbb angeben.")
		return
	}
	valid := map[string]bool{}
	for _, v := range ui.Views {
		valid[v.Key] = true
	}
	var allowed []string
	for _, v := range st.Home.Allowed {
		if valid[v] {
			allowed = append(allowed, v)
		}
	}
	if len(allowed) == 0 {
		allowed = []string{"standard"}
	}
	st.Home.Allowed = allowed
	for role, v := range st.Home.Default {
		ok := false
		for _, a := range allowed {
			ok = ok || a == v
		}
		if !ok {
			st.Home.Default[role] = allowed[0]
		}
	}
	known := map[string]ui.TextDef{}
	for _, d := range ui.TextDefs {
		known[d.Key] = d
	}
	for k, v := range st.Texts {
		d, ok := known[k]
		if !ok || v == d.Default {
			delete(st.Texts, k)
		}
	}
	if err := s.st.PutDoc(r.Context(), "setting", "ui", st, se.Username, 0); err != nil {
		s.jsonErr(w, 500, err.Error())
		return
	}
	ui.Set(st)
	s.st.Audit(r.Context(), store.AuditEntry{Actor: se.Username, IP: s.ip(r), Action: "einstellungen", Detail: "Farbschema " + st.Theme.Preset + ", " + itoa(len(st.Texts)) + " angepasste Texte"})
	s.jsonOut(w, 200, map[string]any{"ok": true})
}

func itoa(n int) string { return strconv.Itoa(n) }
