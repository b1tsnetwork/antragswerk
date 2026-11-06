// Antragswerk – Einstellungen: Erscheinungsbild, Startseite, Texte
"use strict";
(function () {
  const el = document.getElementById("settings-data");
  const root = document.getElementById("settings");
  if (!el || !root) return;
  const AW = window.AW;
  function h(tag, attrs, ...kids) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === "class") e.className = v; else if (k === "value") e.value = v; else if (k === "checked") e.checked = !!v;
      else if (k.startsWith("on")) e.addEventListener(k.slice(2), v); else e.setAttribute(k, v === true ? "" : v);
    }
    for (const c of kids.flat(4)) { if (c == null || c === false || c === true) continue; e.append(c.nodeType ? c : document.createTextNode(String(c))); }
    return e;
  }
  const P = JSON.parse(el.textContent);
  const S = P.settings;
  S.texts = S.texts || {};
  S.home = S.home || {};
  S.home.default = S.home.default || {};
  S.home.allowed = S.home.allowed || [];
  S.theme = S.theme || {};
  const st = { tab: (location.hash || "#look").slice(1), dirty: false, q: "", group: "" };
  if (!["look", "home", "texts", "api"].includes(st.tab)) st.tab = "look";
  const G = P.glpi || [];
  const dirty = () => { st.dirty = true; const d = root.querySelector(".dirty"); if (d) d.textContent = "● ungespeicherte Änderungen"; };
  window.addEventListener("beforeunload", (e) => { if (st.dirty) { e.preventDefault(); e.returnValue = ""; } });

  // ---------- Live-Vorschau des Farbschemas ----------
  const hex = (s) => { s = (s || "").replace("#", ""); if (s.length === 3) s = s.split("").map((c) => c + c).join(""); const n = parseInt(s, 16); return isNaN(n) || s.length !== 6 ? null : [n >> 16 & 255, n >> 8 & 255, n & 255]; };
  const toHex = (c) => "#" + c.map((x) => Math.max(0, Math.min(255, Math.round(x))).toString(16).padStart(2, "0")).join("");
  const mix = (a, b, t) => a.map((x, i) => x * (1 - t) + b[i] * t);
  function customVars(c) {
    const l = hex(c) || [31, 95, 191], d = mix(l, [255, 255, 255], 0.35);
    return ["--accent:" + toHex(l) + ";--accent-2:" + toHex(mix(l, [0, 0, 0], 0.18)) + ";--accent-soft:" + toHex(mix(l, [255, 255, 255], 0.88)) + ";--accent-text:#fff;",
      "--accent:" + toHex(d) + ";--accent-2:" + toHex(mix(d, [255, 255, 255], 0.15)) + ";--accent-soft:" + toHex(mix(d, [21, 27, 35], 0.78)) + ";--accent-text:#0b1220;"];
  }
  function preview() {
    const v = S.theme.preset === "eigen" ? customVars(S.theme.accent) : P.vars[S.theme.preset] || P.vars.blau;
    const rad = { eckig: "--radius:4px;--radius-sm:3px;", rund: "--radius:18px;--radius-sm:12px;" }[S.theme.radius] || "";
    let tag = document.getElementById("aw-theme");
    tag.textContent = "html:root{" + v[0] + rad + "}html:root[data-theme=dark]{" + v[1] + "}@media (prefers-color-scheme: dark){html:root:not([data-theme=light]){" + v[1] + "}}";
    const u = document.getElementById("aw-user-theme");
    if (u) u.remove();
  }

  async function save() {
    try {
      await AW.api("PUT", "/api/einstellungen", S);
      st.dirty = false;
      AW.toast("Gespeichert – gilt ab sofort für alle.", "ok");
      setTimeout(() => location.reload(), 600);
    } catch (e) { AW.toast(e.message, "err"); }
  }

  function render() {
    root.innerHTML = "";
    const nav = h("div", { class: "mnav" }, h("div", { class: "card" },
      [["look", "🎨 Erscheinungsbild"], ["home", "🏠 Startseite"], ["texts", "✎ Texte & Mails"], ["api", "🔌 Schnittstellen"]].map(([k, l]) =>
        h("button", { class: st.tab === k ? "on" : "", onclick: () => { st.tab = k; history.replaceState(null, "", "#" + k); render(); } }, l,
          k === "texts" ? h("small", null, Object.keys(S.texts).length + " geändert") : k === "api" ? h("small", null, G.length ? "GLPI: " + G.length : "") : null))));
    const main = h("div");
    root.append(nav, main);
    ({ look: renderLook, home: renderHome, texts: renderTexts, api: renderAPI })[st.tab](main);
    main.append(h("div", { class: "savebar" }, h("span", { class: "dirty" }, st.dirty ? "● ungespeicherte Änderungen" : ""), h("button", { class: "btn primary", onclick: st.tab === "api" ? saveGLPI : save }, "Speichern")));
  }

  // ---------- Erscheinungsbild ----------
  function renderLook(main) {
    const inp = (obj, k, attrs) => h("input", Object.assign({ value: obj[k] || "", oninput: (e) => { obj[k] = e.target.value; dirty(); } }, attrs || {}));
    main.append(h("div", { class: "card" }, h("h2", null, "Name & Organisation"),
      h("div", { class: "row2", style: "display:grid;grid-template-columns:1fr 1fr;gap:12px" },
        h("label", null, "Name der Anwendung", inp(S, "app_name", { placeholder: "Antragswerk" }), h("small", { class: "muted" }, "Kopfzeile, Seitentitel, Mails")),
        h("label", null, "Organisation", inp(S, "org", { placeholder: "Stadtverwaltung Musterstadt" }), h("small", { class: "muted" }, "Fußzeile und Mail-Kopf")))));
    const grid = h("div", { class: "themegrid" });
    const card = (key, name, color, extra) => {
      const on = S.theme.preset === key;
      return h("button", { type: "button", class: "themecard" + (on ? " on" : ""), onclick: () => { S.theme.preset = key; dirty(); preview(); render(); } },
        h("span", { class: "tc-prev", style: "--c:" + color },
          h("i", { class: "tc-bar" }), h("i", { class: "tc-btn" }), h("i", { class: "tc-pill" }), h("i", { class: "tc-line" }), h("i", { class: "tc-line s" })),
        h("b", null, name), extra);
    };
    P.presets.forEach((p) => grid.append(card(p.key, p.name, p.light)));
    const picker = h("input", { type: "color", value: S.theme.accent || "#1f5fbf", oninput: (e) => { S.theme.accent = e.target.value; S.theme.preset = "eigen"; dirty(); preview(); grid.querySelectorAll(".themecard").forEach((c) => c.classList.toggle("on", c === ownCard)); } });
    const ownCard = card("eigen", "Eigene Farbe", S.theme.accent || "#1f5fbf", picker);
    grid.append(ownCard);
    const seg = (k, opts) => h("div", { class: "seg" }, opts.map(([v, l]) => h("button", { type: "button", class: (S.theme[k] || opts[0][0]) === v ? "on" : "", onclick: () => { S.theme[k] = v; dirty(); preview(); render(); } }, l)));
    main.append(h("div", { class: "card" }, h("h2", null, "Farbschema"),
      h("p", { class: "small muted" }, "Die Akzentfarbe prägt Schaltflächen, Links, Markierungen und die Mails. Die Vorschau wirkt sofort auf dieser Seite."),
      grid,
      h("div", { class: "row3", style: "display:grid;grid-template-columns:repeat(3,1fr);gap:16px;margin-top:16px" },
        h("div", null, h("h4", null, "Hell / Dunkel"), seg("mode", [["auto", "◐ System"], ["light", "☀ Hell"], ["dark", "☾ Dunkel"]])),
        h("div", null, h("h4", null, "Ecken"), seg("radius", [["normal", "Normal"], ["eckig", "Eckig"], ["rund", "Rund"]])),
        h("div", null, h("h4", null, "Dichte"), seg("density", [["normal", "Normal"], ["kompakt", "Kompakt"]]))),
      h("label", { class: "check", style: "margin-top:16px;background:var(--surface-2)" }, h("input", { type: "checkbox", checked: !!S.theme.allow_user, onchange: (e) => { S.theme.allow_user = e.target.checked; dirty(); } }),
        h("span", null, h("b", null, "Benutzer dürfen Hell/Dunkel und Farbe selbst wählen"), h("br"), h("small", { class: "muted" }, "Im Benutzermenü oben rechts; die Wahl gilt pro Browser. Ohne Haken gilt überall die Vorgabe.")))));
  }

  // ---------- Schnittstellen (GLPI) ----------
  async function saveGLPI() {
    try {
      const r = await AW.api("PUT", "/api/glpi", G);
      G.splice(0, G.length, ...r.conns);
      st.dirty = false;
      AW.toast("GLPI-Verbindungen gespeichert.", "ok");
      render();
    } catch (e) { AW.toast(e.message, "err"); }
  }
  const randomSecret = () => { const a = new Uint8Array(24); crypto.getRandomValues(a); return Array.from(a, (b) => b.toString(16).padStart(2, "0")).join(""); };
  function renderAPI(main) {
    main.append(h("div", { class: "card" }, h("h2", null, "GLPI"),
      h("p", { class: "small muted" }, "Abläufe können Tickets und Changes in GLPI anlegen (Schrittart „GLPI-Ticket/Change“ im Designer), auf die Lösung warten und Angaben aus GLPI übernehmen. ",
        "Genutzt wird ausschließlich die moderne API (v2, ab GLPI 11) mit OAuth2 – nicht die alte REST-API (apirest.php)."),
      h("details", { class: "small" }, h("summary", null, "Einrichtung in GLPI"),
        h("ol", null,
          h("li", null, h("b", null, "OAuth-Client: "), "Einrichtung → OAuth-Clients → Hinzufügen. Grant „Password“ (Anmeldung mit einem Dienstkonto, Rechte wie dieses Konto) oder „Client credentials“, Scope ", h("code", null, "api"), ". Client-ID und Secret hier eintragen."),
          h("li", null, h("b", null, "Dienstkonto: "), "Profil mit Rechten zum Anlegen und Lesen von Tickets/Changes, Folgeeinträgen, Aufgaben und Genehmigungen in der Ziel-Entität."),
          h("li", null, h("b", null, "Webhook (optional, für sofortige Rückmeldung): "), "Einrichtung → Webhooks → Hinzufügen. Typ Ticket (und/oder Change, Folgeeintrag, Lösung), Ereignis „Aktualisierung“, Methode POST, URL siehe unten, Geheimnis wie hier. Ohne Webhook fragt das Antragswerk den Status regelmäßig ab."),
          h("li", null, h("b", null, "Rückmeldelink: "), "Im Tickettext steht ", h("code", null, "{{.glpi.rueckmeldung}}"), " – darüber melden Techniker/innen „erledigt“ samt Angaben, auch ohne Webhook.")))));
    if (!G.length) main.append(h("div", { class: "card empty" }, "Noch keine GLPI-Verbindung."));
    G.forEach((c, idx) => main.append(connCard(c, idx)));
    main.append(h("div", { class: "card" }, h("button", { class: "btn", onclick: () => { G.push({ key: G.length ? "glpi" + (G.length + 1) : "glpi", name: "GLPI", url: "https://", api_path: "/api.php", grant: "password", scope: "api", language: "de_DE", poll_minutes: 5 }); dirty(); render(); } }, "+ GLPI-Verbindung")));
  }
  function connCard(c, idx) {
    const inp = (k, attrs) => h("input", Object.assign({ value: c[k] == null ? "" : c[k], oninput: (e) => { c[k] = attrs && attrs.type === "number" ? (parseInt(e.target.value, 10) || 0) : e.target.value; dirty(); } }, attrs || {}));
    const lab = (t, el, hint) => h("label", null, t, el, hint ? h("small", { class: "muted" }, hint) : null);
    const out = h("div");
    const grant = h("select", { onchange: (e) => { c.grant = e.target.value; dirty(); render(); } }, h("option", { value: "password" }, "Password (Dienstkonto)"), h("option", { value: "client_credentials" }, "Client credentials"));
    grant.value = c.grant || "password";
    const hook = P.base_url + "/api/glpi/webhook/" + encodeURIComponent(c.key || "");
    const kind = h("select", { style: "width:auto;margin:0" }, P.lookups.map((l) => h("option", { value: l.kind }, l.label)));
    const q = h("input", { type: "search", placeholder: "Name (leer = alle)", style: "margin:0;max-width:240px" });
    const res = h("div", { class: "lookupres" });
    const lookup = async () => {
      res.innerHTML = ""; res.append(h("p", { class: "muted small" }, "Suche …"));
      try {
        const r = await AW.api("POST", "/api/glpi/nachschlagen", { conn: c, kind: kind.value, q: q.value });
        res.innerHTML = "";
        if (!r.results.length) { res.append(h("p", { class: "muted small" }, "Nichts gefunden.")); return; }
        res.append(h("table", { class: "etable small" }, h("thead", null, h("tr", null, h("th", null, "ID"), h("th", null, "Name"), h("th", null, "Vollständig"), h("th"))),
          h("tbody", null, r.results.map((x) => h("tr", null, h("td", { class: "mono" }, x.id), h("td", null, x.name), h("td", { class: "muted" }, x.full || ""),
            h("td", null, h("button", { class: "btn tiny ghost", type: "button", onclick: () => { navigator.clipboard && navigator.clipboard.writeText(String(x.id)); AW.toast("ID " + x.id + " kopiert"); } }, "ID kopieren")))))));
      } catch (e) { res.innerHTML = ""; res.append(h("div", { class: "hint err" }, e.message)); }
    };
    return h("div", { class: "card glpiconn" },
      h("div", { style: "display:flex;align-items:center;gap:10px;margin-bottom:10px" }, h("span", { class: "gb-logo" }, "GLPI"), h("h2", { style: "margin:0;flex:1" }, c.name || c.key),
        h("button", { class: "btn small ghost", style: "color:var(--bad)", onclick: () => { if (confirm("Verbindung entfernen? Abläufe mit diesem Schlüssel können dann nicht mehr übergeben.")) { G.splice(idx, 1); dirty(); render(); } } }, "Entfernen")),
      h("div", { class: "fgrid3" },
        lab("Name", inp("name")), lab("Schlüssel", inp("key", { class: "mono" }), "im Designer und in der Webhook-URL"),
        lab("Abfrage alle … Minuten", inp("poll_minutes", { type: "number", min: 0 }), "0 = nur Webhook (stündliche Kontrolle)")),
      h("div", { class: "fgrid2" },
        lab("GLPI-Adresse", inp("url", { class: "mono", placeholder: "https://glpi.stadt.local" })),
        lab("API-Pfad", inp("api_path", { class: "mono", placeholder: "/api.php" }), "optional mit Version, z. B. /api.php/v2.2")),
      h("div", { class: "fgrid3" },
        lab("Anmeldung (OAuth2)", grant), lab("Client-ID", inp("client_id", { class: "mono" })), lab("Client-Secret", inp("client_secret", { type: "password", autocomplete: "new-password" }))),
      (c.grant || "password") === "password" ? h("div", { class: "fgrid3" }, lab("Dienstkonto", inp("username", { autocomplete: "off" })), lab("Kennwort", inp("password", { type: "password", autocomplete: "new-password" })), lab("Scope", inp("scope", { placeholder: "api" }))) : h("div", { class: "fgrid3" }, lab("Scope", inp("scope", { placeholder: "api" }))),
      h("div", { class: "fgrid3" },
        lab("Standard-Entität", inp("entity", { placeholder: "ID oder vollständiger Name" }), "wenn der Schritt keine angibt"),
        lab("Sprache", inp("language", { placeholder: "de_DE" }), "Accept-Language (Statusnamen)"),
        lab("Webhook-Geheimnis", h("div", { style: "display:flex;gap:6px" }, inp("webhook_secret", { type: "password", autocomplete: "new-password", style: "margin-top:6px" }),
          h("button", { class: "btn small", type: "button", style: "margin-top:6px", onclick: () => { c.webhook_secret = randomSecret(); dirty(); navigator.clipboard && navigator.clipboard.writeText(c.webhook_secret); AW.toast("Neues Geheimnis erzeugt und kopiert – in GLPI eintragen, dann speichern."); render(); } }, "erzeugen")), "prüft X-GLPI-signature")),
      h("label", { class: "chk", style: "display:inline-flex;gap:6px;font-weight:500" }, h("input", { type: "checkbox", checked: !!c.insecure, onchange: (e) => { c.insecure = e.target.checked; dirty(); } }), "Zertifikat nicht prüfen (nur für Tests)"),
      h("div", { class: "callout small" }, h("b", null, "Webhook-URL für GLPI: "), h("code", null, hook), " ", h("button", { class: "btn tiny ghost", type: "button", onclick: () => { navigator.clipboard && navigator.clipboard.writeText(hook); AW.toast("Kopiert"); } }, "kopieren")),
      h("div", { style: "display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-top:10px" },
        h("button", { class: "btn small", onclick: async () => {
          out.innerHTML = ""; out.append(h("p", { class: "muted small" }, "Prüfe …"));
          try {
            const r = await AW.api("POST", "/api/glpi/test", c);
            out.innerHTML = "";
            out.append(r.ok ? h("div", { class: "hint ok" }, "✓ Anmeldung erfolgreich" + (r.entities != null ? " · " + r.entities + " Entität(en) sichtbar" : "") + (r.warn ? " – " + r.warn : "")) : h("div", { class: "hint err" }, r.error));
          } catch (e) { out.innerHTML = ""; out.append(h("div", { class: "hint err" }, e.message)); }
        } }, "▶ Verbindung testen"),
        h("span", { class: "muted small", style: "margin-left:12px" }, "IDs nachschlagen:"), kind, q, h("button", { class: "btn small ghost", onclick: lookup }, "Suchen")),
      out, res);
  }

  // ---------- Startseite ----------
  function renderHome(main) {
    const H = S.home;
    const views = P.views;
    const vgrid = h("div", { class: "viewgrid" });
    views.forEach((v) => {
      const on = H.allowed.includes(v.key);
      vgrid.append(h("label", { class: "viewcard" + (on ? " on" : "") },
        h("input", { type: "checkbox", checked: on, onchange: (e) => { H.allowed = e.target.checked ? H.allowed.concat(v.key) : H.allowed.filter((x) => x !== v.key); dirty(); render(); } }),
        h("span", { class: "vc-ico" }, v.icon), h("span", null, h("b", null, v.label), h("small", null, v.hint))));
    });
    const roles = h("div", { class: "rolegrid" }, P.roles.map((r) => h("label", null, r.label,
      (() => { const s = h("select", { onchange: (e) => { H.default[r.key] = e.target.value; dirty(); } }, views.filter((v) => H.allowed.includes(v.key)).map((v) => h("option", { value: v.key }, v.icon + " " + v.label))); s.value = H.default[r.key] || H.allowed[0]; return s; })())));
    main.append(h("div", { class: "card" }, h("h2", null, "Ansichten"),
      h("p", { class: "small muted" }, "Gewählte Ansichten erscheinen als Umschalter oben auf der Startseite. Die Auswahl merkt sich jeder Browser."), vgrid),
      h("div", { class: "card" }, h("h2", null, "Standard je Rolle"), h("p", { class: "small muted" }, "Welche Ansicht sieht jemand, der die Startseite zum ersten Mal öffnet?"), roles),
      h("div", { class: "card" }, h("h2", null, "Willkommenstext"), h("p", { class: "small muted" }, "Erscheint oben auf der Startseite (alle Ansichten). **fett** und Zeilenumbrüche möglich; leer = aus."),
        h("textarea", { rows: 3, oninput: (e) => { H.welcome = e.target.value; dirty(); } }, H.welcome || "")),
      h("div", { class: "card" }, h("h2", null, "Hinweisbanner"), h("p", { class: "small muted" }, "Erscheint auf allen Seiten unter der Kopfzeile – z. B. für Wartungsfenster. Leer = aus."),
        h("textarea", { rows: 2, placeholder: "z. B. Am Freitag ab 14 Uhr werden keine Konten angelegt (Wartung AD).", oninput: (e) => { H.banner = e.target.value; dirty(); } }, H.banner || ""),
        h("div", { class: "seg", style: "margin-top:8px" }, [["info", "Information"], ["warn", "Warnung"], ["ok", "Erfolg"], ["bad", "Störung"]].map(([v, l]) =>
          h("button", { type: "button", class: (H.tone || "info") === v ? "on" : "", onclick: () => { H.tone = v; dirty(); render(); } }, l)))));
  }

  // ---------- Texte ----------
  function renderTexts(main) {
    const groups = [];
    P.texts.forEach((t) => { if (!groups.includes(t.group)) groups.push(t.group); });
    const q = h("input", { type: "search", placeholder: "Text oder Schlüssel suchen …", value: st.q, style: "margin:0;max-width:320px", oninput: (e) => { st.q = e.target.value; draw(); } });
    const gsel = h("select", { style: "margin:0;width:auto", onchange: (e) => { st.group = e.target.value; draw(); } }, h("option", { value: "" }, "Alle Bereiche"), groups.map((g) => h("option", { value: g }, g)));
    gsel.value = st.group;
    const only = h("label", { class: "chk", style: "margin:0;display:inline-flex;gap:6px;font-weight:500" }, h("input", { type: "checkbox", onchange: () => draw() }), "nur geänderte");
    const list = h("div", { class: "textlist" });
    const fileIn = h("input", { type: "file", accept: ".json", hidden: true, onchange: async (e) => {
      const f = e.target.files[0]; if (!f) return;
      try { const j = JSON.parse(await f.text()); let n = 0; Object.entries(j).forEach(([k, v]) => { if (P.texts.some((t) => t.key === k) && typeof v === "string") { S.texts[k] = v; n++; } }); dirty(); render(); AW.toast(n + " Texte übernommen – bitte speichern."); }
      catch (x) { AW.toast("Datei ist kein gültiges JSON.", "err"); }
    } });
    function draw() {
      list.innerHTML = "";
      const s = st.q.trim().toLowerCase();
      let cur = "";
      P.texts.forEach((t) => {
        if (st.group && t.group !== st.group) return;
        const val = S.texts[t.key];
        if (only.querySelector("input").checked && val == null) return;
        if (s && !(t.key + " " + t.label + " " + t.default + " " + (val || "")).toLowerCase().includes(s)) return;
        if (t.group !== cur) { cur = t.group; list.append(h("h3", { class: "tl-group" }, cur)); }
        const multi = t.tpl || t.default.length > 90 || t.default.includes("\n");
        const changed = val != null;
        const field = multi ? h("textarea", { rows: t.default.split("\n").length > 4 ? 12 : 3, class: t.tpl ? "mono" : "", placeholder: t.default, oninput: (e) => { setText(t, e.target.value, row); } }, changed ? val : "")
          : h("input", { value: changed ? val : "", placeholder: t.default, oninput: (e) => setText(t, e.target.value, row) });
        const row = h("div", { class: "tl-row" + (changed ? " changed" : "") },
          h("div", { class: "tl-head" }, h("b", null, t.label), h("code", null, t.key), h("span", { class: "tl-badge" }, "geändert"),
            h("button", { type: "button", class: "linkbtn", onclick: () => { delete S.texts[t.key]; dirty(); draw(); } }, "Vorgabe wiederherstellen"),
            multi ? h("button", { type: "button", class: "linkbtn", onclick: () => { field.value = field.value || t.default; setText(t, field.value, row); field.focus(); } }, "Vorgabe zum Bearbeiten übernehmen") : null),
          field,
          t.tpl ? h("small", { class: "muted" }, "Vorlage: {{.antrag.nummer}} {{.antrag.titel}} {{.antrag.antragsteller_name}} {{.antrag.datum}} {{.link}} · Schritt: {{.schritt.titel}} {{.schritt.eintrag}} {{.schritt.frist}} {{.schritt.zustaendig}} {{.schritt.warum}} {{.schritt.anleitung}} {{range .schritt.checkliste}} · {{.zusammenfassung}} (Freigaben) · {{.begruendung}} (Ablehnung/Rückfrage) · alle Antragsfelder und {{.c.…}}") : null);
        list.append(row);
      });
      if (!list.children.length) list.append(h("p", { class: "muted" }, "Keine Treffer."));
    }
    function setText(t, v, row) {
      if (v === "" || v === t.default) delete S.texts[t.key]; else S.texts[t.key] = v;
      row.classList.toggle("changed", S.texts[t.key] != null);
      dirty();
    }
    only.querySelector("input").addEventListener("change", draw);
    main.append(h("div", { class: "card" },
      h("div", { class: "cardhead" }, h("h2", null, "Texte & Mails"), h("div", { class: "actions" },
        h("button", { class: "btn small ghost", onclick: () => { const b = new Blob([JSON.stringify(S.texts, null, 2)], { type: "application/json" }); const a = h("a", { href: URL.createObjectURL(b), download: "antragswerk-texte.json" }); document.body.append(a); a.click(); a.remove(); } }, "⇩ Export"),
        h("button", { class: "btn small ghost", onclick: () => fileIn.click() }, "⇧ Import"), fileIn)),
      h("p", { class: "small muted" }, "Leer lassen = Vorgabe (grau). Mails sind Vorlagen mit Platzhaltern; eigene Mailtexte je Schritt im Designer haben Vorrang. Texte eines Ablaufs (Feldnamen, Schrittnamen, Hinweise) bearbeiten Sie im Designer."),
      h("div", { class: "filters", style: "margin-bottom:8px" }, q, gsel, only), list));
    draw();
  }

  render();
})();
