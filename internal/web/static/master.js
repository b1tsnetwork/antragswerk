// Antragswerk – Stammdaten (Kataloge, Stellen, Variablen, Skriptbausteine)
"use strict";
(function () {
  const el = document.getElementById("master-data");
  const root = document.getElementById("master");
  if (!el || !root) return;
  const AW = window.AW;
  const h = AW.h || function (tag, attrs, ...kids) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === "class") e.className = v; else if (k === "value") e.value = v; else if (k === "checked") e.checked = !!v;
      else if (k.startsWith("on")) e.addEventListener(k.slice(2), v); else e.setAttribute(k, v === true ? "" : v);
    }
    for (const c of kids.flat(4)) { if (c == null || c === false || c === true) continue; e.append(c.nodeType ? c : document.createTextNode(String(c))); }
    return e;
  };
  const M = JSON.parse(el.textContent);
  M.catalogs = M.catalogs || []; M.stellen = M.stellen || []; M.vars = M.vars || []; M.snippets = M.snippets || []; M.sources = M.sources || [];
  const usage = M.usage || {};
  const S = { view: (location.hash || "#catalog").slice(1), cat: 0, snip: 0, src: 0, dirty: {} };
  if (!["catalog", "stelle", "var", "snippet", "source"].includes(S.view)) S.view = "catalog";
  const slug = (s) => String(s || "").toLowerCase().replace(/ä/g, "ae").replace(/ö/g, "oe").replace(/ü/g, "ue").replace(/ß/g, "ss").replace(/[^a-z0-9.]+/g, "_").replace(/^_+|_+$/g, "");

  const dirty = (k) => { S.dirty[k] = true; const d = root.querySelector(".dirty"); if (d) d.textContent = "● ungespeicherte Änderungen"; };
  window.addEventListener("beforeunload", (e) => { if (Object.values(S.dirty).some(Boolean)) { e.preventDefault(); e.returnValue = ""; } });

  async function save(kind) {
    const body = { catalog: M.catalogs, stelle: M.stellen, var: M.vars, snippet: M.snippets, source: M.sources }[kind];
    try {
      await AW.api("PUT", "/api/stammdaten/" + kind, body);
      S.dirty[kind] = false;
      AW.toast("Gespeichert", "ok");
      render();
    } catch (e) { AW.toast(e.message, "err"); }
  }

  function render() {
    root.innerHTML = "";
    const nav = h("div", { class: "mnav" }, h("div", { class: "card" },
      [["catalog", "▤ Kataloge", M.catalogs.length], ["stelle", "👥 Stellen", M.stellen.length], ["var", "⚙ Variablen", M.vars.length], ["snippet", "⌨ Skriptbausteine", M.snippets.length], ["source", "⇄ Datenquellen", M.sources.length]].map(([k, l, n]) =>
        h("button", { class: S.view === k ? "on" : "", onclick: () => { S.view = k; history.replaceState(null, "", "#" + k); render(); } }, l, h("small", null, n))),
      S.view === "catalog" ? [h("h4", null, "Kataloge"), M.catalogs.map((c, i) => h("button", { class: S.cat === i ? "on" : "", onclick: () => { S.cat = i; render(); } }, (c.icon || "▤") + " " + c.name, h("small", null, c.entries.length))),
        h("button", { onclick: () => { M.catalogs.push({ key: "katalog" + (M.catalogs.length + 1), name: "Neuer Katalog", icon: "▤", attrs: [], entries: [] }); S.cat = M.catalogs.length - 1; dirty("catalog"); render(); } }, "+ Neuer Katalog")] : null,
      S.view === "snippet" ? [h("h4", null, "Bausteine"), M.snippets.map((s, i) => h("button", { class: S.snip === i ? "on" : "", onclick: () => { S.snip = i; render(); } }, s.title, h("small", null, s.group))),
        h("button", { onclick: () => { M.snippets.push({ key: "baustein" + (M.snippets.length + 1), title: "Neuer Baustein", group: "Eigene", lang: "powershell", code: "" }); S.snip = M.snippets.length - 1; dirty("snippet"); render(); } }, "+ Neuer Baustein")] : null,
      S.view === "source" ? [h("h4", null, "Quellen"), M.sources.map((s, i) => h("button", { class: S.src === i ? "on" : "", onclick: () => { S.src = i; render(); } }, (s.enabled ? "● " : "○ ") + s.name, h("small", null, s.interval ? s.interval + " min" : "manuell"))),
        h("button", { onclick: () => { const p = JSON.parse(JSON.stringify(M.preset)); p.key = "quelle" + (M.sources.length + 1); p.name = "Neue Quelle"; p.enabled = false; M.sources.push(p); S.src = M.sources.length - 1; dirty("source"); render(); } }, "+ Neue Quelle (Vorlage Telefonbuch)")] : null));
    const main = h("div");
    root.append(nav, main);
    ({ catalog: renderCatalog, stelle: renderStellen, var: renderVars, snippet: renderSnippet, source: renderSource })[S.view](main);
  }
  const savebar = (kind) => h("div", { class: "savebar" }, h("span", { class: "dirty" }, S.dirty[kind] ? "● ungespeicherte Änderungen" : ""), h("button", { class: "btn primary", onclick: () => save(kind) }, "Speichern"));
  const inp = (obj, k, kind, attrs) => h("input", Object.assign({ value: obj[k] == null ? "" : obj[k], oninput: (e) => { obj[k] = e.target.value; dirty(kind); } }, attrs || {}));

  // ---------- Kataloge ----------
  function renderCatalog(main) {
    const c = M.catalogs[S.cat];
    if (!c) { main.append(h("div", { class: "card empty" }, "Kein Katalog vorhanden.")); return; }
    c.attrs = c.attrs || []; c.entries = c.entries || [];
    const used = usage["catalog:" + c.key] || [];
    main.append(h("div", { class: "card" },
      h("div", { class: "cardhead" }, h("h2", null, (c.icon || "▤") + " " + c.name), h("span", { class: "small muted" }, used.length ? "verwendet in: " + used.join(", ") : "noch in keinem Ablauf verwendet")),
      h("div", { class: "row2", style: "display:grid;grid-template-columns:2fr 1fr 70px;gap:12px" },
        h("label", null, "Name", inp(c, "name", "catalog")), h("label", null, "Schlüssel", inp(c, "key", "catalog", { class: "mono" })), h("label", null, "Symbol", inp(c, "icon", "catalog"))),
      h("label", null, "Beschreibung", inp(c, "description", "catalog")),
      h("h3", null, "Attribute ", h("small", { class: "muted" }, "– zusätzliche Spalten, in Skripten als {{.feld.attribut}}")),
      h("div", { class: "chips", style: "gap:6px" }, c.attrs.map((a, i) => h("span", { class: "condbox", style: "display:inline-flex;gap:4px;align-items:center;padding:4px 6px" },
        h("input", { value: a.label, style: "width:150px;margin:0;padding:4px 6px", oninput: (e) => { const auto = a.key === slug(a.label); a.label = e.target.value; if (auto) a.key = slug(e.target.value); dirty("catalog"); } }),
        h("input", { value: a.key, class: "mono", style: "width:120px;margin:0;padding:4px 6px", oninput: (e) => { const old = a.key; a.key = e.target.value; c.entries.forEach((en) => { if (en.attrs && old in en.attrs) { en.attrs[a.key] = en.attrs[old]; delete en.attrs[old]; } }); dirty("catalog"); } }),
        h("button", { class: "x", title: "Attribut löschen", onclick: () => { if (confirm("Attribut „" + a.label + "“ inkl. Werten löschen?")) { c.attrs.splice(i, 1); dirty("catalog"); render(); } } }, "×"))),
        h("button", { class: "btn tiny", onclick: () => { c.attrs.push({ key: "attr" + (c.attrs.length + 1), label: "Neues Attribut" }); dirty("catalog"); render(); } }, "+ Attribut"))));
    const q = h("input", { type: "search", placeholder: "Einträge filtern …", style: "max-width:260px;margin:0" });
    const tbody = h("tbody");
    const drawRows = () => {
      tbody.innerHTML = "";
      const s = q.value.trim().toLowerCase();
      c.entries.forEach((en, i) => {
        en.attrs = en.attrs || {};
        if (s && !JSON.stringify(en).toLowerCase().includes(s)) return;
        tbody.append(h("tr", { class: en.inactive ? "inactive" : "" },
          h("td", { class: "mono" }, inp(en, "key", "catalog", { style: "width:120px" }), en.src ? h("span", { class: "srcbadge", title: "aus Datenquelle „" + en.src + "“ – wird beim nächsten Abgleich überschrieben (nicht zugeordnete Attribute bleiben)" }, "⇄ " + en.src) : null),
          h("td", null, inp(en, "label", "catalog", { style: "min-width:180px" })),
          h("td", null, inp(en, "group", "catalog", { style: "width:130px" })),
          c.attrs.map((a) => h("td", null, h("input", { value: en.attrs[a.key] || "", title: a.hint || a.label, style: "min-width:150px", oninput: (e) => { en.attrs[a.key] = e.target.value; dirty("catalog"); } }))),
          h("td", { style: "white-space:nowrap" }, h("label", { class: "chk", style: "margin:0;font-weight:400;font-size:12px;display:inline-flex;gap:4px" }, h("input", { type: "checkbox", checked: !!en.inactive, onchange: (e) => { en.inactive = e.target.checked; dirty("catalog"); drawRows(); } }), "inaktiv"),
            h("button", { class: "x", onclick: () => { c.entries.splice(i, 1); dirty("catalog"); drawRows(); } }, "×"))));
      });
    };
    q.addEventListener("input", drawRows);
    drawRows();
    main.append(h("div", { class: "card" },
      h("div", { class: "cardhead" }, h("h2", null, "Einträge (" + c.entries.length + ")"), h("div", { class: "actions" }, q,
        h("button", { class: "btn small", onclick: () => { c.entries.push({ key: "", label: "", attrs: {} }); dirty("catalog"); drawRows(); const r = tbody.lastChild; if (r) r.querySelector("input").focus(); } }, "+ Eintrag"),
        h("button", { class: "btn small ghost", onclick: () => importCsv(c) }, "CSV einfügen …"),
        h("button", { class: "btn small ghost", onclick: () => exportCsv(c) }, "CSV kopieren"))),
      h("p", { class: "small muted" }, "Inaktive Einträge sind in neuen Anträgen nicht wählbar, bleiben aber für bestehende Vorgänge erhalten. Zuständigkeits-Attribute enthalten Stellen-Schlüssel (z. B. ", h("code", null, "fv_betreuung"), "), Mail-Adressen oder Anmeldenamen."),
      h("div", { class: "ewrap" }, h("table", { class: "etable" }, h("thead", null, h("tr", null, h("th", null, "Schlüssel"), h("th", null, "Bezeichnung"), h("th", null, "Gruppe"), c.attrs.map((a) => h("th", { title: a.hint || "" }, a.label)), h("th"))), tbody)),
      h("div", { class: "actions", style: "margin-top:10px" }, h("button", { class: "btn small ghost", style: "color:var(--bad)", onclick: () => {
        if (used.length && !confirm("Der Katalog wird in " + used.join(", ") + " verwendet. Trotzdem löschen?")) return;
        if (!used.length && !confirm("Katalog löschen?")) return;
        M.catalogs.splice(S.cat, 1); S.cat = 0; dirty("catalog"); render();
      } }, "Katalog löschen"))), savebar("catalog"));
  }
  function exportCsv(c) {
    const cols = ["key", "label", "group"].concat(c.attrs.map((a) => a.key));
    const q = (v) => '"' + String(v || "").replace(/"/g, '""') + '"';
    const text = [cols.join(";")].concat(c.entries.map((e) => [e.key, e.label, e.group].concat(c.attrs.map((a) => (e.attrs || {})[a.key])).map(q).join(";"))).join("\n");
    AW.copy(text).then((ok) => AW.toast(ok ? "CSV in die Zwischenablage kopiert (; getrennt)" : "Kopieren nicht möglich", ok ? "ok" : "err"));
  }
  function importCsv(c) {
    const t = prompt("CSV einfügen (erste Zeile = Spalten: key;label;group;" + c.attrs.map((a) => a.key).join(";") + "). Vorhandene Schlüssel werden aktualisiert.");
    if (!t) return;
    const rows = t.trim().split(/\r?\n/).map((l) => l.split(/;|\t/).map((x) => x.replace(/^"|"$/g, "").replace(/""/g, '"').trim()));
    const head = rows.shift();
    let n = 0;
    rows.forEach((r) => {
      const o = {};
      head.forEach((k, i) => { o[k] = r[i] || ""; });
      if (!o.key) return;
      let e = c.entries.find((x) => x.key === o.key);
      if (!e) { e = { key: o.key, attrs: {} }; c.entries.push(e); }
      e.label = o.label || e.label || o.key;
      if (o.group) e.group = o.group;
      head.forEach((k) => { if (!["key", "label", "group"].includes(k)) { e.attrs = e.attrs || {}; e.attrs[k] = o[k]; if (!c.attrs.find((a) => a.key === k)) c.attrs.push({ key: k, label: k }); } });
      n++;
    });
    dirty("catalog"); render();
    AW.toast(n + " Einträge übernommen – bitte speichern.");
  }

  // ---------- Stellen ----------
  function renderStellen(main) {
    const tb = h("tbody");
    M.stellen.forEach((s, i) => {
      const used = usage["stelle:" + s.key] || [];
      tb.append(h("tr", null,
        h("td", null, h("input", { type: "color", value: s.color || "#64748b", style: "width:36px;padding:2px", oninput: (e) => { s.color = e.target.value; dirty("stelle"); } })),
        h("td", { class: "mono" }, inp(s, "key", "stelle", { style: "width:130px" })),
        h("td", null, inp(s, "name", "stelle", { style: "min-width:180px" }), h("input", { value: s.description || "", placeholder: "Aufgabe", style: "font-size:12px;color:var(--muted)", oninput: (e) => { s.description = e.target.value; dirty("stelle"); } })),
        h("td", null, inp(s, "email", "stelle", { placeholder: "funktionspostfach@…", style: "min-width:200px" })),
        h("td", null, h("textarea", { rows: 2, style: "min-width:200px", placeholder: "Anmeldenamen oder Mails, eine pro Zeile", oninput: (e) => { s.members = e.target.value.split(/[\n,;]+/).map((x) => x.trim()).filter(Boolean); dirty("stelle"); } }, (s.members || []).join("\n"))),
        h("td", null, inp(s, "group", "stelle", { placeholder: "AD-Gruppe (CN oder DN)", style: "min-width:180px" })),
        h("td", { class: "small muted" }, used.length ? used.length + " Abläufe" : "–"),
        h("td", null, h("button", { class: "x", onclick: () => { if (!used.length || confirm("Stelle wird in " + used.join(", ") + " verwendet. Löschen?")) { M.stellen.splice(i, 1); dirty("stelle"); render(); } } }, "×"))));
    });
    main.append(h("div", { class: "card" }, h("div", { class: "cardhead" }, h("h2", null, "Stellen"), h("button", { class: "btn small", onclick: () => { M.stellen.push({ key: "stelle" + (M.stellen.length + 1), name: "Neue Stelle", members: [] }); dirty("stelle"); render(); } }, "+ Stelle")),
      h("p", { class: "small muted" }, "Stellen bearbeiten Schritte. Benachrichtigt wird das Funktionspostfach – ohne Postfach alle Mitglieder mit Mail-Adresse. Bearbeiten dürfen alle Mitglieder (Anmeldename/Mail) sowie Mitglieder der AD-Gruppe (auch verschachtelt; wird bei der Anmeldung geprüft)."),
      h("div", { class: "ewrap" }, h("table", { class: "etable" }, h("thead", null, h("tr", null, ["", "Schlüssel", "Name", "Funktionspostfach", "Mitglieder", "AD-Gruppe", "Nutzung", ""].map((x) => h("th", null, x)))), tb))), savebar("stelle"));
  }

  // ---------- Variablen ----------
  function renderVars(main) {
    const tb = h("tbody");
    M.vars.forEach((v, i) => tb.append(h("tr", null,
      h("td", { class: "mono" }, inp(v, "key", "var", { style: "width:170px" })),
      h("td", null, inp(v, "label", "var", { style: "min-width:180px" })),
      h("td", { class: "mono" }, inp(v, "value", "var", { style: "min-width:300px" })),
      h("td", null, inp(v, "hint", "var", { style: "min-width:200px" })),
      h("td", null, h("code", { class: "small" }, "{{.var." + v.key + "}}")),
      h("td", null, h("button", { class: "x", onclick: () => { M.vars.splice(i, 1); dirty("var"); render(); } }, "×")))));
    main.append(h("div", { class: "card" }, h("div", { class: "cardhead" }, h("h2", null, "Globale Variablen"), h("button", { class: "btn small", onclick: () => { M.vars.push({ key: "variable" + (M.vars.length + 1), label: "Neue Variable", value: "" }); dirty("var"); render(); } }, "+ Variable")),
      h("p", { class: "small muted" }, "Umgebungswerte wie Domäne, OUs oder Freigaben – einmal pflegen, in allen Skripten nutzen. ", h("code", null, "benutzername_schema"), " steuert die Funktion ", h("code", null, "{{sam .vorname .nachname}}"), "."),
      h("div", { class: "ewrap" }, h("table", { class: "etable" }, h("thead", null, h("tr", null, ["Schlüssel", "Bezeichnung", "Wert", "Hinweis", "Verwendung", ""].map((x) => h("th", null, x)))), tb))), savebar("var"));
  }

  // ---------- Bausteine ----------
  function renderSnippet(main) {
    const s = M.snippets[S.snip];
    if (!s) { main.append(h("div", { class: "card empty" }, "Keine Bausteine.")); return; }
    const pre = h("pre", { "aria-hidden": "true" });
    const ta = h("textarea", { spellcheck: "false", wrap: "off" });
    ta.value = s.code || "";
    const sync = () => { pre.innerHTML = AW.highlight(ta.value + "\n", "powershell"); pre.scrollTop = ta.scrollTop; pre.scrollLeft = ta.scrollLeft; };
    ta.addEventListener("input", () => { s.code = ta.value; dirty("snippet"); sync(); });
    ta.addEventListener("scroll", sync);
    sync();
    const ed = h("div", { class: "codeed" }, pre, ta);
    pre.style.minHeight = "360px";
    main.append(h("div", { class: "card" },
      h("div", { style: "display:grid;grid-template-columns:2fr 1fr 1fr;gap:12px" }, h("label", null, "Titel", inp(s, "title", "snippet")), h("label", null, "Gruppe", inp(s, "group", "snippet")), h("label", null, "Schlüssel", inp(s, "key", "snippet", { class: "mono" }))),
      h("label", null, "Beschreibung", inp(s, "description", "snippet")),
      h("label", null, "Code"), ed,
      h("p", { class: "small muted" }, "Bausteine werden im Designer in Schritte kopiert (keine Verknüpfung) – Änderungen hier wirken nur auf künftige Übernahmen."),
      h("button", { class: "btn small ghost", style: "color:var(--bad)", onclick: () => { if (confirm("Baustein löschen?")) { M.snippets.splice(S.snip, 1); S.snip = 0; dirty("snippet"); render(); } } }, "Baustein löschen")), savebar("snippet"));
  }

  // ---------- Datenquellen ----------
  function renderSource(main) {
    const src = M.sources[S.src];
    main.append(h("div", { class: "card" }, h("h2", null, "Organisationsstruktur aus anderen Systemen"),
      h("p", { class: "small muted" }, "Datenquellen übernehmen Organisationseinheiten, Räume und Personen z. B. aus dem internen Telefonbuch – die Pflege bleibt dort. ",
        "Zugeordnete Felder werden bei jedem Abgleich überschrieben; Attribute, die nur hier gepflegt werden (z. B. OU, Standardgruppen, Laufwerk), bleiben erhalten. ",
        "Entfällt eine Einheit in der Quelle, wird sie hier inaktiv – laufende Vorgänge bleiben gültig. Personen ergänzen die Personensuche im Antrag (Telefon, Raum, Funktion).")));
    if (!src) { main.append(h("div", { class: "card empty" }, "Noch keine Datenquelle. Links „+ Neue Quelle“ wählen.")); return; }
    src.targets = src.targets || [];
    const stt = (M.states || {})[src.key];
    const result = h("div");
    const D = () => dirty("source");
    const inp = (obj, k, attrs) => h("input", Object.assign({ value: obj[k] == null ? "" : obj[k], oninput: (e) => { obj[k] = e.target.value; D(); } }, attrs || {}));
    main.append(h("div", { class: "card" },
      h("div", { class: "cardhead" }, h("h2", null, src.name || "Datenquelle"),
        h("div", { class: "actions" },
          h("button", { class: "btn small", onclick: () => test(src, result) }, "▶ Testen (ohne Übernahme)"),
          h("button", { class: "btn small primary", onclick: () => sync(src, result) }, "⇄ Jetzt abgleichen"))),
      stt ? h("div", { class: "hint " + (stt.ok ? "" : "err"), style: stt.ok ? "background:var(--ok-soft);color:var(--ok)" : "" },
        "Letzter Abgleich " + new Date(stt.at).toLocaleString("de-DE") + ": " + (stt.ok ? "erfolgreich" : "Fehler – " + stt.message), stt.counts ? h("div", { class: "small" }, stt.counts.join(" · ")) : null) : h("div", { class: "small muted" }, "Noch nicht abgeglichen."),
      (M.persons || {})[src.key] ? h("div", { class: "small muted" }, (M.persons[src.key]) + " Personen im Verzeichnis") : null,
      h("div", { style: "display:grid;grid-template-columns:2fr 1fr 1fr;gap:12px;margin-top:12px" },
        h("label", null, "Name", inp(src, "name")), h("label", null, "Schlüssel", inp(src, "key", { class: "mono" })),
        h("label", null, "Abgleich alle … Minuten", h("input", { type: "number", min: 0, value: src.interval || 0, oninput: (e) => { src.interval = parseInt(e.target.value, 10) || 0; D(); } }), h("small", { class: "muted" }, "0 = nur manuell"))),
      h("div", { style: "display:grid;grid-template-columns:140px 1fr;gap:12px" },
        h("label", null, "Art", (() => { const s = h("select", { onchange: (e) => { src.kind = e.target.value; D(); } }, h("option", { value: "url" }, "URL (HTTP/S)"), h("option", { value: "file" }, "Datei")); s.value = src.kind || "url"; return s; })()),
        h("label", null, src.kind === "file" ? "Dateipfad (auf dem Server)" : "URL", inp(src, "url", { class: "mono", placeholder: "https://telefonbuch.stadt.local/api/export/org" }))),
      h("label", null, "Zusätzliche Kopfzeilen ", h("small", { class: "muted" }, "eine je Zeile, z. B. Authorization: Bearer … – wird gespeichert, nur Administration sieht sie"),
        h("textarea", { rows: 2, class: "mono", oninput: (e) => { src.header = e.target.value; D(); } }, src.header || "")),
      h("div", { style: "display:flex;gap:18px" },
        h("label", { class: "chk", style: "display:inline-flex;gap:6px;font-weight:500" }, h("input", { type: "checkbox", checked: !!src.enabled, onchange: (e) => { src.enabled = e.target.checked; D(); } }), "aktiv (zeitgesteuerter Abgleich)"),
        h("label", { class: "chk", style: "display:inline-flex;gap:6px;font-weight:500" }, h("input", { type: "checkbox", checked: !!src.insecure, onchange: (e) => { src.insecure = e.target.checked; D(); } }), "Zertifikat nicht prüfen (nur Test)"))));
    src.targets.forEach((t, ti) => main.append(targetCard(src, t, ti, D)));
    main.append(h("div", { class: "actions", style: "margin-bottom:16px" },
      h("button", { class: "btn small", onclick: () => { src.targets.push({ kind: "catalog", catalog: (M.catalogs[0] || {}).key, path: "", key: "{{.id}}", label: "{{.name}}", attrs: {}, missing: "inactive" }); D(); render(); } }, "+ Ziel: Katalog"),
      h("button", { class: "btn small", onclick: () => { src.targets.push({ kind: "persons", path: "people", attrs: { sam: "{{.login}}", name: "{{.display_name}}", mail: "{{.mail}}" } }); D(); render(); } }, "+ Ziel: Personenverzeichnis"),
      h("span", { style: "flex:1" }),
      h("button", { class: "btn small ghost", style: "color:var(--bad)", onclick: () => { if (confirm("Datenquelle löschen? Übernommene Einträge bleiben erhalten.")) { M.sources.splice(S.src, 1); S.src = 0; D(); render(); } } }, "Quelle löschen")));
    main.append(result, savebar("source"));
  }
  function targetCard(src, t, ti, D) {
    t.attrs = t.attrs || {};
    const isP = t.kind === "persons";
    const cat = M.catalogs.find((c) => c.key === t.catalog);
    const tin = (k, ph, cls) => h("input", { value: t[k] || "", placeholder: ph || "", class: cls || "mono", oninput: (e) => { t[k] = e.target.value; D(); } });
    const rows = [];
    if (!isP) {
      rows.push(["Schlüssel *", tin("key", "{{.id}}")], ["Bezeichnung *", tin("label", "{{.name}}")], ["Gruppe", tin("group", "{{.uebergeordnet.name}}")]);
      (cat ? cat.attrs : []).forEach((a) => rows.push([a.label + " (" + a.key + ")", h("input", { class: "mono", value: t.attrs[a.key] || "", placeholder: "– hier gepflegt –", oninput: (e) => { if (e.target.value) t.attrs[a.key] = e.target.value; else delete t.attrs[a.key]; D(); } })]));
    } else {
      M.person_fields.forEach((f) => rows.push([f.label + (f.key === "sam" ? " *" : ""), h("input", { class: "mono", value: t.attrs[f.key] || "", oninput: (e) => { if (e.target.value) t.attrs[f.key] = e.target.value; else delete t.attrs[f.key]; D(); } })]));
    }
    return h("div", { class: "card" },
      h("div", { class: "cardhead" }, h("h2", null, isP ? "→ Personenverzeichnis" : "→ Katalog „" + (cat ? cat.name : t.catalog) + "“"),
        h("button", { class: "x", onclick: () => { src.targets.splice(ti, 1); D(); render(); } }, "×")),
      h("div", { style: "display:grid;grid-template-columns:repeat(4,1fr);gap:12px" },
        isP ? h("div") : h("label", null, "Zielkatalog", (() => { const s = h("select", { onchange: (e) => { t.catalog = e.target.value; D(); render(); } }, M.catalogs.map((c) => h("option", { value: c.key }, c.name))); s.value = t.catalog; return s; })()),
        h("label", null, "Pfad zur Liste", tin("path", "units"), h("small", { class: "muted" }, "leer = JSON-Wurzel")),
        isP ? h("div") : h("label", null, "Unterelemente in", tin("children", "children"), h("small", { class: "muted" }, "für Hierarchien")),
        isP ? h("div") : h("label", null, "Fehlt in der Quelle", (() => { const s = h("select", { onchange: (e) => { t.missing = e.target.value; D(); } }, h("option", { value: "inactive" }, "inaktiv setzen"), h("option", { value: "keep" }, "behalten"), h("option", { value: "delete" }, "löschen")); s.value = t.missing || "inactive"; return s; })())),
      h("label", null, "Filter ", h("small", { class: "muted" }, "Vorlage; leeres Ergebnis = Datensatz überspringen, z. B. {{if .code}}ja{{end}}"), tin("filter", "")),
      h("table", { class: "etable maptable" }, h("thead", null, h("tr", null, h("th", null, "Feld"), h("th", null, "Vorlage aus der Quelle"))),
        h("tbody", null, rows.map(([l, i]) => h("tr", null, h("td", { style: "white-space:nowrap" }, l), h("td", null, i))))),
      h("p", { class: "small muted" }, "Vorlagen greifen auf Felder des Datensatzes zu: {{.name}}, verschachtelt {{.head.mail}}, übergeordnet {{.uebergeordnet.name}}, Hierarchie {{.pfad}}, Tiefe {{.ebene}}. Funktionen wie {{lower .mail}} sind möglich."));
  }
  function showResults(box, res, title) {
    box.innerHTML = "";
    const card = h("div", { class: "card" }, h("h2", null, title));
    res.forEach((r) => {
      card.append(h("h3", null, r.target + ": " + r.count + " Datensätze" + (r.skipped ? " (" + r.skipped + " übersprungen)" : "") +
        (r.added || r.updated || r.retired ? " · neu " + r.added + ", aktualisiert " + r.updated + ", inaktiv/entfernt " + r.retired : "")));
      (r.errors || []).forEach((e) => card.append(h("div", { class: "hint err" }, e)));
      if (r.rows && r.rows.length) {
        const keys = Array.from(new Set(r.rows.flatMap((x) => Object.keys(x.attrs || {}))));
        card.append(h("div", { class: "ewrap", style: "max-height:320px" }, h("table", { class: "etable" },
          h("thead", null, h("tr", null, h("th", null, "Schlüssel"), h("th", null, "Bezeichnung"), h("th", null, "Gruppe"), keys.map((k) => h("th", null, k)))),
          h("tbody", null, r.rows.map((x) => h("tr", null, h("td", { class: "mono" }, x.key), h("td", null, x.label), h("td", null, x.group), keys.map((k) => h("td", null, (x.attrs || {})[k] || ""))))))));
      }
      if (r.fields && r.fields.length) card.append(h("details", null, h("summary", { class: "small" }, "Verfügbare Felder im ersten Datensatz: " + r.fields.join(", ")), r.sample ? h("pre", { class: "prevbox" }, r.sample) : null));
    });
    box.append(card);
    card.scrollIntoView({ behavior: "smooth", block: "start" });
  }
  async function test(src, box) {
    box.innerHTML = ""; box.append(h("div", { class: "card muted" }, "Rufe Quelle ab …"));
    try { const r = await AW.api("POST", "/api/datenquellen/test", src); showResults(box, r.results || [], "Vorschau (nichts übernommen)"); }
    catch (e) { box.innerHTML = ""; box.append(h("div", { class: "card" }, h("div", { class: "hint err" }, e.message))); }
  }
  async function sync(src, box) {
    if (S.dirty.source) { await save("source"); }
    box.innerHTML = ""; box.append(h("div", { class: "card muted" }, "Gleiche ab …"));
    try {
      const r = await AW.api("POST", "/api/datenquellen/" + encodeURIComponent(src.key) + "/sync", {});
      M.states = M.states || {}; M.states[src.key] = r.status;
      AW.toast(r.status.ok ? "Abgleich erfolgreich" : "Abgleich mit Fehlern", r.status.ok ? "ok" : "err");
      setTimeout(() => location.reload(), 1600);
      showResults(box, r.results || [], "Ergebnis des Abgleichs");
    } catch (e) { box.innerHTML = ""; box.append(h("div", { class: "card" }, h("div", { class: "hint err" }, e.message))); }
  }

  render();
})();
