// Antragswerk – Ablauf-Designer
// Reiter: Formular (Baukasten) · Ablauf (Graph) · Einstellungen · Test & Vorschau · Prüfung
"use strict";
(function () {
  const dataEl = document.getElementById("designer-data");
  const root = document.getElementById("designer");
  if (!dataEl || !root) return;
  const AW = window.AW;
  const h = AW.h;
  const D = JSON.parse(dataEl.textContent);
  const RO = !!D.readonly;
  const KEY = D.key;
  const FT = D.field_types, SK = D.step_kinds, OPS = D.ops;
  const STELLEN = D.stellen || [], CATS = D.catalogs || [], VARS = D.vars || [], SNIPS = D.snippets || [];
  const catByKey = Object.fromEntries(CATS.map((c) => [c.key, c]));
  const stByKey = Object.fromEntries(STELLEN.map((s) => [s.key, s]));
  const ftByKey = Object.fromEntries(FT.map((t) => [t.key, t]));
  const skByKey = Object.fromEntries(SK.map((t) => [t.key, t]));
  const KCOL = { freigabe: "var(--k-freigabe)", aufgabe: "var(--k-aufgabe)", info: "var(--k-info)", warten: "var(--k-warten)", glpi: "var(--k-glpi)" };
  const GL = D.glpi || { conns: [], lookups: [], status: {}, done: {}, fail: {}, content: "" };
  const store = (k, v) => { try { if (v === undefined) return localStorage.getItem(k); localStorage.setItem(k, v); } catch (e) { /* */ } return null; };

  let uidN = 0;
  const uid = () => "u" + ++uidN;
  function prep(def) {
    def.form = def.form || { sections: [] };
    def.form.sections = def.form.sections || [];
    def.form.sections.forEach((s) => { s._u = uid(); s.fields = s.fields || []; s.fields.forEach((f) => { f._u = uid(); (f.fields || []).forEach((c) => { c._u = uid(); }); }); });
    def.steps = (def.steps || []).map((s) => Object.assign({ after: [], assign: { type: "stelle" }, checklist: [], scripts: [], outputs: [] }, s, { after: s.after || [], assign: s.assign || { type: "stelle" } }));
    def.computed = def.computed || [];
    def.audience = def.audience || [];
    def.done = def.done || {};
    return def;
  }
  let def = prep(JSON.parse(JSON.stringify(D.def)));
  const clean = (d) => JSON.parse(JSON.stringify(d, (k, v) => (k.startsWith("_") ? undefined : v)));

  const S = {
    tab: store("awd-tab") || "form", sel: null, dirty: false, undo: [], redo: [], issues: D.issues || [],
    preview: false, test: null, snapTaken: false, zoom: 1,
  };
  if (!["form", "flow", "settings", "test", "check"].includes(S.tab)) S.tab = "form";

  // ---------- Testdaten ----------
  const SAMPLE = { vorname: "Maria", nachname: "Musterfrau", funktion: "Sachbearbeitung", firma: "Muster IT GmbH", personalnummer: "104711", titel: "", zweck: "Wartung des Fachverfahrens", begruendung: "Wird für die tägliche Arbeit benötigt." };
  function sampleValue(f, i) {
    const iso = (d) => { const t = new Date(); t.setDate(t.getDate() + d); return t.toISOString().slice(0, 10); };
    switch (f.type) {
      case "text": return SAMPLE[f.key] != null ? SAMPLE[f.key] : (f.placeholder && !/^z\. ?B\./.test(f.placeholder) ? "" : f.label + (i ? " " + i : ""));
      case "textarea": return SAMPLE[f.key] || "Beispieltext für " + f.label;
      case "number": return f.default || "2";
      case "date": return iso(14 + (i || 0) * 30);
      case "email": return "max.beispiel@example.org";
      case "phone": return "-1234";
      case "select": case "radio": return f.default || ((f.options || [])[0] || {}).value || "";
      case "checkbox": return f.default ? f.default === "true" : true;
      case "checkboxes": return (f.options || []).slice(0, 1).map((o) => o.value);
      case "catalog": {
        const c = catByKey[f.catalog];
        const en = c ? c.entries.filter((e) => !e.inactive) : [];
        if (f.multi) return en.slice(0, 2).map((e) => e.key);
        return (en[(i || 0) % Math.max(1, en.length)] || {}).key || "";
      }
      case "user": return { sam: "m.schulz", name: "Markus Schulz", given: "Markus", surname: "Schulz", mail: "m.schulz@demo-stadt.de", department: "Bürgerservice (32)", title: "Abteilungsleiter", phone: "-3200", office: "Rathaus B 0.04" };
      case "group": return [0, 1].map((n) => { const it = {}; (f.fields || []).forEach((c) => { if (c.type !== "info") it[c.key] = sampleValue(c, n); }); return it; });
    }
    return "";
  }
  function sampleData() {
    const d = {};
    def.form.sections.forEach((s) => s.fields.forEach((f) => { if (f.type !== "info") d[f.key] = sampleValue(f, 0); }));
    return d;
  }
  try { S.test = JSON.parse(store("awd-test-" + KEY) || "null"); } catch (e) { S.test = null; }
  const testData = () => {
    const base = sampleData();
    if (S.test) Object.keys(base).forEach((k) => { if (S.test[k] !== undefined) base[k] = S.test[k]; });
    return base;
  };

  // ---------- Zugriff ----------
  const allFields = () => def.form.sections.flatMap((s) => s.fields);
  const fieldByKey = (k) => allFields().find((f) => f.key === k);
  const stepById = (id) => def.steps.find((s) => s.id === id);
  function findField(u) {
    for (const s of def.form.sections) {
      for (let i = 0; i < s.fields.length; i++) {
        const f = s.fields[i];
        if (f._u === u) return { f, list: s.fields, i, sec: s, parent: null };
        for (let j = 0; j < (f.fields || []).length; j++) if (f.fields[j]._u === u) return { f: f.fields[j], list: f.fields, i: j, sec: s, parent: f };
      }
    }
    return null;
  }
  const secByU = (u) => def.form.sections.find((s) => s._u === u);
  function slug(s) {
    const r = { "ä": "ae", "ö": "oe", "ü": "ue", "ß": "ss" };
    let x = String(s || "").toLowerCase().replace(/[äöüß]/g, (c) => r[c]).replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "");
    if (!/^[a-z]/.test(x)) x = "f_" + x;
    return x.slice(0, 40) || "feld";
  }
  function uniqueKey(base, taken) {
    let k = base, n = 2;
    while (taken.has(k)) k = base + "_" + n++;
    return k;
  }
  const takenKeys = () => new Set(allFields().map((f) => f.key));

  // ---------- Undo ----------
  function snapshot() { S.undo.push(JSON.stringify(def)); if (S.undo.length > 120) S.undo.shift(); S.redo = []; }
  function mutate(fn, opt) {
    if (RO) return;
    snapshot();
    fn();
    setDirty();
    render(opt);
  }
  // Tippen: einmal Schnappschuss je Fokus, nur Canvas neu zeichnen
  function live(fn) {
    if (RO) return;
    if (!S.snapTaken) { snapshot(); S.snapTaken = true; }
    fn();
    setDirty();
    clearTimeout(live.t);
    live.t = setTimeout(() => renderCanvas(), 120);
  }
  document.addEventListener("focusout", () => { S.snapTaken = false; });
  function undo() { if (!S.undo.length) return; S.redo.push(JSON.stringify(def)); def = prep(JSON.parse(S.undo.pop())); setDirty(); render(); }
  function redo() { if (!S.redo.length) return; S.undo.push(JSON.stringify(def)); def = prep(JSON.parse(S.redo.pop())); setDirty(); render(); }
  function setDirty() { S.dirty = true; updateStatus(); validateSoon(); }

  // ---------- Prüfung ----------
  function validateSoon() {
    clearTimeout(validateSoon.t);
    validateSoon.t = setTimeout(async () => {
      try { const r = await AW.api("POST", "/api/designer/validate", { def: clean(def) }); S.issues = r.issues || []; renderTabs(); if (S.tab === "check") renderCanvas(); } catch (e) { /* */ }
    }, 700);
  }
  const issuesFor = (ref) => S.issues.filter((i) => i.ref === ref);

  // ---------- Speichern ----------
  async function save(quiet) {
    if (RO) return false;
    const btn = root.querySelector("[data-save]");
    if (btn) btn.disabled = true;
    try {
      const r = await AW.api("PUT", "/api/designer/" + encodeURIComponent(KEY), { def: clean(def) });
      S.issues = r.issues || [];
      S.dirty = false;
      D.has_draft = true;
      if (!quiet) AW.toast("Entwurf gespeichert", "ok");
      renderTop();
      return true;
    } catch (e) { AW.toast(e.message, "err"); return false; } finally { if (btn) btn.disabled = false; }
  }
  window.addEventListener("beforeunload", (e) => { if (S.dirty) { e.preventDefault(); e.returnValue = ""; } });
  document.addEventListener("keydown", (e) => {
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === "s") { e.preventDefault(); save(); }
    else if (mod && e.key.toLowerCase() === "z" && !e.shiftKey && !isTyping()) { e.preventDefault(); undo(); }
    else if (mod && (e.key.toLowerCase() === "y" || (e.key.toLowerCase() === "z" && e.shiftKey)) && !isTyping()) { e.preventDefault(); redo(); }
    else if ((e.key === "Delete" || e.key === "Backspace") && !isTyping() && S.sel) { e.preventDefault(); removeSel(); }
    else if (e.key === "Escape" && !document.querySelector(".modal-bg")) { S.sel = null; render(); }
  });
  const isTyping = () => { const a = document.activeElement; return a && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName); };

  // ---------- Grundgerüst ----------
  let topEl, tabsEl, bodyEl, palEl, canvasEl, inspEl;
  function mount() {
    root.innerHTML = "";
    topEl = h("div", { class: "dz-top" });
    bodyEl = h("div", { class: "dz-body" });
    palEl = h("aside", { class: "dz-pal" });
    canvasEl = h("div", { class: "dz-canvas" });
    inspEl = h("aside", { class: "dz-insp insp" });
    bodyEl.append(palEl, canvasEl, inspEl);
    root.append(topEl);
    if (D.archived) root.append(h("div", { class: "dz-off" }, "⏸ Dieser Antrag ist deaktiviert und kann nicht gestellt werden" + (D.disabled_info ? " (" + D.disabled_info + ")" : "") + ".",
      D.disabled_note ? h("span", { class: "note" }, "Hinweis: " + D.disabled_note) : null, h("span", { class: "sp" }),
      RO ? null : h("button", { class: "btn small", onclick: () => toggleDisabled(false) }, "▶ Wieder aktivieren")));
    root.append(bodyEl);
    render();
  }
  function render(opt) {
    renderTop();
    renderPalette();
    renderCanvas();
    if (!opt || !opt.keepInspector) renderInspector();
  }

  function renderTop() {
    topEl.innerHTML = "";
    const status = D.archived && !RO ? h("span", { class: "pill s-bad" }, "⏸ deaktiviert" + (D.has_draft ? " · Entwurf" : ""))
      : RO ? h("span", { class: "pill s-muted" }, "Version " + D.version + " · nur ansehen")
      : D.has_draft ? h("span", { class: "pill s-warn" }, "Entwurf" + (D.has_active ? " (aktiv: v" + (D.versions.find((v) => v.status === "aktiv") || {}).v + ")" : ""))
        : h("span", { class: "pill s-ok" }, "Aktiv · v" + D.version);
    tabsEl = h("div", { class: "dz-tabs" });
    topEl.append(
      h("a", { class: "btn ghost small", href: "/designer", title: "Zur Übersicht" }, "←"),
      h("span", { class: "qico", style: "--c:" + (def.color || "#1f5fbf") }, def.icon || "📝"),
      h("div", { class: "dz-title" }, h("b", null, def.name || "(ohne Namen)"), h("small", null, status, " ", D.open ? D.open + " laufende Vorgänge · " : "", "Schlüssel ", h("code", null, KEY))),
      tabsEl,
      h("span", { class: "dz-sp" }),
      h("span", { class: "dz-status" }, S.dirty ? "● ungespeichert" : ""),
    );
    renderTabs();
    const ap = (...x) => topEl.append(...x.filter(Boolean));
    if (!RO) {
      ap(
        h("button", { class: "btn ghost small", title: "Rückgängig (Strg+Z)", disabled: !S.undo.length, onclick: undo }, "↶"),
        h("button", { class: "btn ghost small", title: "Wiederholen (Strg+Y)", disabled: !S.redo.length, onclick: redo }, "↷"),
        versionsMenu(),
        h("button", { class: "btn small", "data-save": "", onclick: () => save() }, "Speichern"),
        h("button", { class: "btn small primary", onclick: publish }, "Veröffentlichen …"));
    } else {
      ap(versionsMenu(), h("a", { class: "btn small primary", href: "/designer/" + KEY }, "Zur aktuellen Fassung"));
    }
  }
  function renderTabs() {
    if (!tabsEl) return;
    const err = S.issues.filter((i) => i.level === "error").length, warn = S.issues.length - err;
    tabsEl.innerHTML = "";
    [["form", "▤ Formular"], ["flow", "⇄ Ablauf"], ["settings", "⚙ Einstellungen"], ["test", "▶ Test & Vorschau"], ["check", "✓ Prüfung"]].forEach(([k, l]) => {
      tabsEl.append(h("button", { class: S.tab === k ? "on" : "", onclick: () => { S.tab = k; store("awd-tab", k); S.sel = null; canvasEl.scrollLeft = 0; canvasEl.scrollTop = 0; render(); } }, l,
        k === "check" && (err || warn) ? h("span", { class: "n" + (err ? "" : " w") }, err || warn) : null));
    });
  }
  function updateStatus() { const s = topEl && topEl.querySelector(".dz-status"); if (s) s.textContent = S.dirty ? "● ungespeichert" : ""; }

  function versionsMenu() {
    const d = h("details", { class: "dropdown" }, h("summary", { class: "btn small ghost" }, "Versionen & mehr ▾"));
    const box = h("div", { class: "card dropform", style: "width:380px" }, h("b", null, "Versionen"),
      h("p", { class: "small muted" }, "Laufende Vorgänge bleiben auf der Version, mit der sie gestellt wurden."));
    (D.versions || []).forEach((v) => box.append(h("div", { class: "small", style: "padding:6px 0;border-top:1px solid var(--border)" },
      h("b", null, v.status === "entwurf" ? "Entwurf" : "v" + v.v), " ", v.status === "aktiv" ? h("span", { class: "pill s-ok" }, "aktiv") : v.status === "abgeloest" ? h("span", { class: "pill s-muted" }, "abgelöst") : null,
      " ", h("span", { class: "muted" }, v.at + " · " + v.by), v.note ? h("div", null, v.note) : null,
      v.status !== "entwurf" ? h("div", null, h("a", { href: "/designer/" + KEY + "?v=" + v.id }, "ansehen")) : null)));
    box.append(h("div", { class: "actions", style: "border-top:1px solid var(--border);padding-top:10px;margin-top:4px" },
      h("a", { class: "btn small", href: "/designer/" + KEY + "/export.yaml" }, "⇩ YAML exportieren"),
      RO ? null : D.archived ? h("button", { class: "btn small", onclick: () => toggleDisabled(false) }, "▶ Aktivieren")
        : h("button", { class: "btn small", style: "color:var(--bad)", title: "Antrag für neue Anträge sperren", onclick: () => { d.open = false; toggleDisabled(true); } }, "⏸ Antrag deaktivieren …")));
    d.append(box);
    return d;
  }

  function toggleDisabled(off) {
    const go = (note) => {
      const f = h("form", { method: "post", action: "/designer/" + KEY + "/archive" }, h("input", { type: "hidden", name: "csrf", value: AW.csrf }),
        h("input", { type: "hidden", name: "archived", value: off ? "1" : "0" }), h("input", { type: "hidden", name: "note", value: note || "" }), h("input", { type: "hidden", name: "back", value: "designer" }));
      document.body.append(f);
      f.submit();
    };
    if (!off) { if (S.dirty && !confirm("Ungespeicherte Änderungen verwerfen?")) return; S.dirty = false; go(""); return; }
    const m = modal("Antrag deaktivieren");
    const note = h("textarea", { rows: 3, placeholder: "z. B. Wird überarbeitet – bis dahin bitte formlos an it-benutzer@ wenden." });
    m.body.append(h("p", { class: "muted" }, "„" + def.name + "“ verschwindet aus „Antrag stellen“ und kann nicht mehr neu beantragt werden. " +
      (D.open ? D.open + " laufende Vorgänge werden normal zu Ende bearbeitet. " : "") + "Der Ablauf bleibt erhalten und lässt sich jederzeit wieder aktivieren."),
      h("label", null, "Hinweis für Antragstellende (optional)", note),
      h("div", { class: "actions" }, h("button", { class: "btn danger", onclick: async () => { if (S.dirty && !(await save(true))) return; S.dirty = false; go(note.value); } }, "Deaktivieren"),
        h("button", { class: "btn ghost", onclick: m.close }, "Abbrechen")));
    note.focus();
  }

  async function publish() {
    if (S.issues.some((i) => i.level === "error")) { AW.toast("Erst die Fehler unter „Prüfung“ beheben.", "err"); S.tab = "check"; render(); return; }
    const m = modal("Veröffentlichen");
    const note = h("input", { placeholder: "z. B. Freigabe Datenschutz bei Externen ergänzt" });
    m.body.append(h("p", { class: "muted" }, "Neue Anträge nutzen ab sofort die neue Version. Laufende Vorgänge bleiben auf ihrer Version."),
      h("label", null, "Änderungsnotiz", note),
      h("div", { class: "actions" }, h("button", { class: "btn primary", onclick: async () => {
        if (S.dirty && !(await save(true))) return;
        const f = h("form", { method: "post", action: "/designer/" + KEY + "/publish" }, h("input", { type: "hidden", name: "csrf", value: AW.csrf }), h("input", { type: "hidden", name: "note", value: note.value }));
        document.body.append(f);
        S.dirty = false;
        f.submit();
      } }, "Jetzt veröffentlichen"), h("button", { class: "btn ghost", onclick: m.close }, "Abbrechen")));
    note.focus();
  }

  function modal(title, wide) {
    const bg = h("div", { class: "modal-bg" });
    const body = h("div");
    const close = () => bg.remove();
    bg.append(h("div", { class: "modal" + (wide ? " wide" : "") }, h("div", { class: "modal-h" }, h("h2", null, title), h("button", { class: "x", onclick: close }, "×")), body));
    bg.addEventListener("mousedown", (e) => { if (e.target === bg) close(); });
    document.body.append(bg);
    return { body, close };
  }

  // ---------- Palette ----------
  function renderPalette() {
    palEl.innerHTML = "";
    bodyEl.classList.toggle("nopal", S.tab === "settings" || S.tab === "test" || S.tab === "check");
    palEl.hidden = bodyEl.classList.contains("nopal");
    inspEl.hidden = S.tab === "test" || S.tab === "check";
    if (S.tab === "test" || S.tab === "check") bodyEl.style.gridTemplateColumns = "1fr"; else bodyEl.style.gridTemplateColumns = "";
    if (S.tab === "form") {
      palEl.append(h("div", { class: "pal-h" }, "Feldarten"));
      FT.forEach((t) => {
        const it = h("div", { class: "pal-item", draggable: !RO, title: "Ziehen oder klicken zum Hinzufügen" },
          h("span", { class: "pi" }, t.icon), h("span", null, h("b", null, t.label), h("small", null, t.hint)));
        it.addEventListener("dragstart", (e) => { e.dataTransfer.setData("text/aw", JSON.stringify({ palette: t.key })); e.dataTransfer.effectAllowed = "copy"; });
        it.addEventListener("click", () => addFieldAtSelection(t.key));
        palEl.append(it);
      });
      palEl.append(h("button", { class: "btn block", style: "margin-top:8px", disabled: RO, onclick: addSection }, "+ Eingabegruppe"),
        h("div", { class: "pal-tip" }, "Felder auf eine Eingabegruppe oder in eine wiederholbare Gruppe ziehen. Breite, Pflicht und Bedingungen rechts im Inspektor. ",
          h("b", null, "Vorschau"), " zeigt das Formular wie für Antragstellende."));
    } else if (S.tab === "flow") {
      palEl.append(h("div", { class: "pal-h" }, "Schrittarten"));
      SK.forEach((k) => {
        const it = h("div", { class: "pal-item k", style: "--kc:" + KCOL[k.key], draggable: !RO, title: "Auf die Fläche ziehen oder klicken" },
          h("span", { class: "pi", style: "color:" + KCOL[k.key] }, k.icon), h("span", null, h("b", null, k.label), h("small", null, k.hint)));
        it.addEventListener("dragstart", (e) => { e.dataTransfer.setData("text/aw", JSON.stringify({ step: k.key })); });
        it.addEventListener("click", () => addStep(k.key));
        palEl.append(it);
      });
      palEl.append(h("div", { class: "pal-h", style: "margin-top:16px" }, "Stellen"));
      STELLEN.forEach((s) => palEl.append(h("div", { class: "small", style: "display:flex;gap:8px;align-items:center;padding:3px 0" },
        h("span", { style: "width:10px;height:10px;border-radius:50%;background:" + (s.color || "#94a3b8") }), s.name)));
      palEl.append(h("div", { class: "pal-tip" }, "Den Punkt rechts an einem Schritt auf einen anderen ziehen = „danach“. Mehrere Nachfolger laufen ",
        h("b", null, "parallel"), "; ein Schritt mit mehreren Vorgängern wartet auf alle. Verbindung anklicken und Entf drücken zum Lösen."));
    }
  }

  // ---------- Canvas ----------
  function renderCanvas() {
    const sx = canvasEl.scrollLeft, sy = canvasEl.scrollTop;
    canvasEl.innerHTML = "";
    canvasEl.className = "dz-canvas" + (S.tab === "flow" ? " dots" : "");
    if (S.tab === "form") (S.preview ? renderFormPreview() : renderFormBuilder());
    else if (S.tab === "flow") renderGraph();
    else if (S.tab === "settings") renderSettings();
    else if (S.tab === "test") renderTest();
    else renderCheck();
    canvasEl.scrollLeft = sx; canvasEl.scrollTop = sy;
  }

  // ======================= Formular-Baukasten =======================
  function newField(type) {
    const ft = ftByKey[type];
    const label = type === "info" ? "Hinweistext" : ft.label;
    const f = { _u: uid(), key: uniqueKey(slug(type === "group" ? "eintraege" : label), takenKeys()), label, type };
    if (type === "select" || type === "radio" || type === "checkboxes") f.options = [{ value: "a", label: "Option A" }, { value: "b", label: "Option B" }];
    if (type === "catalog") f.catalog = (CATS[0] || {}).key || "";
    if (type === "group") { f.add_label = "Eintrag hinzufügen"; f.item_label = "Eintrag"; f.fields = [{ _u: uid(), key: "bezeichnung", label: "Bezeichnung", type: "text", required: true, width: "half" }]; }
    if (type === "info") f.tone = "info";
    if (type === "date" || type === "phone" || type === "number") f.width = "third";
    return f;
  }
  function addSection() {
    mutate(() => {
      const ids = new Set(def.form.sections.map((s) => s.id));
      const s = { _u: uid(), id: uniqueKey("abschnitt", ids), title: "Neue Eingabegruppe", icon: "📝", fields: [] };
      def.form.sections.push(s);
      S.sel = { t: "section", u: s._u };
    });
  }
  function addFieldAtSelection(type) {
    if (RO) return;
    let sec = def.form.sections[def.form.sections.length - 1], idx = null, parent = null;
    if (S.sel && S.sel.t === "field") {
      const r = findField(S.sel.u);
      if (r) { if (r.f.type === "group" && type !== "group") { parent = r.f; } else { sec = r.sec; parent = r.parent; idx = r.i + 1; } }
    } else if (S.sel && S.sel.t === "section") sec = secByU(S.sel.u);
    if (!sec) { addSection(); sec = def.form.sections[def.form.sections.length - 1]; }
    mutate(() => {
      const f = newField(type);
      const list = parent ? (parent.fields = parent.fields || []) : sec.fields;
      if (parent && type === "group") return;
      list.splice(idx == null ? list.length : idx, 0, f);
      S.sel = { t: "field", u: f._u };
    });
  }

  function dropIndex(container, e) {
    const kids = Array.from(container.children).filter((c) => c.classList.contains("fb-field") && !c.classList.contains("dragging"));
    for (let i = 0; i < kids.length; i++) {
      const r = kids[i].getBoundingClientRect();
      if (e.clientY < r.top) return i;
      if (e.clientY <= r.bottom && e.clientX < r.left + r.width / 2) return i;
    }
    return kids.length;
  }
  function makeDrop(container, getList, allowGroups) {
    let mark = null;
    const clear = () => { if (mark) { mark.remove(); mark = null; } };
    container.addEventListener("dragover", (e) => {
      if (RO) return;
      e.preventDefault();
      e.stopPropagation();
      const i = dropIndex(container, e);
      const kids = Array.from(container.children).filter((c) => c.classList.contains("fb-field") && !c.classList.contains("dragging"));
      if (!mark) mark = h("div", { class: "dropmark" });
      if (kids[i]) container.insertBefore(mark, kids[i]); else container.append(mark);
    });
    container.addEventListener("dragleave", (e) => { if (!container.contains(e.relatedTarget)) clear(); });
    container.addEventListener("drop", (e) => {
      e.preventDefault();
      e.stopPropagation();
      clear();
      let p;
      try { p = JSON.parse(e.dataTransfer.getData("text/aw") || "{}"); } catch (x) { return; }
      const i = dropIndex(container, e);
      if (p.palette) {
        if (!allowGroups && p.palette === "group") { AW.toast("Gruppen lassen sich nicht verschachteln.", "err"); return; }
        mutate(() => { const f = newField(p.palette); getList().splice(i, 0, f); S.sel = { t: "field", u: f._u }; });
      } else if (p.field) {
        const r = findField(p.field);
        if (!r) return;
        if (!allowGroups && r.f.type === "group") { AW.toast("Gruppen lassen sich nicht verschachteln.", "err"); return; }
        mutate(() => {
          const list = getList();
          let at = i;
          if (r.list === list && r.i < at) at--;
          r.list.splice(r.i, 1);
          list.splice(at, 0, r.f);
          S.sel = { t: "field", u: r.f._u };
        });
      }
    });
  }

  function condBadge(c) { return c && c.rules && c.rules.length ? h("span", { class: "cd" }, "wenn …") : null; }

  function fieldCard(f, sec, inGroup) {
    const ft = ftByKey[f.type] || { icon: "?" };
    const sel = S.sel && S.sel.t === "field" && S.sel.u === f._u;
    const errs = issuesFor("field:" + f.key).length;
    const el = h("div", { class: "fb-field w-" + (f.width || "full") + " t-" + f.type + (sel ? " sel" : ""), draggable: !RO, style: errs ? "border-color:var(--bad)" : null },
      h("div", { class: "fh" }, h("span", { class: "ti" }, ft.icon), h("span", { class: "lb" }, f.label || "(ohne Beschriftung)", f.required ? h("span", { class: "req" }, " *") : null),
        RO ? null : h("span", { class: "fb-tools" },
          h("button", { title: "Duplizieren", onclick: (e) => { e.stopPropagation(); dupField(f._u); } }, "⧉"),
          h("button", { title: "Löschen", onclick: (e) => { e.stopPropagation(); S.sel = { t: "field", u: f._u }; removeSel(); } }, "🗑"))),
      h("div", { class: "meta" }, f.type !== "info" ? h("span", null, f.key) : null,
        f.type === "catalog" ? h("span", { class: "ct" }, "▤ " + ((catByKey[f.catalog] || {}).name || f.catalog) + (f.multi ? " · mehrfach" : "")) : null,
        f.type === "group" ? h("span", { class: "ct" }, "n× " + (f.item_label || "Eintrag") + (f.max ? " (max " + f.max + ")" : "")) : null,
        f.options ? h("span", null, f.options.length + " Optionen") : null,
        f.prefill ? h("span", { class: "ct" }, "⇠ " + f.prefill.from) : null,
        condBadge(f.show)),
      f.type === "info" ? h("div", { class: "small muted", style: "margin-top:4px" }, f.label) : null);
    el.addEventListener("click", (e) => { e.stopPropagation(); S.sel = { t: "field", u: f._u }; render({}); });
    el.addEventListener("dragstart", (e) => { e.stopPropagation(); e.dataTransfer.setData("text/aw", JSON.stringify({ field: f._u })); el.classList.add("dragging"); });
    el.addEventListener("dragend", () => el.classList.remove("dragging"));
    if (f.type === "group" && !inGroup) {
      const sub = h("div", { class: "fb-sub" });
      (f.fields || []).forEach((c) => sub.append(fieldCard(c, sec, true)));
      if (!(f.fields || []).length) sub.append(h("div", { class: "empty-drop" }, "Felder hierher ziehen – sie werden je Eintrag wiederholt"));
      makeDrop(sub, () => (f.fields = f.fields || []), false);
      el.append(sub);
    }
    return el;
  }

  function renderFormBuilder() {
    const wrap = h("div", { class: "fb" });
    wrap.append(h("div", { class: "fb-add", style: "margin-bottom:14px" },
      h("div", { class: "seg" }, h("button", { class: "on" }, "Bearbeiten"), h("button", { onclick: () => { S.preview = true; renderCanvas(); } }, "Vorschau"))));
    def.form.sections.forEach((s, si) => {
      const sel = S.sel && S.sel.t === "section" && S.sel.u === s._u;
      const fields = h("div", { class: "fb-fields" });
      s.fields.forEach((f) => fields.append(fieldCard(f, s, false)));
      if (!s.fields.length) fields.append(h("div", { class: "fb-empty" }, "Feldart aus der linken Leiste hierher ziehen"));
      makeDrop(fields, () => s.fields, true);
      const head = h("div", { class: "fb-sec-head" }, h("span", { class: "qico", style: "--c:" + (def.color || "#1f5fbf") }, s.icon || "•"),
        h("div", { class: "grow" }, h("b", null, s.title || "(ohne Überschrift)"), s.description ? h("small", null, s.description) : null),
        condBadge(s.show) ? h("span", { class: "cond" }, "nur wenn …") : null,
        RO ? null : h("span", { class: "fb-tools" },
          si > 0 ? h("button", { title: "nach oben", onclick: (e) => { e.stopPropagation(); mutate(() => { def.form.sections.splice(si - 1, 0, def.form.sections.splice(si, 1)[0]); }); } }, "↑") : null,
          si < def.form.sections.length - 1 ? h("button", { title: "nach unten", onclick: (e) => { e.stopPropagation(); mutate(() => { def.form.sections.splice(si + 1, 0, def.form.sections.splice(si, 1)[0]); }); } }, "↓") : null,
          h("button", { title: "Löschen", onclick: (e) => { e.stopPropagation(); S.sel = { t: "section", u: s._u }; removeSel(); } }, "🗑")));
      head.addEventListener("click", () => { S.sel = { t: "section", u: s._u }; render({}); });
      wrap.append(h("div", { class: "fb-sec" + (sel ? " sel" : "") }, head, fields));
    });
    if (!RO) wrap.append(h("div", { class: "fb-add" }, h("button", { class: "btn", onclick: addSection }, "+ Eingabegruppe hinzufügen")));
    canvasEl.append(wrap);
    canvasEl.onclick = (e) => { if (e.target === canvasEl || e.target === wrap) { S.sel = null; render(); } };
  }

  function renderFormPreview() {
    const wrap = h("div", { class: "fb-prev" });
    wrap.append(h("div", { class: "fb-add", style: "margin-bottom:14px" },
      h("div", { class: "seg" }, h("button", { onclick: () => { S.preview = false; renderCanvas(); } }, "Bearbeiten"), h("button", { class: "on" }, "Vorschau"))),
      h("p", { class: "small muted", style: "text-align:center" }, "So sieht das Formular für Antragstellende aus. Eingaben hier dienen als Testdaten für Skript- und Ablaufvorschau."));
    const box = h("div");
    wrap.append(box);
    canvasEl.append(wrap);
    mountPreviewForm(box);
  }
  function catalogsMap() { return Object.fromEntries(CATS.map((c) => [c.key, Object.assign({}, c, { entries: c.entries.filter((e) => !e.inactive) })])); }
  function mountPreviewForm(box) {
    AW.mountForm(box, { mode: "preview", key: KEY, def: clean(def), catalogs: catalogsMap(), data: testData(), me: {} }, {
      preview: true,
      onChange: (d) => { S.test = d; store("awd-test-" + KEY, JSON.stringify(d)); },
      simulate: (d) => AW.api("POST", "/api/designer/simulate", { def: clean(def), data: d }),
    });
  }

  function dupField(u) {
    const r = findField(u);
    if (!r) return;
    mutate(() => {
      const c = JSON.parse(JSON.stringify(r.f));
      c._u = uid();
      (c.fields || []).forEach((x) => { x._u = uid(); });
      c.key = uniqueKey(c.key, r.parent ? new Set(r.list.map((x) => x.key)) : takenKeys());
      r.list.splice(r.i + 1, 0, c);
      S.sel = { t: "field", u: c._u };
    });
  }

  function removeSel() {
    if (RO || !S.sel) return;
    const sel = S.sel;
    if (sel.t === "field") {
      const r = findField(sel.u);
      if (!r) return;
      const used = def.steps.filter((s) => JSON.stringify(s).includes("." + r.f.key) || s.foreach === r.f.key || (s.assign || {}).field === r.f.key);
      if (used.length && !confirm("Das Feld „" + r.f.label + "“ wird in " + used.length + " Schritt(en) verwendet (" + used.map((s) => s.title).join(", ") + "). Trotzdem löschen?")) return;
      mutate(() => { r.list.splice(r.i, 1); S.sel = null; });
    } else if (sel.t === "section") {
      const i = def.form.sections.findIndex((s) => s._u === sel.u);
      if (i < 0) return;
      if (def.form.sections[i].fields.length && !confirm("Eingabegruppe mit " + def.form.sections[i].fields.length + " Feldern löschen?")) return;
      mutate(() => { def.form.sections.splice(i, 1); S.sel = null; });
    } else if (sel.t === "step") {
      const st = stepById(sel.id);
      if (!st || !confirm("Schritt „" + st.title + "“ löschen? Nachfolger übernehmen seine Vorgänger.")) return;
      mutate(() => {
        def.steps.forEach((s) => {
          if (s.after.includes(st.id)) s.after = Array.from(new Set(s.after.filter((x) => x !== st.id).concat(st.after)));
        });
        def.steps = def.steps.filter((s) => s.id !== st.id);
        S.sel = null;
      });
    } else if (sel.t === "edge") {
      mutate(() => { const s = stepById(sel.to); if (s) s.after = s.after.filter((x) => x !== sel.from); S.sel = null; });
    }
  }

  // ======================= Ablauf-Graph =======================
  const NW = 230, COLW = 300, ROWH = 150, X0 = 190, Y0 = 70;
  function levels() {
    const lv = {};
    const visit = (s, stack) => {
      if (lv[s.id] != null) return lv[s.id];
      if (stack.has(s.id)) return 0;
      stack.add(s.id);
      let l = 0;
      s.after.forEach((p) => { const ps = stepById(p); if (ps) l = Math.max(l, visit(ps, stack) + 1); });
      stack.delete(s.id);
      return (lv[s.id] = l);
    };
    def.steps.forEach((s) => visit(s, new Set()));
    return lv;
  }
  function autoLayout() {
    const lv = levels();
    const cols = {};
    def.steps.forEach((s) => { (cols[lv[s.id]] = cols[lv[s.id]] || []).push(s); });
    const maxRows = Math.max(1, ...Object.values(cols).map((c) => c.length));
    Object.entries(cols).forEach(([l, list]) => {
      // nach mittlerer Höhe der Vorgänger sortieren → weniger Kreuzungen
      list.sort((a, b) => avgPredY(a) - avgPredY(b));
      const off = ((maxRows - list.length) * ROWH) / 2;
      list.forEach((s, i) => { s.x = X0 + l * COLW; s.y = Y0 + off + i * ROWH; });
    });
  }
  function avgPredY(s) { const ys = s.after.map((p) => (stepById(p) || {}).y || 0); return ys.length ? ys.reduce((a, b) => a + b, 0) / ys.length : 0; }
  if (def.steps.length && def.steps.every((s) => !s.x && !s.y)) autoLayout();
  else def.steps.forEach((s) => { if (s.x < X0 - 40) s.x = (s.x || 0) + X0 - 40; });

  function assignLabel(s) {
    const a = s.assign || {};
    switch (a.type) {
      case "stelle": return { t: (stByKey[a.stelle] || {}).name || a.stelle || "– Stelle wählen –", c: (stByKey[a.stelle] || {}).color };
      case "feld": return { t: "Person aus „" + ((fieldByKey(a.field) || findChild(a.field) || {}).label || a.field || "?") + "“", c: "#64748b" };
      case "katalog": return { t: "Katalog: " + (a.field || "?") + "." + (a.attr || "?"), c: "#b45309" };
      case "antragsteller": return { t: "Antragsteller/in", c: "#64748b" };
      case "email": return { t: a.email || "–", c: "#64748b" };
    }
    return { t: "–", c: "#94a3b8" };
  }
  function findChild(k) { for (const f of allFields()) for (const c of f.fields || []) if (c.key === k) return c; return null; }

  let gsvg, gwrap;
  function renderGraph() {
    const tb = h("div", { class: "gtoolbar" },
      RO ? null : h("button", { onclick: () => mutate(autoLayout) }, "⇉ Automatisch anordnen"),
      h("span", { class: "sep" }),
      h("button", { onclick: () => { S.zoom = Math.max(0.5, S.zoom - 0.1); renderCanvas(); } }, "−"),
      h("button", { onclick: () => { S.zoom = 1; renderCanvas(); } }, Math.round(S.zoom * 100) + "%"),
      h("button", { onclick: () => { S.zoom = Math.min(1.5, S.zoom + 0.1); renderCanvas(); } }, "+"));
    canvasEl.append(tb);
    let maxX = 600, maxY = 400;
    def.steps.forEach((s) => { maxX = Math.max(maxX, s.x + NW + 260); maxY = Math.max(maxY, s.y + 220); });
    gwrap = h("div", { class: "gwrap", style: `width:${maxX}px;height:${maxY}px;transform:scale(${S.zoom});transform-origin:0 0` });
    gsvg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    gsvg.setAttribute("class", "gsvg");
    gsvg.setAttribute("width", maxX); gsvg.setAttribute("height", maxY);
    gsvg.innerHTML = '<defs><marker id="arr" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" class="arrow"/></marker></defs>';
    gwrap.append(gsvg);
    const roots = def.steps.filter((s) => !s.after.length);
    const startY = roots.length ? roots.reduce((a, s) => a + s.y, 0) / roots.length + 20 : Y0;
    gwrap.append(h("div", { class: "gstart", style: `left:20px;top:${startY}px`, id: "g-start" }, "▶ Antrag gestellt"));
    const hasSucc = new Set(def.steps.flatMap((s) => s.after));
    const ends = def.steps.filter((s) => !hasSucc.has(s.id));
    const endX = Math.max(X0, ...def.steps.map((s) => s.x + NW + 70));
    const endY = ends.length ? ends.reduce((a, s) => a + s.y, 0) / ends.length + 20 : Y0;
    gwrap.append(h("div", { class: "gstart gend", style: `left:${endX}px;top:${endY}px`, id: "g-end" }, "✓ Abschluss"));
    def.steps.forEach((s) => gwrap.append(node(s)));
    canvasEl.append(gwrap);
    if (!RO) {
      canvasEl.ondragover = (e) => e.preventDefault();
      canvasEl.ondrop = (e) => {
        e.preventDefault();
        let p; try { p = JSON.parse(e.dataTransfer.getData("text/aw") || "{}"); } catch (x) { return; }
        if (!p.step) return;
        const r = gwrap.getBoundingClientRect();
        addStep(p.step, (e.clientX - r.left) / S.zoom - NW / 2, (e.clientY - r.top) / S.zoom - 30);
      };
    }
    canvasEl.onclick = (e) => { if (e.target === canvasEl || e.target === gwrap || e.target === gsvg) { S.sel = null; render(); } };
    requestAnimationFrame(drawEdges);
  }

  function node(s) {
    const sel = S.sel && S.sel.t === "step" && S.sel.id === s.id;
    const a = assignLabel(s);
    const err = S.issues.some((i) => i.ref === "step:" + s.id && i.level === "error");
    const badges = [];
    if (s.foreach) badges.push(h("span", { class: "fe" }, "je " + ((fieldByKey(s.foreach) || {}).item_label || (fieldByKey(s.foreach) || {}).label || s.foreach)));
    if (s.when && s.when.rules && s.when.rules.length) badges.push(h("span", { class: "wh" }, "nur wenn …"));
    if ((s.scripts || []).length) badges.push(h("span", { class: "sc" }, "⌨ " + s.scripts.length));
    if ((s.checklist || []).length) badges.push(h("span", null, "☑ " + s.checklist.length));
    if (s.due_hours) badges.push(h("span", null, "⏱ " + (s.due_hours % 24 === 0 && s.due_hours >= 48 ? s.due_hours / 24 + " T" : s.due_hours + " h")));
    if (s.kind === "glpi" && s.glpi) badges.push(h("span", { class: "wh" }, s.glpi.action === "followup" ? "↳ Folgeeintrag" : (s.glpi.type === "change" ? "Change" : "Ticket") + (s.glpi.wait === "none" ? " · nur übergeben" : " · bis gelöst")));
    if (s.kind === "warten") badges.push(h("span", { class: "wh" }, "📅 " + (s.wait_field || "?") + (s.wait_offset ? (s.wait_offset > 0 ? " +" : " ") + s.wait_offset + " T" : "")));
    const el = h("div", { class: "gnode" + (sel ? " sel" : "") + (err ? " err" : "") + (s.foreach ? " stack" : ""), id: "n-" + s.id, "data-id": s.id,
      style: `left:${s.x}px;top:${s.y}px;--kc:${KCOL[s.kind] || "#64748b"}` },
      h("div", { class: "gh" }, h("span", { class: "gk" }, (skByKey[s.kind] || {}).icon || "•"), h("span", { class: "gt" }, s.title || "(ohne Titel)")),
      s.kind !== "warten" ? h("div", { class: "ga" }, h("span", { class: "gdot", style: "background:" + (a.c || "#94a3b8") }), a.t) : null,
      badges.length ? h("div", { class: "gb" }, badges) : null,
      RO ? null : h("span", { class: "gport", title: "Ziehen: danach folgt …" }));
    el.addEventListener("mouseenter", () => highlight(s.id, true));
    el.addEventListener("mouseleave", () => highlight(s.id, false));
    if (!RO) {
      el.addEventListener("mousedown", (e) => {
        if (e.button !== 0) return;
        if (e.target.classList.contains("gport")) return startConnect(e, s);
        const sx = e.clientX, sy = e.clientY, ox = s.x, oy = s.y;
        let moved = false, snap = JSON.stringify(def);
        const mv = (ev) => {
          const dx = (ev.clientX - sx) / S.zoom, dy = (ev.clientY - sy) / S.zoom;
          if (!moved && Math.abs(dx) + Math.abs(dy) < 4) return;
          moved = true;
          s.x = Math.max(0, Math.round((ox + dx) / 10) * 10); s.y = Math.max(0, Math.round((oy + dy) / 10) * 10);
          el.style.left = s.x + "px"; el.style.top = s.y + "px";
          drawEdges();
        };
        const up = () => {
          document.removeEventListener("mousemove", mv); document.removeEventListener("mouseup", up);
          if (moved) { S.undo.push(snap); S.redo = []; setDirty(); renderCanvas(); }
        };
        document.addEventListener("mousemove", mv); document.addEventListener("mouseup", up);
      });
    }
    el.addEventListener("click", (e) => { e.stopPropagation(); S.sel = { t: "step", id: s.id }; render({}); });
    return el;
  }

  function anc(id, acc = new Set()) { const s = stepById(id); (s ? s.after : []).forEach((p) => { if (!acc.has(p)) { acc.add(p); anc(p, acc); } }); return acc; }
  function desc(id, acc = new Set()) { def.steps.forEach((s) => { if (s.after.includes(id) && !acc.has(s.id)) { acc.add(s.id); desc(s.id, acc); } }); return acc; }
  function highlight(id, on) {
    if (!gwrap) return;
    const chain = on ? new Set([id, ...anc(id), ...desc(id)]) : null;
    gwrap.querySelectorAll(".gnode").forEach((n) => { n.classList.toggle("dim", !!chain && !chain.has(n.dataset.id)); });
    gsvg.querySelectorAll("path.e").forEach((p) => p.classList.toggle("hi", !!chain && chain.has(p.dataset.from) && chain.has(p.dataset.to)));
  }

  function anchor(el, side) {
    return { x: el.offsetLeft + (side === "r" ? el.offsetWidth : 0), y: el.offsetTop + el.offsetHeight / 2 };
  }
  function curve(a, b) {
    const dx = Math.max(40, Math.abs(b.x - a.x) / 2);
    return `M${a.x} ${a.y} C${a.x + dx} ${a.y}, ${b.x - dx} ${b.y}, ${b.x} ${b.y}`;
  }
  function drawEdges() {
    if (!gsvg) return;
    gsvg.querySelectorAll("path.e, path.tmp").forEach((p) => p.remove());
    const add = (fromEl, toEl, from, to, cls) => {
      if (!fromEl || !toEl) return;
      const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
      p.setAttribute("d", curve(anchor(fromEl, "r"), anchor(toEl, "l")));
      p.setAttribute("class", "e" + (cls || ""));
      p.setAttribute("marker-end", "url(#arr)");
      p.dataset.from = from; p.dataset.to = to;
      if (from && to && from !== "start" && to !== "end") {
        if (S.sel && S.sel.t === "edge" && S.sel.from === from && S.sel.to === to) p.classList.add("sel");
        p.addEventListener("click", (e) => { e.stopPropagation(); S.sel = { t: "edge", from, to }; render({}); });
      } else p.style.pointerEvents = "none";
      gsvg.append(p);
    };
    const start = gwrap.querySelector("#g-start"), end = gwrap.querySelector("#g-end");
    const hasSucc = new Set(def.steps.flatMap((s) => s.after));
    def.steps.forEach((s) => {
      const to = gwrap.querySelector("#n-" + CSS.escape(s.id));
      if (!s.after.length) add(start, to, "start", s.id, " start");
      s.after.forEach((p) => add(gwrap.querySelector("#n-" + CSS.escape(p)), to, p, s.id, s.when && s.when.rules && s.when.rules.length ? " cond" : ""));
      if (!hasSucc.has(s.id)) add(to, end, s.id, "end");
    });
  }

  function wouldCycle(stepId, depId) { return depId === stepId || anc(depId).has(stepId); }
  function startConnect(e, s) {
    e.preventDefault(); e.stopPropagation();
    const fromEl = gwrap.querySelector("#n-" + CSS.escape(s.id));
    const tmp = document.createElementNS("http://www.w3.org/2000/svg", "path");
    tmp.setAttribute("class", "tmp");
    gsvg.append(tmp);
    let target = null;
    const mv = (ev) => {
      const r = gwrap.getBoundingClientRect();
      const pt = { x: (ev.clientX - r.left) / S.zoom, y: (ev.clientY - r.top) / S.zoom };
      tmp.setAttribute("d", curve(anchor(fromEl, "r"), pt));
      const over = document.elementFromPoint(ev.clientX, ev.clientY);
      const n = over && over.closest(".gnode");
      gwrap.querySelectorAll(".droptarget").forEach((x) => x.classList.remove("droptarget"));
      target = n && n.dataset.id !== s.id && !wouldCycle(n.dataset.id, s.id) ? n.dataset.id : null;
      if (target) n.classList.add("droptarget");
    };
    const up = () => {
      document.removeEventListener("mousemove", mv); document.removeEventListener("mouseup", up);
      tmp.remove();
      gwrap.querySelectorAll(".droptarget").forEach((x) => x.classList.remove("droptarget"));
      if (target) {
        const t = stepById(target);
        if (t.after.includes(s.id)) { AW.toast("Verbindung besteht bereits."); return; }
        mutate(() => { t.after.push(s.id); S.sel = { t: "step", id: t.id }; });
        AW.toast("„" + t.title + "“ folgt jetzt auf „" + s.title + "“", "ok");
      }
    };
    document.addEventListener("mousemove", mv); document.addEventListener("mouseup", up);
  }

  function addStep(kind, x, y) {
    if (RO) return;
    mutate(() => {
      const ids = new Set(def.steps.map((s) => s.id));
      const base = { freigabe: "freigabe", aufgabe: "aufgabe", info: "info", warten: "termin", glpi: "glpi" }[kind];
      const prev = S.sel && S.sel.t === "step" ? stepById(S.sel.id) : null;
      const s = { id: uniqueKey(base, ids), title: { freigabe: "Neue Freigabe", aufgabe: "Neue Aufgabe", info: "Benachrichtigung", warten: "Termin abwarten", glpi: "Ticket in GLPI" }[kind], kind,
        after: prev && x == null ? [prev.id] : [], assign: kind === "warten" ? {} : { type: "stelle", stelle: (STELLEN[0] || {}).key || "" }, checklist: [], scripts: [], outputs: [] };
      if (kind === "warten") s.wait_field = (allFields().find((f) => f.type === "date") || {}).key || "";
      if (kind === "glpi") s.glpi = { type: "ticket", ticket_type: "2", name: "{{.antrag.titel}} ({{.antrag.nummer}})", content: GL.content, conn: (GL.conns[0] || {}).key || "" };
      s.x = x != null ? Math.max(0, Math.round(x)) : prev ? prev.x + COLW : X0 + 20;
      s.y = y != null ? Math.max(0, Math.round(y)) : prev ? prev.y : Y0 + def.steps.length * 20;
      def.steps.push(s);
      S.sel = { t: "step", id: s.id };
    });
  }

  // ======================= Inspektor =======================
  const fieldRow = (label, input, hint) => h("label", null, label, input, hint ? h("small", null, hint) : null);
  function textIn(obj, k, attrs, after) {
    return h("input", Object.assign({ value: obj[k] == null ? "" : obj[k], disabled: RO, oninput: (e) => live(() => { obj[k] = e.target.value; if (after) after(e.target.value); }) }, attrs || {}));
  }
  function numIn(obj, k, attrs) {
    return h("input", Object.assign({ type: "number", value: obj[k] || "", disabled: RO, oninput: (e) => live(() => { const n = parseInt(e.target.value, 10); if (isNaN(n) || n === 0) delete obj[k]; else obj[k] = n; }) }, attrs || {}));
  }
  function areaIn(obj, k, rows, ph) {
    return h("textarea", { rows: rows || 2, placeholder: ph || "", disabled: RO, value: obj[k] || "", oninput: (e) => live(() => { obj[k] = e.target.value; }) });
  }
  function selectIn(obj, k, options, after, structural) {
    const s = h("select", { disabled: RO, onchange: (e) => (structural === false ? live : (fn) => mutate(fn, { keepInspector: false }))(() => { obj[k] = e.target.value; if (!obj[k]) delete obj[k]; if (after) after(e.target.value); }) },
      options.map((o) => (o.group ? h("optgroup", { label: o.group }, o.items.map((x) => h("option", { value: x.v }, x.l))) : h("option", { value: o.v }, o.l))));
    s.value = obj[k] || "";
    return s;
  }
  function toggle(obj, k, label, hint, structural) {
    return h("label", { class: "chk" }, h("input", { type: "checkbox", checked: !!obj[k], disabled: RO, onchange: (e) => (structural ? mutate : live)(() => { if (e.target.checked) obj[k] = true; else delete obj[k]; }) }), label, hint ? h("small", { class: "muted", style: "margin-left:4px" }, hint) : null);
  }
  function sec(title, ...kids) { return h("div", { class: "insp-sec" }, h("h4", null, title), kids); }

  function renderInspector() {
    inspEl.innerHTML = "";
    if (S.tab === "form") {
      if (S.sel && S.sel.t === "field") { const r = findField(S.sel.u); if (r) return inspField(r); }
      if (S.sel && S.sel.t === "section") { const s = secByU(S.sel.u); if (s) return inspSection(s); }
      return inspNone("Formular", "Feld oder Eingabegruppe anklicken, um sie zu bearbeiten. Neue Felder aus der linken Leiste ziehen.",
        [["Eingabegruppen", def.form.sections.length], ["Felder", allFields().length], ["Pflichtfelder", allFields().filter((f) => f.required).length], ["Wiederholbare Gruppen", allFields().filter((f) => f.type === "group").length]]);
    }
    if (S.tab === "flow") {
      if (S.sel && S.sel.t === "step") { const s = stepById(S.sel.id); if (s) return inspStep(s); }
      if (S.sel && S.sel.t === "edge") return inspEdge(S.sel);
      const lv = levels();
      const par = Object.values(def.steps.reduce((a, s) => { a[lv[s.id]] = (a[lv[s.id]] || 0) + 1; return a; }, {})).filter((n) => n > 1).length;
      return inspNone("Ablauf", "Schritt anklicken, um Zuständigkeit, Bedingungen, Skripte und Mails festzulegen. Schrittarten aus der linken Leiste auf die Fläche ziehen.",
        [["Schritte", def.steps.length], ["Freigaben", def.steps.filter((s) => s.kind === "freigabe").length], ["Aufgaben mit Skripten", def.steps.filter((s) => (s.scripts || []).length).length], ["Ebenen mit Parallelität", par]]);
    }
    if (S.tab === "settings") return inspNone("Einstellungen", "Name, Darstellung, Nummernkreis, berechnete Werte und Abschlussmail.", []);
  }
  function inspNone(title, text, stats) {
    inspEl.append(h("div", { class: "insp-h" }, h("h3", null, title)), h("p", { class: "small muted" }, text));
    if (stats.length) inspEl.append(h("dl", { class: "kv" }, stats.map(([k, v]) => [h("dt", null, k), h("dd", null, h("b", null, v))])));
    if (!RO) inspEl.append(h("div", { class: "pal-tip" }, "Tastatur: ", h("b", null, "Strg+S"), " speichern · ", h("b", null, "Strg+Z"), " rückgängig · ", h("b", null, "Entf"), " Auswahl löschen · ", h("b", null, "Esc"), " Auswahl aufheben"));
    const iss = S.issues.slice(0, 6);
    if (iss.length) inspEl.append(sec("Prüfung", h("ul", { class: "issues" }, iss.map((i) => h("li", { class: i.level, onclick: () => gotoIssue(i) }, i.level === "error" ? "✕" : "!", i.msg)))));
  }

  function inspSection(s) {
    inspEl.append(h("div", { class: "insp-h" }, h("span", { class: "kindchip" }, "Eingabegruppe"), h("h3", null, s.title)),
      fieldRow("Überschrift", textIn(s, "title")),
      h("div", { class: "row2" }, fieldRow("Symbol", textIn(s, "icon", { maxlength: 4 }), "Emoji"), fieldRow("ID", textIn(s, "id"))),
      fieldRow("Beschreibung", areaIn(s, "description", 2)),
      sec("Nur anzeigen, wenn …", condEditor(s, "show", null, "Ohne Bedingung immer sichtbar.")));
  }

  function inspField(r) {
    const f = r.f;
    const ft = ftByKey[f.type] || {};
    let keyTouched = f.label && f.key !== slug(f.label);
    inspEl.append(h("div", { class: "insp-h" }, h("span", { class: "kindchip" }, ft.icon + " " + ft.label), h("h3", null, f.label),
      RO ? null : h("button", { class: "btn tiny ghost", onclick: () => dupField(f._u) }, "⧉ Duplizieren")));
    issuesFor("field:" + (r.parent ? r.parent.key + "." : "") + f.key).forEach((i) => inspEl.append(h("div", { class: "hint " + (i.level === "error" ? "err" : "warn") }, i.msg)));
    if (f.type === "info") {
      inspEl.append(fieldRow("Text", areaIn(f, "label", 4)),
        fieldRow("Darstellung", selectIn(f, "tone", [{ v: "info", l: "Information" }, { v: "warn", l: "Warnung" }, { v: "ok", l: "Erfolg" }], null, false)),
        sec("Nur anzeigen, wenn …", condEditor(f, "show", r.parent)));
      return;
    }
    inspEl.append(
      fieldRow("Beschriftung", textIn(f, "label", {}, (v) => { if (!keyTouched && !r.parent) { f.key = uniqueKey(slug(v), new Set(allFields().filter((x) => x !== f).map((x) => x.key))); const k = inspEl.querySelector("[data-key]"); if (k) k.value = f.key; } })),
      h("div", { class: "row2" },
        fieldRow("Schlüssel", textIn(f, "key", { "data-key": "", class: "mono", oninput: () => { keyTouched = true; } }), "für Skripte: {{." + (r.parent ? "item." : "") + f.key + "}}"),
        fieldRow("Feldart", selectIn(f, "type", FT.filter((t) => !r.parent || t.key !== "group").map((t) => ({ v: t.key, l: t.icon + " " + t.label })), (v) => {
          if ((v === "select" || v === "radio" || v === "checkboxes") && !f.options) f.options = [{ value: "a", label: "Option A" }];
          if (v === "catalog" && !f.catalog) f.catalog = (CATS[0] || {}).key;
          if (v === "group" && !f.fields) f.fields = [];
        }))),
      h("div", { class: "row2" }, toggle(f, "required", "Pflichtfeld", null, true),
        f.type !== "group" ? h("div", null, h("div", { class: "seg", style: "margin-bottom:12px" }, [["full", "ganz"], ["half", "½"], ["third", "⅓"]].map(([v, l]) =>
          h("button", { class: (f.width || "full") === v ? "on" : "", disabled: RO, onclick: () => mutate(() => { if (v === "full") delete f.width; else f.width = v; }) }, l)))) : null),
      fieldRow("Hilfetext", textIn(f, "help", { placeholder: "erscheint unter dem Feld" })));
    if (["text", "textarea", "number", "email", "phone", "user"].includes(f.type)) inspEl.append(fieldRow("Platzhalter", textIn(f, "placeholder")));
    if (["text", "number", "date", "select", "radio", "textarea", "checkbox"].includes(f.type)) inspEl.append(fieldRow("Vorgabewert", textIn(f, "default", { placeholder: f.type === "checkbox" ? "true = angehakt" : "" })));
    if (["select", "radio", "checkboxes"].includes(f.type)) inspEl.append(sec("Auswahlwerte", optionsEditor(f)));
    if (f.type === "catalog") {
      const c = catByKey[f.catalog];
      inspEl.append(sec("Katalog",
        fieldRow("Katalog", selectIn(f, "catalog", CATS.map((x) => ({ v: x.key, l: (x.icon || "") + " " + x.name + " (" + x.entries.length + ")" })))),
        toggle(f, "multi", "Mehrfachauswahl", "(Liste – für „je Eintrag“ nutzbar)", true),
        c ? h("p", { class: "small muted" }, "Attribute in Skripten: ", c.attrs.map((a) => h("code", { style: "margin:2px", title: a.label }, "." + f.key + "." + a.key)), " · Pflege unter ", h("a", { href: "/stammdaten", target: "_blank" }, "Stammdaten"), ".") : null));
    }
    if (["text", "number", "phone"].includes(f.type)) inspEl.append(sec("Prüfung", fieldRow("Muster (regulärer Ausdruck)", textIn(f, "pattern", { class: "mono", placeholder: "^\\d{5,8}$" })), fieldRow("Meldung bei Fehler", textIn(f, "pattern_msg"))));
    if (f.type === "group") {
      inspEl.append(sec("Wiederholung",
        h("div", { class: "row2" }, fieldRow("Eintrag heißt", textIn(f, "item_label", { placeholder: "Programm" })), fieldRow("Schaltfläche", textIn(f, "add_label", { placeholder: "Programm hinzufügen" }))),
        h("div", { class: "row2" }, fieldRow("Mindestens", numIn(f, "min")), fieldRow("Höchstens", numIn(f, "max"))),
        h("p", { class: "small muted" }, "Unterfelder per Drag & Drop in die Gruppe ziehen. In Skripten: ", h("code", null, "{{range ." + f.key + "}}{{.feld}}{{end}}"), " – für eine Aufgabe je Eintrag im Schritt „je Eintrag“ wählen.")));
    }
    if (f.type !== "user" && f.type !== "group") {
      const users = allFields().filter((x) => x.type === "user");
      if (users.length) {
        const pf = f.prefill || { from: "", attr: "" };
        inspEl.append(sec("Vorbelegen aus Person",
          h("div", { class: "row2" },
            fieldRow("Personenfeld", h("select", { disabled: RO, onchange: (e) => mutate(() => { if (e.target.value) f.prefill = { from: e.target.value, attr: pf.attr || "title" }; else delete f.prefill; }) },
              h("option", { value: "" }, "– keine –"), users.map((u) => h("option", { value: u.key, selected: pf.from === u.key }, u.label)))),
            fieldRow("Attribut", h("select", { disabled: RO || !f.prefill, onchange: (e) => mutate(() => { f.prefill.attr = e.target.value; }) },
              [["title", "Funktion"], ["department", "Abteilung"], ["phone", "Telefon"], ["office", "Büro"], ["mail", "Mail"], ["given", "Vorname"], ["surname", "Nachname"], ["sam", "Anmeldename"]].map(([v, l]) => h("option", { value: v, selected: pf.attr === v }, l)))))));
      }
    }
    inspEl.append(sec("Nur anzeigen, wenn …", condEditor(f, "show", r.parent, "Ausgeblendete Felder sind nie Pflicht und werden nicht gespeichert.")));
  }

  function optionsEditor(f) {
    const box = h("div");
    (f.options || []).forEach((o, i) => box.append(h("div", { class: "opt-row" },
      h("input", { value: o.label, placeholder: "Beschriftung", disabled: RO, oninput: (e) => live(() => { const auto = !o.value || o.value === slug(o.label); o.label = e.target.value; if (auto) { o.value = slug(e.target.value); const v = e.target.nextSibling; if (v) v.value = o.value; } }) }),
      h("input", { value: o.value, class: "mono", placeholder: "Wert", style: "max-width:110px", disabled: RO, oninput: (e) => live(() => { o.value = e.target.value; }) }),
      RO ? null : h("button", { class: "x", onclick: () => mutate(() => f.options.splice(i, 1)) }, "×"))));
    if (!RO) box.append(h("button", { class: "btn tiny", onclick: () => mutate(() => { f.options = f.options || []; f.options.push({ value: "opt" + (f.options.length + 1), label: "Option " + (f.options.length + 1) }); }) }, "+ Option"),
      h("button", { class: "btn tiny ghost", style: "margin-left:6px", onclick: () => {
        const t = prompt("Eine Option pro Zeile einfügen:");
        if (t) mutate(() => { f.options = (f.options || []).concat(t.split(/\n/).map((x) => x.trim()).filter(Boolean).map((x) => ({ value: slug(x), label: x }))); });
      } }, "Liste einfügen …"));
    return box;
  }

  // ---------- Bedingungen ----------
  function condFields(parent, step) {
    const out = [];
    const add = (group, items) => { if (items.length) out.push({ group, items }); };
    const top = [];
    allFields().forEach((f) => {
      if (f.type === "info") return;
      top.push({ v: f.key, l: f.label, f });
      if (f.type === "catalog" && !f.multi && catByKey[f.catalog]) catByKey[f.catalog].attrs.forEach((a) => top.push({ v: f.key + "." + a.key, l: f.label + " → " + a.label, f: null }));
      if (f.type === "user") [["department", "Abteilung"], ["title", "Funktion"]].forEach(([k, l]) => top.push({ v: f.key + "." + k, l: f.label + " → " + l }));
    });
    let itemFields = [];
    const grp = parent || (step && step.foreach ? fieldByKey(step.foreach) : null);
    if (grp && grp.type === "group") {
      (grp.fields || []).forEach((c) => {
        if (c.type === "info") return;
        itemFields.push({ v: "item." + c.key, l: "Eintrag: " + c.label, f: c });
        if (c.type === "catalog" && catByKey[c.catalog]) catByKey[c.catalog].attrs.forEach((a) => itemFields.push({ v: "item." + c.key + "." + a.key, l: "Eintrag: " + c.label + " → " + a.label }));
      });
    } else if (grp && grp.type === "catalog" && catByKey[grp.catalog]) {
      itemFields.push({ v: "item.key", l: "Eintrag (Schlüssel)" });
      catByKey[grp.catalog].attrs.forEach((a) => itemFields.push({ v: "item." + a.key, l: "Eintrag → " + a.label }));
    }
    add("Aktueller Eintrag", itemFields);
    add("Antrag", top);
    if (def.computed.length) add("Berechnete Werte", def.computed.map((c) => ({ v: "c." + c.key, l: c.label || c.key })));
    return out;
  }
  function fieldOfPath(p, parent) {
    if (p.startsWith("item.") && parent) return (parent.fields || []).find((c) => c.key === p.slice(5));
    return fieldByKey(p) || (parent ? (parent.fields || []).find((c) => c.key === p) : null);
  }
  function condEditor(obj, k, parent, hint, step) {
    const box = h("div", { class: "condbox" });
    const c = obj[k] || { mode: "all", rules: [] };
    const fields = condFields(parent, step);
    const flat = fields.flatMap((g) => g.items);
    const redraw = () => { box.replaceWith(condEditor(obj, k, parent, hint, step)); };
    const commit = (fn) => mutate(() => { if (!obj[k]) obj[k] = c; fn(); if (!obj[k].rules.length) delete obj[k]; }, { keepInspector: true });
    if (c.rules.length > 1) box.append(h("div", { class: "mode" }, "Erfüllt sein müssen", h("select", { disabled: RO, onchange: (e) => { commit(() => { c.mode = e.target.value; }); redraw(); } },
      h("option", { value: "all", selected: c.mode !== "any" }, "alle Regeln (und)"), h("option", { value: "any", selected: c.mode === "any" }, "eine Regel (oder)"))));
    c.rules.forEach((r, i) => {
      const op = OPS.find((o) => o.Key === r.op) || OPS[0];
      const fd = fieldOfPath(r.field, parent) || (flat.find((x) => x.v === r.field) || {}).f;
      let val = null;
      if (op.Values > 0) {
        const opts = fd && fd.options ? fd.options.map((o) => ({ v: o.value, l: o.label })) : fd && fd.type === "catalog" && catByKey[fd.catalog] ? catByKey[fd.catalog].entries.map((e) => ({ v: e.key, l: e.label })) : null;
        if (r.op === "same" || r.op === "changed") val = h("select", { disabled: RO, onchange: (e) => commit(() => { r.values = [e.target.value]; }) }, flat.map((x) => h("option", { value: x.v, selected: (r.values || [])[0] === x.v }, x.l)));
        else if (opts && op.Values === 2) {
          val = h("div", { class: "chips", style: "flex:1" }, opts.map((o) => h("span", { class: "chip" + ((r.values || []).includes(o.v) ? " on" : ""), onclick: () => { commit(() => { r.values = (r.values || []).includes(o.v) ? r.values.filter((x) => x !== o.v) : (r.values || []).concat(o.v); }); redraw(); } }, o.l)));
        } else if (opts && fd.type !== "checkboxes" || opts && r.op !== "contains") {
          val = h("select", { disabled: RO, onchange: (e) => commit(() => { r.values = [e.target.value]; }) }, h("option", { value: "" }, "–"), opts.map((o) => h("option", { value: o.v, selected: (r.values || [])[0] === o.v }, o.l)));
        } else val = h("input", { value: (r.values || []).join(", "), disabled: RO, placeholder: "Wert", oninput: (e) => live(() => { r.values = e.target.value.split(",").map((x) => x.trim()).filter(Boolean); }) });
      }
      box.append(h("div", { class: "rule-row" },
        h("select", { disabled: RO, onchange: (e) => { commit(() => { r.field = e.target.value; r.values = []; }); redraw(); } },
          !flat.some((x) => x.v === r.field) ? h("option", { value: r.field }, r.field) : null,
          fields.map((g) => h("optgroup", { label: g.group }, g.items.map((x) => h("option", { value: x.v, selected: x.v === r.field }, x.l))))),
        h("select", { disabled: RO, style: "max-width:130px", onchange: (e) => { commit(() => { r.op = e.target.value; }); redraw(); } }, OPS.map((o) => h("option", { value: o.Key, selected: o.Key === r.op }, o.Label))),
        RO ? null : h("button", { class: "x", onclick: () => { commit(() => { c.rules.splice(i, 1); }); redraw(); } }, "×")));
      if (val) box.append(h("div", { class: "rule-row", style: "padding-left:12px" }, "↳ ", val));
    });
    if (!RO) box.append(h("button", { class: "btn tiny", onclick: () => { if (!flat.length) return; commit(() => { c.rules.push({ field: flat[0].v, op: "eq", values: [] }); }); redraw(); } }, "+ Regel"));
    if (hint && !c.rules.length) box.append(h("div", { class: "small muted", style: "margin-top:6px" }, hint));
    return box;
  }

  // ---------- Schritt-Inspektor ----------
  function userFieldOpts(step) {
    const out = [];
    allFields().forEach((f) => { if (f.type === "user") out.push({ v: f.key, l: f.label }); });
    const g = step.foreach ? fieldByKey(step.foreach) : null;
    if (g && g.type === "group") (g.fields || []).forEach((c) => { if (c.type === "user") out.push({ v: c.key, l: "Eintrag: " + c.label }); });
    return out;
  }
  function catFieldOpts(step) {
    const out = [];
    const g = step.foreach ? fieldByKey(step.foreach) : null;
    if (g && g.type === "group") (g.fields || []).forEach((c) => { if (c.type === "catalog") out.push({ v: c.key, l: "Eintrag: " + c.label, c: c.catalog }); });
    allFields().forEach((f) => { if (f.type === "catalog") out.push({ v: f.key, l: f.label + (f.multi ? (step.foreach === f.key ? " (aktueller Eintrag)" : " (alle)") : ""), c: f.catalog }); });
    return out;
  }
  function inspStep(s) {
    const k = skByKey[s.kind] || {};
    inspEl.append(h("div", { class: "insp-h" }, h("span", { class: "kindchip k-" + s.kind }, k.icon + " " + k.label), h("h3", null, s.title),
      RO ? null : h("button", { class: "btn tiny ghost", onclick: () => dupStep(s) }, "⧉"),
      RO ? null : h("button", { class: "btn tiny ghost", onclick: removeSel }, "🗑")));
    issuesFor("step:" + s.id).forEach((i) => inspEl.append(h("div", { class: "hint " + (i.level === "error" ? "err" : "warn") }, i.msg)));
    inspEl.append(
      fieldRow("Titel", textIn(s, "title")),
      h("div", { class: "row2" },
        fieldRow("ID", textIn(s, "id", { class: "mono", onchange: (e) => {
          const old = s.id, nw = e.target.value.trim();
          mutate(() => { def.steps.forEach((x) => { x.after = x.after.map((a) => (a === old ? nw : a)); }); S.sel = { t: "step", id: nw }; });
        } }), "Ergebnisse: {{.out." + s.id + ".…}}"),
        fieldRow("Art", selectIn(s, "kind", SK.map((x) => ({ v: x.key, l: x.icon + " " + x.label }))))));
    // Zuständigkeit
    if (s.kind !== "warten") {
      const a = s.assign = s.assign || { type: "stelle" };
      const box = h("div");
      box.append(fieldRow("Zuständig", selectIn(a, "type", [{ v: "stelle", l: "Stelle" }, { v: "feld", l: "Person aus dem Antrag" }, { v: "katalog", l: "Aus Katalog (z. B. Verfahrensverantwortung)" }, { v: "antragsteller", l: "Antragsteller/in" }, { v: "email", l: "Feste Adresse / mehrere Stellen" }])));
      if (a.type === "stelle") box.append(fieldRow("Stelle", selectIn(a, "stelle", [{ v: "", l: "– wählen –" }].concat(STELLEN.map((x) => ({ v: x.key, l: x.name })))),
        a.stelle && stByKey[a.stelle] ? (stByKey[a.stelle].email || "") + " · " + (stByKey[a.stelle].members || []).length + " Mitglieder" : ""));
      if (a.type === "feld") box.append(fieldRow("Personenfeld", selectIn(a, "field", [{ v: "", l: "– wählen –" }].concat(userFieldOpts(s)))));
      if (a.type === "katalog") {
        const opts = catFieldOpts(s);
        const cur = opts.find((o) => o.v === a.field);
        const cat = cur ? catByKey[cur.c] : null;
        box.append(h("div", { class: "row2" },
          fieldRow("Katalogfeld", selectIn(a, "field", [{ v: "", l: "– wählen –" }].concat(opts))),
          fieldRow("Attribut", selectIn(a, "attr", [{ v: "", l: "– wählen –" }].concat(cat ? cat.attrs.map((x) => ({ v: x.key, l: x.label })) : [])))),
          h("small", { class: "muted" }, "Das Attribut enthält Stelle, Mail oder Anmeldename – z. B. die Verfahrensverantwortung je Programm."));
      }
      if (a.type === "email") box.append(fieldRow("Empfänger", textIn(a, "email", { placeholder: "it_benutzer; jemand@stadt.de" }), "Stellen-Schlüssel oder Mail-Adressen, mit ; getrennt"));
      inspEl.append(sec(s.kind === "glpi" ? "Zuständig, falls GLPI scheitert" : "Zuständigkeit", box,
        s.kind === "glpi" ? h("small", { class: "muted" }, "Ist GLPI nicht erreichbar oder wird das Ticket abgelehnt, wird der Schritt zur Aufgabe für diese Stelle.") : null));
    } else {
      inspEl.append(sec("Termin",
        h("div", { class: "row2" },
          fieldRow("Datumsfeld", selectIn(s, "wait_field", [{ v: "", l: "– wählen –" }].concat(allFields().filter((f) => f.type === "date").map((f) => ({ v: f.key, l: f.label }))))),
          fieldRow("Versatz (Tage)", h("input", { type: "number", value: s.wait_offset || 0, disabled: RO, oninput: (e) => live(() => { const n = parseInt(e.target.value, 10) || 0; if (n) s.wait_offset = n; else delete s.wait_offset; }) }), "−3 = drei Tage vorher")),
        h("small", { class: "muted" }, "Der Ablauf wartet bis 7:00 Uhr am Stichtag und läuft dann automatisch weiter.")));
    }
    if (s.kind === "glpi") inspGLPI(s);
    // Ablauf
    const others = def.steps.filter((x) => x.id !== s.id);
    const lists = allFields().filter((f) => f.type === "group" || (f.multi && f.type === "catalog") || f.type === "checkboxes");
    inspEl.append(sec("Ablauf",
      h("label", null, "Startet nach", h("div", { class: "chips", style: "margin-top:6px" },
        others.map((o) => { const on = s.after.includes(o.id); const cyc = !on && wouldCycle(s.id, o.id) ; return h("span", { class: "chip" + (on ? " on" : ""), style: cyc ? "opacity:.35;cursor:not-allowed" : null, title: cyc ? "würde einen Kreis bilden" : "",
          onclick: () => { if (RO || cyc) return; mutate(() => { s.after = on ? s.after.filter((x) => x !== o.id) : s.after.concat(o.id); }, { keepInspector: false }); } }, o.title); })),
        h("small", null, s.after.length ? "wartet auf alle gewählten Schritte" : "startet sofort nach Antragstellung")),
      fieldRow("Je Eintrag ausführen", selectIn(s, "foreach", [{ v: "", l: "– einmal –" }].concat(lists.map((f) => ({ v: f.key, l: "je " + (f.item_label || f.label) + " (" + f.key + ")" })))),
        "Erzeugt einen eigenen Schritt je Eintrag – parallel, mit {{.item}} in Texten und Skripten."),
      h("div", { class: "row2" }, fieldRow("Frist (Stunden)", numIn(s, "due_hours", { placeholder: "keine" }), "48 = 2 Tage; danach Erinnerung"), h("div")),
      h("label", null, "Nur ausführen, wenn …", condEditor(s, "when", s.foreach ? fieldByKey(s.foreach) : null, "Sonst „entfällt“ der Schritt; Nachfolger laufen trotzdem weiter.", s))));
    // Inhalte
    if (s.kind !== "warten") {
      inspEl.append(sec("Inhalt",
        fieldRow("Warum? (Begründung)", tplEditor(s, "why", s, true)),
        fieldRow(s.kind === "info" ? "Zusatztext" : "Anleitung", tplEditor(s, "instructions", s, true), "Platzhalter wie {{.vorname}} werden mit den Antragsdaten gefüllt; **fett** möglich.")));
    }
    if (s.kind === "aufgabe" || s.kind === "freigabe") {
      inspEl.append(sec("Checkliste", listEditor(s, "checklist", "Punkt …")));
    }
    if (s.kind === "aufgabe") {
      inspEl.append(sec("Skripte", scriptsEditor(s)));
      inspEl.append(sec("Ergebnisfelder", outputsEditor(s)));
    }
    if (s.kind === "glpi" && (s.glpi || {}).action !== "followup") {
      inspEl.append(sec("Rückmeldefelder", h("p", { class: "small muted" }, "Angaben, die aus GLPI zurückkommen – über den Rückmeldelink oder als „Bezeichnung: Wert“ in Lösung/Folgeeintrag. Danach als {{.out." + s.id + ".schluessel}} nutzbar."), outputsEditor(s)));
    }
    if (s.kind !== "warten") {
      s.mail = s.mail || {};
      inspEl.append(h("details", { class: "insp-sec", open: !!(s.mail.subject || s.mail.body) || s.kind === "info" }, h("summary", { class: "small", style: "cursor:pointer;font-weight:700" }, "✉ Mail an Zuständige " + (s.mail.subject || s.mail.body ? "(eigener Text)" : "(Standardtext)")),
        h("p", { class: "small muted" }, "Leer = Standardtext mit Aufgabe, Anleitung, Checkliste, Antragsdaten (bei Freigaben) und Link zum Schritt. Zusätzlich verfügbar: {{.link}}, {{.schritt.titel}}, {{.schritt.frist}}."),
        fieldRow("Betreff", tplEditor(s.mail, "subject", s, true)), fieldRow("Text", tplEditor(s.mail, "body", s))));
    }
  }
  // ---------- GLPI ----------
  function glpiLookup(kind, onPick, conn) {
    const m = modal("In GLPI nachschlagen");
    const q = h("input", { type: "search", placeholder: "Name (leer = alle)" });
    const ksel = h("select", { style: "width:auto" }, GL.lookups.map((l) => h("option", { value: l.kind }, l.label)));
    ksel.value = kind;
    const list = h("div", { class: "varlist" });
    const run = async () => {
      list.innerHTML = ""; list.append(h("p", { class: "muted small" }, "Suche …"));
      try {
        const r = await AW.api("POST", "/api/glpi/nachschlagen", { conn: { key: conn || "" }, kind: ksel.value, q: q.value });
        list.innerHTML = "";
        if (!r.results.length) list.append(h("p", { class: "muted small" }, "Nichts gefunden."));
        r.results.forEach((x) => list.append(h("button", { type: "button", onclick: () => { onPick(x); m.close(); } }, h("span", null, x.full || x.name), h("code", null, "#" + x.id))));
      } catch (e) { list.innerHTML = ""; list.append(h("div", { class: "hint err" }, e.message)); }
    };
    q.addEventListener("keydown", (e) => { if (e.key === "Enter") run(); });
    m.body.append(h("div", { style: "display:flex;gap:8px" }, ksel, q, h("button", { class: "btn", onclick: run }, "Suchen")),
      h("p", { class: "small muted" }, "Übernommen wird der Name (lesbar, wird beim Senden aufgelöst). Mit gedrückter Umschalt-Taste: die ID."), list);
    list.style.maxHeight = "55vh";
    run();
    q.focus();
  }
  function refIn(obj, k, kind, ph, conn) {
    const inp = textIn(obj, k, { placeholder: ph || "ID oder Name" });
    return h("div", { class: "refin" }, inp, RO ? null : h("button", { type: "button", class: "btn tiny ghost", title: "In GLPI nachschlagen", onclick: () => glpiLookup(kind, (x) => {
      const v = window.event && window.event.shiftKey ? String(x.id) : (x.full || x.name);
      inp.value = v; inp.dispatchEvent(new Event("input"));
    }, conn()) }, "🔍"));
  }
  function statusChips(g, k, type) {
    const all = GL.status[type] || [];
    const def = (k === "done_status" ? GL.done : GL.fail)[type] || [];
    const cur = g[k] && g[k].length ? g[k] : def;
    return h("div", { class: "chips" }, all.map(([id, name]) => {
      const on = cur.includes(id);
      return h("span", { class: "chip" + (on ? " on" : ""), onclick: () => { if (RO) return; mutate(() => { const set = new Set(cur); if (on) set.delete(id); else set.add(id); g[k] = all.map((x) => x[0]).filter((x) => set.has(x)); }, { keepInspector: false }); } }, name);
    }), g[k] && g[k].length ? h("span", { class: "chip", title: "Vorgabe wiederherstellen", onclick: () => mutate(() => { delete g[k]; }, { keepInspector: false }) }, "↺ Vorgabe") : null);
  }
  function inspGLPI(s) {
    const g = s.glpi = s.glpi || { type: "ticket" };
    const conn = () => g.conn || "";
    const isChange = g.type === "change", isFU = g.action === "followup";
    const seg = (k, opts, def0) => h("div", { class: "seg" }, opts.map(([v, l]) => h("button", { type: "button", disabled: RO, class: (g[k] || def0) === v ? "on" : "", onclick: () => mutate(() => { if (v === def0) delete g[k]; else g[k] = v; }, { keepInspector: false }) }, l)));
    const num = (k, label) => fieldRow(label, selectIn(g, k, [{ v: "", l: "– GLPI-Vorgabe –" }, { v: "1", l: "1 · sehr niedrig" }, { v: "2", l: "2 · niedrig" }, { v: "3", l: "3 · mittel" }, { v: "4", l: "4 · hoch" }, { v: "5", l: "5 · sehr hoch" }], null, false));
    const connSel = selectIn(g, "conn", [{ v: "", l: GL.conns.length ? "– erste Verbindung –" : "– keine eingerichtet –" }].concat(GL.conns.map((c) => ({ v: c.key, l: c.name + " (" + c.key + ")" }))), null, false);
    const prevBox = h("div");
    const head = sec("GLPI",
      !GL.conns.length ? h("div", { class: "hint warn" }, "Noch keine GLPI-Verbindung – unter Einstellungen → Schnittstellen anlegen.") : null,
      h("div", { class: "row2" }, fieldRow("Verbindung", connSel), fieldRow("Aktion", seg("action", [["create", "Neu anlegen"], ["followup", "Folgeeintrag"]], "create"))),
      isFU ? null : fieldRow("Art", seg("type", [["ticket", "🎫 Ticket"], ["change", "⚙ Change"]], "ticket")),
      RO ? null : h("div", { class: "code-tools" }, h("button", { type: "button", class: "btn tiny", onclick: async () => {
        prevBox.innerHTML = ""; prevBox.append(h("div", { class: "small muted" }, "wird berechnet …"));
        try {
          const r = await AW.api("POST", "/api/glpi/vorschau", { def: clean(def), data: testData(), step: s.id, item: 0 });
          prevBox.innerHTML = "";
          (r.errors || []).forEach((e) => prevBox.append(h("div", { class: "hint err" }, e)));
          const pre = h("pre", { class: "prevbox code" }, JSON.stringify(r.requests, null, 2));
          prevBox.append(h("div", { class: "small muted" }, "So geht es an GLPI (Testdaten; ‹Namen› werden beim Senden in IDs aufgelöst):"), pre);
        } catch (e) { prevBox.innerHTML = ""; prevBox.append(h("div", { class: "hint err" }, e.message)); }
      } }, "▶ Vorschau der Übergabe")), prevBox);
    inspEl.append(head);
    if (isFU) {
      const gsteps = def.steps.filter((x) => x.id !== s.id && x.kind === "glpi" && (x.glpi || {}).action !== "followup");
      inspEl.append(sec("Folgeeintrag",
        fieldRow("Zu Ticket/Change aus", selectIn(g, "ref", [{ v: "", l: "– Schritt wählen –" }].concat(gsteps.map((x) => ({ v: x.id, l: x.title }))))),
        fieldRow("Text", tplEditor(g, "content", s)),
        toggle(g, "private", "privat", "nur für Techniker sichtbar")));
      return;
    }
    inspEl.append(sec(isChange ? "Change" : "Ticket",
      fieldRow("Titel", tplEditor(g, "name", s, true)),
      fieldRow("Beschreibung", tplEditor(g, "content", s), "Text mit Zeilen und Links oder HTML. {{.glpi.rueckmeldung}} = Link, über den GLPI-Techniker/innen „erledigt“ samt Angaben melden."),
      isChange ? null : h("div", { class: "row2" },
        fieldRow("Typ", selectIn(g, "ticket_type", [{ v: "", l: "– GLPI-Vorgabe –" }, { v: "1", l: "Störung" }, { v: "2", l: "Anfrage" }], null, false)),
        fieldRow("Anfragequelle", refIn(g, "request_type", "RequestType", "z. B. Antragswerk", conn))),
      h("div", { class: "row2" },
        fieldRow("Status beim Anlegen", selectIn(g, "status", [{ v: "", l: "– Neu (Vorgabe) –" }].concat((GL.status[isChange ? "Change" : "Ticket"] || []).map(([id, n]) => ({ v: String(id), l: n }))), null, false)),
        fieldRow("Lösungsfrist", tplEditor(g, "due_date", s, true), "Datum, z. B. {{.eintritt}} oder {{psdate (addDays -2 .eintritt)}}")),
      h("div", { class: "row3" }, num("urgency", "Dringlichkeit"), num("impact", "Auswirkung"), num("priority", "Priorität")),
      h("div", { class: "row2" }, fieldRow("Entität", refIn(g, "entity", "Entity", "leer = Vorgabe der Verbindung", conn)), fieldRow("Kategorie", refIn(g, "category", "ITILCategory", null, conn))),
      h("div", { class: "row2" }, fieldRow("Ort", refIn(g, "location", "Location", "z. B. {{.raum.gebaeude}}", conn)), isChange ? h("div") : fieldRow("Externe ID", textIn(g, "external_id", { placeholder: "{{.glpi.ref}}" }), "leer = Vorgangsnummer/Schritt")),
      h("small", { class: "muted" }, "Alle Felder sind Vorlagen: z. B. Kategorie {{.software.glpi_kategorie}} aus einem Katalog-Attribut.")));
    if (isChange) {
      inspEl.append(h("details", { class: "insp-sec", open: !!(g.impact_content || g.rollout_plan || g.backout_plan) }, h("summary", { class: "small", style: "cursor:pointer;font-weight:700" }, "Analyse & Pläne (Change)"),
        fieldRow("Auswirkungsanalyse", tplEditor(g, "impact_content", s)), fieldRow("Kontrollliste", tplEditor(g, "control_content", s)),
        fieldRow("Einführungsplan", tplEditor(g, "rollout_plan", s)), fieldRow("Rückfallplan", tplEditor(g, "backout_plan", s)), fieldRow("Checkliste", tplEditor(g, "checklist_plan", s)),
        h("small", { class: "muted" }, "Werden als impactcontent, controlistcontent, rolloutplancontent, backoutplancontent, checklistcontent übergeben.")));
    }
    // Team
    g.team = g.team || [];
    const teamBox = h("div");
    g.team.forEach((t, i) => teamBox.append(h("div", { class: "condbox glpirow" },
      selectIn(t, "role", [{ v: "assigned", l: "Zugewiesen" }, { v: "observer", l: "Beobachter" }, { v: "requester", l: "Anforderer" }], null, false),
      selectIn(t, "type", [{ v: "Group", l: "Gruppe" }, { v: "User", l: "Benutzer" }, { v: "Supplier", l: "Lieferant" }], null, false),
      refIn(t, "value", t.type || "Group", "Name(n) oder ID, ; getrennt", conn),
      RO ? null : h("button", { class: "x", onclick: () => mutate(() => g.team.splice(i, 1), { keepInspector: false }) }, "×"))));
    if (!RO) teamBox.append(h("button", { class: "btn tiny", onclick: () => mutate(() => g.team.push({ role: "assigned", type: "Group", value: "" }), { keepInspector: false }) }, "+ Team"));
    inspEl.append(sec("Team", teamBox, h("small", { class: "muted" }, "z. B. Zugewiesen: Gruppe „IT-Benutzerverwaltung“, Beobachter: {{.antrag.antragsteller}}.")));
    // Aufgaben
    g.tasks = g.tasks || [];
    const taskBox = h("div");
    g.tasks.forEach((t, i) => taskBox.append(h("div", { class: "condbox", style: "margin-bottom:8px;padding:10px" },
      h("div", { class: "opt-row" }, h("b", { style: "flex:1" }, "Aufgabe " + (i + 1)), RO ? null : h("button", { class: "x", onclick: () => mutate(() => g.tasks.splice(i, 1), { keepInspector: false }) }, "×")),
      fieldRow("Text", tplEditor(t, "content", s)),
      h("div", { class: "row2" }, fieldRow("Techniker/in", refIn(t, "user_tech", "User", null, conn)), fieldRow("Techniker-Gruppe", refIn(t, "group_tech", "Group", null, conn))),
      h("div", { class: "row3" }, fieldRow("Kategorie", refIn(t, "category", "TaskCategory", null, conn)), fieldRow("Dauer (Min.)", textIn(t, "duration", { placeholder: "30" })),
        fieldRow("Status", selectIn(t, "state", [{ v: "", l: "Zu erledigen" }, { v: "0", l: "Information" }, { v: "2", l: "Erledigt" }], null, false))),
      h("div", { class: "row2" }, fieldRow("Beginn", textIn(t, "begin", { placeholder: "{{.eintritt}} 08:00" })), fieldRow("Ende", textIn(t, "end"))),
      toggle(t, "private", "privat"),
      h("label", null, "Nur wenn …", condEditor(t, "when", s.foreach ? fieldByKey(s.foreach) : null, "", s)))));
    if (!RO) taskBox.append(h("button", { class: "btn tiny", onclick: () => mutate(() => g.tasks.push({ content: "" }), { keepInspector: false }) }, "+ Aufgabe"));
    inspEl.append(sec("Aufgaben im " + (isChange ? "Change" : "Ticket"), taskBox));
    // Genehmigung in GLPI
    const hasV = !!g.validation;
    const vBox = h("div", null, h("label", { class: "chk" }, h("input", { type: "checkbox", checked: hasV, disabled: RO, onchange: (e) => mutate(() => { if (e.target.checked) g.validation = { approver_type: "Group", approver: "" }; else delete g.validation; }, { keepInspector: false }) }), "Genehmigung in GLPI anfordern"));
    if (hasV) vBox.append(h("div", { class: "row2" }, fieldRow("Genehmigt durch", selectIn(g.validation, "approver_type", [{ v: "Group", l: "Gruppe" }, { v: "User", l: "Benutzer" }], null, false)),
      fieldRow("Name/ID", refIn(g.validation, "approver", g.validation.approver_type || "Group", null, conn))), fieldRow("Text der Anfrage", tplEditor(g.validation, "comment", s, true)));
    inspEl.append(sec("Genehmigung", vBox, h("small", { class: "muted" }, "Freigaben im Antragswerk bleiben unabhängig davon möglich.")));
    // Warten
    const type = isChange ? "Change" : "Ticket";
    inspEl.append(sec("Warten & Ergebnis",
      fieldRow("Weiter im Ablauf", seg("wait", [["done", "wenn gelöst"], ["none", "sofort nach Übergabe"]], "done")),
      g.wait === "none" ? h("small", { class: "muted" }, "Der Schritt ist erledigt, sobald das " + type + " angelegt ist; Ticketnummer: {{.out." + s.id + ".glpi_id}}.") : h("div", null,
        h("label", null, "Gilt als erledigt bei", statusChips(g, "done_status", type)),
        h("label", null, "Gilt als gescheitert bei", statusChips(g, "fail_status", type)),
        fieldRow("Wenn gescheitert", selectIn(g, "on_fail", [{ v: "", l: "Aufgabe für die Zuständigen (Vorgabe)" }, { v: "ablehnen", l: "Antrag ablehnen" }, { v: "weiter", l: "trotzdem weiter" }], null, false)),
        h("small", { class: "muted" }, "Auch „Problem melden“ über den Rückmeldelink zählt als gescheitert."))));
    inspEl.append(h("details", { class: "insp-sec", open: !!g.extra }, h("summary", { class: "small", style: "cursor:pointer;font-weight:700" }, "Weitere GLPI-Felder (JSON)"),
      h("p", { class: "small muted" }, "Jedes Feld der GLPI-API v2, z. B. {\"time_to_own\": \"{{psdate .eintritt}}\", \"user_recipient\": {\"id\": 2}}. Überschreibt gleichnamige Angaben oben."),
      codeEditor(g, "extra", { lang: "text" })));
  }

  function dupStep(s) {
    mutate(() => {
      const c = JSON.parse(JSON.stringify(s));
      c.id = uniqueKey(s.id, new Set(def.steps.map((x) => x.id)));
      c.title = s.title + " (Kopie)";
      c.y = s.y + 60; c.x = s.x + 30;
      def.steps.push(c);
      S.sel = { t: "step", id: c.id };
    });
  }
  function inspEdge(e) {
    const a = stepById(e.from), b = stepById(e.to);
    inspEl.append(h("div", { class: "insp-h" }, h("h3", null, "Verbindung")),
      h("p", null, h("b", null, a ? a.title : e.from), " → ", h("b", null, b ? b.title : e.to)),
      h("p", { class: "small muted" }, "„" + (b ? b.title : e.to) + "“ startet erst, wenn „" + (a ? a.title : e.from) + "“ abgeschlossen (oder entfallen) ist."),
      RO ? null : h("button", { class: "btn danger", onclick: removeSel }, "Verbindung lösen"));
  }
  function listEditor(obj, k, ph) {
    const box = h("div");
    obj[k] = obj[k] || [];
    obj[k].forEach((t, i) => box.append(h("div", { class: "cl-row" }, h("span", { class: "muted" }, "☐"),
      h("input", { value: t, placeholder: ph, disabled: RO, oninput: (e) => live(() => { obj[k][i] = e.target.value; }) }),
      RO ? null : h("button", { class: "x", onclick: () => mutate(() => obj[k].splice(i, 1), { keepInspector: false }) }, "×"))));
    if (!RO) box.append(h("button", { class: "btn tiny", onclick: () => mutate(() => obj[k].push(""), { keepInspector: false }) }, "+ Punkt"));
    return box;
  }
  function outputsEditor(s) {
    const box = h("div");
    s.outputs = s.outputs || [];
    s.outputs.forEach((o, i) => box.append(h("div", { class: "condbox", style: "margin-bottom:6px" },
      h("div", { class: "opt-row" }, h("input", { value: o.label, placeholder: "Beschriftung", disabled: RO, oninput: (e) => live(() => { o.label = e.target.value; }) }),
        h("input", { value: o.key, class: "mono", placeholder: "schluessel", style: "max-width:120px", disabled: RO, oninput: (e) => live(() => { o.key = e.target.value; }) }),
        RO ? null : h("button", { class: "x", onclick: () => mutate(() => s.outputs.splice(i, 1), { keepInspector: false }) }, "×")),
      h("div", { class: "opt-row" }, h("select", { disabled: RO, onchange: (e) => live(() => { o.type = e.target.value; }) }, [["text", "Text"], ["textarea", "Mehrzeilig"], ["email", "E-Mail"], ["phone", "Telefon"], ["date", "Datum"]].map(([v, l]) => h("option", { value: v, selected: o.type === v }, l))),
        h("label", { class: "chk", style: "margin:0" }, h("input", { type: "checkbox", checked: !!o.required, disabled: RO, onchange: (e) => live(() => { o.required = e.target.checked; }) }), "Pflicht")),
      h("input", { value: o.default || "", placeholder: "Vorschlag, z. B. {{.c.sam}}", class: "mono", disabled: RO, oninput: (e) => live(() => { o.default = e.target.value; }) }),
      h("small", { class: "muted" }, "In späteren Schritten: {{.out." + s.id + "." + (o.key || "…") + "}}" + (s.foreach ? " · alle Einträge: {{range .out." + s.id + ".liste}}{{.eintrag}}: {{." + (o.key || "…") + "}}{{end}}" : "")))));
    if (!RO) box.append(h("button", { class: "btn tiny", onclick: () => mutate(() => s.outputs.push({ key: "wert" + (s.outputs.length + 1), label: "Ergebnis", type: "text" }), { keepInspector: false }) }, "+ Ergebnisfeld"));
    return box;
  }

  // ---------- Vorlagen-/Skript-Editor ----------
  function codeEditor(obj, k, opts) {
    opts = opts || {};
    const pre = h("pre", { "aria-hidden": "true" });
    const ta = h("textarea", { spellcheck: "false", disabled: RO, wrap: "off" });
    ta.value = obj[k] || "";
    const sync = () => { pre.innerHTML = AW.highlight(ta.value + "\n", opts.lang === "text" ? "tpl" : "powershell"); pre.scrollTop = ta.scrollTop; pre.scrollLeft = ta.scrollLeft; };
    ta.addEventListener("input", () => { live(() => { obj[k] = ta.value; }); sync(); if (opts.onInput) opts.onInput(); });
    ta.addEventListener("scroll", () => { pre.scrollTop = ta.scrollTop; pre.scrollLeft = ta.scrollLeft; });
    ta.addEventListener("keydown", (e) => {
      if (e.key === "Tab") { e.preventDefault(); const st = ta.selectionStart; ta.setRangeText("    ", st, ta.selectionEnd, "end"); ta.dispatchEvent(new Event("input")); }
    });
    sync();
    const box = h("div", { class: "codeed" + (opts.small ? " small" : "") }, pre, ta);
    box._ta = ta;
    box._sync = sync;
    return box;
  }
  function insertAt(ed, text) {
    const ta = ed._ta;
    const st = ta.selectionStart || ta.value.length;
    ta.setRangeText(text, st, ta.selectionEnd || st, "end");
    ta.focus();
    ta.dispatchEvent(new Event("input"));
  }
  // einfache Vorlagenfelder (einzeilig/mehrzeilig) mit Variablen-Hilfe
  function tplEditor(obj, k, step, oneLine) {
    const wrap = h("div");
    const inp = oneLine ? h("input", { value: obj[k] || "", disabled: RO, oninput: (e) => live(() => { obj[k] = e.target.value; }) })
      : h("textarea", { rows: 6, value: obj[k] || "", disabled: RO, class: "mono", style: "font-size:12.5px", oninput: (e) => live(() => { obj[k] = e.target.value; }) });
    const tools = h("div", { class: "code-tools" },
      h("button", { type: "button", class: "btn tiny ghost", onclick: () => varPicker(step, (t) => { const st = inp.selectionStart || inp.value.length; inp.setRangeText(t, st, inp.selectionEnd || st, "end"); inp.focus(); inp.dispatchEvent(new Event("input")); }) }, "{ } Variable"),
      h("button", { type: "button", class: "btn tiny ghost", onclick: () => previewTpl(step, [inp.value], prev, !oneLine) }, "▶ Vorschau"));
    const prev = h("div");
    wrap.append(inp, RO ? null : tools, prev);
    return wrap;
  }

  function scriptsEditor(s) {
    const box = h("div");
    s.scripts = s.scripts || [];
    s.scripts.forEach((sc, i) => {
      const prev = h("div");
      const ed = codeEditor(sc, "code", { lang: sc.lang, onInput: () => { if (prev.dataset.on) { clearTimeout(prev._t); prev._t = setTimeout(() => previewTpl(s, [sc.code], prev, true), 600); } } });
      box.append(h("div", { class: "condbox", style: "margin-bottom:10px;padding:10px" },
        h("div", { class: "opt-row" }, h("input", { value: sc.title, placeholder: "Titel", disabled: RO, oninput: (e) => live(() => { sc.title = e.target.value; }) }),
          h("select", { style: "max-width:120px", disabled: RO, onchange: (e) => mutate(() => { sc.lang = e.target.value; }, { keepInspector: false }) }, [["powershell", "PowerShell"], ["cmd", "CMD"], ["bash", "Bash"], ["text", "Text"]].map(([v, l]) => h("option", { value: v, selected: (sc.lang || "powershell") === v }, l))),
          i > 0 && !RO ? h("button", { class: "x", title: "nach oben", onclick: () => mutate(() => s.scripts.splice(i - 1, 0, s.scripts.splice(i, 1)[0]), { keepInspector: false }) }, "↑") : null,
          RO ? null : h("button", { class: "x", onclick: () => { if (confirm("Skript löschen?")) mutate(() => s.scripts.splice(i, 1), { keepInspector: false }); } }, "×")),
        ed,
        RO ? null : h("div", { class: "code-tools" },
          h("button", { type: "button", class: "btn tiny", onclick: () => varPicker(s, (t) => insertAt(ed, t)) }, "{ } Variable"),
          h("button", { type: "button", class: "btn tiny", onclick: () => snippetPicker((code) => insertAt(ed, code)) }, "⧉ Baustein"),
          h("button", { type: "button", class: "btn tiny", onclick: () => { prev.dataset.on = "1"; previewTpl(s, [sc.code], prev, true); } }, "▶ Mit Testdaten"),
          h("button", { type: "button", class: "btn tiny ghost", onclick: () => openBig(s, sc) }, "⛶ Groß")),
        prev));
    });
    if (!RO) box.append(h("button", { class: "btn tiny", onclick: () => mutate(() => s.scripts.push({ title: "Neues Skript", lang: "powershell", code: "# {{.antrag.nummer}} – {{.antrag.titel}}\n$ErrorActionPreference = 'Stop'\n" }), { keepInspector: false }) }, "+ Skript"),
      h("p", { class: "small muted" }, "Go-Vorlagen: ", h("code", null, "{{ps .feld}}"), " = sicher in '…' gesetzt, ", h("code", null, "{{psarr (pluck .programme \"software.ad_gruppen\")}}"), " = Liste aller Gruppen, ", h("code", null, "{{range .liste}}…{{end}}"), " = Schleife."));
    return box;
  }

  function openBig(s, sc) {
    const m = modal("Skript: " + sc.title, true);
    const prev = h("div");
    const ed = codeEditor(sc, "code", { onInput: () => { clearTimeout(prev._t); prev._t = setTimeout(() => previewTpl(s, [sc.code], prev, true), 500); } });
    ed.style.minHeight = "420px";
    ed.querySelector("pre").style.minHeight = "420px";
    m.body.append(h("div", { class: "simgrid" }, h("div", null, h("div", { class: "code-tools" },
      h("button", { class: "btn tiny", onclick: () => varPicker(s, (t) => insertAt(ed, t)) }, "{ } Variable"),
      h("button", { class: "btn tiny", onclick: () => snippetPicker((c) => insertAt(ed, c)) }, "⧉ Baustein")), ed),
      h("div", null, h("b", null, "Ergebnis mit Testdaten"), prev)),
      h("div", { class: "actions", style: "margin-top:12px" }, h("button", { class: "btn primary", onclick: () => { m.close(); renderInspector(); } }, "Fertig")));
    previewTpl(s, [sc.code], prev, true);
  }

  async function previewTpl(step, tpls, box, code) {
    box.innerHTML = "";
    box.append(h("div", { class: "small muted" }, "wird berechnet …"));
    try {
      const r = await AW.api("POST", "/api/designer/render", { def: clean(def), data: testData(), tpls, step: step ? step.id : "", item: 0 });
      box.innerHTML = "";
      const res = (r.results || [])[0] || {};
      if (res.err) box.append(h("div", { class: "hint err" }, res.err));
      const pre = h("div", { class: "prevbox" + (code ? "" : " text") });
      if (code) pre.innerHTML = AW.highlight(res.out || "", "powershell"); else pre.textContent = res.out || "(leer)";
      box.append(pre, h("div", { class: "small muted" }, "Testdaten bearbeiten unter „▶ Test & Vorschau“" + (step && step.foreach ? " · gezeigt: 1. Eintrag" : "")));
    } catch (e) { box.innerHTML = ""; box.append(h("div", { class: "hint err" }, e.message)); }
  }

  function varPicker(step, insert) {
    const m = modal("Variable einfügen");
    const q = h("input", { type: "search", placeholder: "Suchen …" });
    const list = h("div", { class: "varlist" });
    const groups = [];
    const add = (g, items) => { if (items.length) groups.push({ g, items }); };
    const fe = step && step.foreach ? fieldByKey(step.foreach) : null;
    if (fe) {
      const it = [];
      if (fe.type === "group") (fe.fields || []).forEach((c) => {
        if (c.type === "info") return;
        it.push(["{{.item." + c.key + "}}", c.label]);
        if (c.type === "catalog" && catByKey[c.catalog]) catByKey[c.catalog].attrs.forEach((a) => it.push(["{{.item." + c.key + "." + a.key + "}}", c.label + " → " + a.label]));
        if (c.type === "select" || c.type === "radio") it.push(["{{.item." + c.key + "_text}}", c.label + " (Beschriftung)"]);
      });
      else if (catByKey[fe.catalog]) { it.push(["{{.item}}", "Eintrag (Bezeichnung)"]); catByKey[fe.catalog].attrs.forEach((a) => it.push(["{{.item." + a.key + "}}", a.label])); }
      it.push(["{{.index}}", "laufende Nummer"]);
      add("Aktueller Eintrag (je " + (fe.item_label || fe.label) + ")", it);
    }
    const f = [];
    allFields().forEach((x) => {
      if (x.type === "info") return;
      if (x.type === "group") {
        const sub = (x.fields || []).filter((c) => c.type !== "info").map((c) => "{{." + c.key + "}}").join(" ");
        f.push(["{{range ." + x.key + "}}\n" + sub + "\n{{end}}", x.label + " – Schleife über alle Einträge"]);
        (x.fields || []).forEach((c) => { if (c.type === "catalog" && catByKey[c.catalog]) catByKey[c.catalog].attrs.forEach((a) => f.push(["{{psarr (pluck ." + x.key + " \"" + c.key + "." + a.key + "\")}}", x.label + ": alle „" + a.label + "“ als PS-Liste"])); });
        return;
      }
      f.push(["{{." + x.key + "}}", x.label]);
      if (x.type === "select" || x.type === "radio" || x.type === "checkboxes") f.push(["{{." + x.key + "_text}}", x.label + " (Beschriftung)"]);
      if (x.type === "date") f.push(["{{datum ." + x.key + "}}", x.label + " (TT.MM.JJJJ)"]);
      if (x.type === "catalog" && catByKey[x.catalog]) {
        if (x.multi) catByKey[x.catalog].attrs.forEach((a) => f.push(["{{psarr (pluck ." + x.key + " \"" + a.key + "\")}}", x.label + ": alle „" + a.label + "“"]));
        else catByKey[x.catalog].attrs.forEach((a) => f.push(["{{." + x.key + "." + a.key + "}}", x.label + " → " + a.label]));
      }
      if (x.type === "user") [["name", "Name"], ["sam", "Anmeldename"], ["mail", "Mail"], ["given", "Vorname"], ["surname", "Nachname"], ["department", "Abteilung"], ["title", "Funktion"], ["phone", "Telefon"]].forEach(([k, l]) => f.push(["{{." + x.key + "." + k + "}}", x.label + " → " + l]));
    });
    if (step && step.kind === "glpi") add("GLPI (dieser Schritt)", [["{{.glpi.rueckmeldung}}", "Rückmeldelink (Seite: erledigt melden + Angaben)"], ["{{.glpi.api}}", "Rückmelde-API (POST, JSON oder Formular)"],
      ["{{.glpi.ref}}", "Referenz Vorgang/Schritt"], ["{{.glpi.vorgang}}", "Link zum Vorgang"], ["{{.glpi.token}}", "Token"], ["{{.glpi.marker}}", "Marker für Webhooks"], ["{{.zusammenfassung}}", "alle Antragsangaben als Text"]]);
    add("Antragsdaten", f);
    add("Berechnete Werte", def.computed.map((c) => ["{{.c." + c.key + "}}", c.label || c.key]));
    add("Ergebnisse früherer Schritte", def.steps.flatMap((st) => (st.outputs || []).map((o) => ["{{.out." + st.id + "." + o.key + "}}", st.title + " → " + o.label])
      .concat(st.foreach ? [["{{range .out." + st.id + ".liste}}\n{{.eintrag}}: " + (st.outputs || []).map((o) => "{{." + o.key + "}}").join(" ") + " ({{.von}})\n{{end}}", st.title + " → alle Einträge (Liste)"]] : [])
      .concat(st.kind === "freigabe" ? [["{{.out." + st.id + ".von}}", st.title + " → erteilt von"]] : [])
      .concat(st.kind === "glpi" ? [["{{.out." + st.id + ".glpi_id}}", st.title + " → Ticket-Nr."], ["{{.out." + st.id + ".glpi_url}}", st.title + " → Link in GLPI"],
        ["{{.out." + st.id + ".glpi_status_name}}", st.title + " → Status"], ["{{.out." + st.id + ".glpi_solution}}", st.title + " → Lösungstext"]] : [])));
    add("Vorgang", [["{{.antrag.nummer}}", "Vorgangsnummer"], ["{{.antrag.titel}}", "Betreff"], ["{{.antrag.antragsteller_name}}", "Antragsteller/in"], ["{{.antrag.antragsteller_mail}}", "Mail Antragsteller/in"], ["{{.antrag.datum}}", "Datum"], ["{{.antrag.link}}", "Link zum Vorgang"]]);
    add("Globale Variablen", VARS.map((v) => ["{{.var." + v.key + "}}", v.label + " = " + v.value]));
    add("Funktionen", [["{{ps .feld}}", "PowerShell-String 'sicher'"], ["{{psarr .liste}}", "PowerShell-Array @('a','b')"], ["{{pluck .gruppe \"feld.attr\"}}", "Werte aus allen Einträgen (zerlegt ; und entdoppelt)"],
      ["{{join \", \" .liste}}", "Liste verbinden"], ["{{split .text}}", "Text an ; , Zeilen trennen"], ["{{concat .a .b}}", "Listen zusammenführen"], ["{{sam .vorname .nachname}}", "Benutzername nach Schema"],
      ["{{lower (ascii .text)}}", "klein + Umlaute umschreiben"], ["{{datum .d}}", "Datum TT.MM.JJJJ"], ["{{psdate (addDays 1 .d)}}", "Datum +1 Tag für Get-Date"], ["{{default \"x\" .wert}}", "Vorgabe wenn leer"],
      ["{{if .feld}}…{{end}}", "Bedingung"], ["{{if eq .feld \"wert\"}}…{{else}}…{{end}}", "Vergleich"], ["{{if has .liste \"wert\"}}…{{end}}", "Liste enthält"], ["{{pstrue .checkbox}}", "$true/$false"]]);
    const draw = () => {
      list.innerHTML = "";
      const s = q.value.trim().toLowerCase();
      groups.forEach((g) => {
        const items = g.items.filter(([v, l]) => !s || (v + " " + l).toLowerCase().includes(s));
        if (!items.length) return;
        list.append(h("div", { class: "vh" }, g.g));
        items.forEach(([v, l]) => list.append(h("button", { type: "button", onclick: () => { insert(v); m.close(); } }, h("span", null, l), h("code", null, v.length > 60 ? v.slice(0, 60) + "…" : v))));
      });
    };
    q.addEventListener("input", draw);
    m.body.append(q, h("div", { style: "height:8px" }), list);
    list.style.maxHeight = "60vh";
    draw();
    q.focus();
  }

  function snippetPicker(insert) {
    const m = modal("Skriptbaustein einfügen", true);
    const groups = {};
    SNIPS.forEach((s) => { (groups[s.group || "Weitere"] = groups[s.group || "Weitere"] || []).push(s); });
    const prev = h("pre", { class: "prevbox", style: "min-height:300px" });
    let cur = null;
    const list = h("div", { class: "varlist", style: "max-height:60vh" });
    Object.entries(groups).forEach(([g, l]) => {
      list.append(h("div", { class: "vh" }, g));
      l.forEach((s) => list.append(h("button", { type: "button", onclick: () => { cur = s; prev.innerHTML = AW.highlight(s.code, "powershell"); } }, h("span", null, h("b", null, s.title), h("br"), h("small", { class: "muted" }, s.description)))));
    });
    m.body.append(h("div", { class: "simgrid" }, list, h("div", null, prev, h("div", { class: "actions", style: "margin-top:8px" },
      h("button", { class: "btn primary", onclick: () => { if (cur) { insert(cur.code); m.close(); } } }, "Einfügen"),
      h("a", { class: "btn ghost", href: "/stammdaten#snippet", target: "_blank" }, "Bausteine pflegen")))),
      h("p", { class: "small muted" }, "Bausteine verwenden Platzhalter wie {{.c.sam}} – nach dem Einfügen an die Felder dieses Ablaufs anpassen."));
  }

  // ======================= Einstellungen =======================
  function renderSettings() {
    const wrap = h("div", { class: "fb" });
    const card = (title, ...kids) => h("div", { class: "card insp" }, h("h2", null, title), kids);
    wrap.append(
      card("Allgemein",
        h("div", { class: "row2" }, fieldRow("Name", textIn(def, "name", {}, () => renderTop())), fieldRow("Kategorie", textIn(def, "category", { list: "cats" }), "Gruppiert die Kacheln unter „Antrag stellen“")),
        h("datalist", { id: "cats" }, ["Benutzer & Konten", "Berechtigungen", "Hardware", "Sonstiges"].map((c) => h("option", { value: c }))),
        fieldRow("Beschreibung", areaIn(def, "description", 2)),
        h("div", { class: "row3" }, fieldRow("Symbol", textIn(def, "icon", { maxlength: 4 }), "Emoji"), fieldRow("Farbe", h("input", { type: "color", value: def.color || "#1f5fbf", disabled: RO, oninput: (e) => live(() => { def.color = e.target.value; }), onchange: () => renderTop() })),
          fieldRow("Nummernkreis", textIn(def, "prefix", { maxlength: 6, style: "text-transform:uppercase" }), (def.prefix || "AW") + "-2026-0001")),
        fieldRow("Hinweis über dem Formular", areaIn(def, "intro", 2))),
      card("Betreff des Vorgangs", fieldRow("Vorlage", tplEditor(def, "title_tpl", null, true), "z. B. Eintritt {{.vorname}} {{.nachname}} ab {{datum .eintritt}}")),
      card("Wer darf den Antrag stellen?",
        h("div", { class: "chips" }, STELLEN.map((s) => { const on = def.audience.includes(s.key); return h("span", { class: "chip" + (on ? " on" : ""), onclick: () => { if (!RO) mutate(() => { def.audience = on ? def.audience.filter((x) => x !== s.key) : def.audience.concat(s.key); }); } }, s.name); })),
        h("p", { class: "small muted" }, def.audience.length ? "Nur Mitglieder der gewählten Stellen (und Administration)." : "Keine Auswahl = alle angemeldeten Personen.")),
      card("Berechnete Werte", h("p", { class: "small muted" }, "Einmal zentral berechnen, überall nutzen: in Skripten, Mails und Bedingungen als ", h("code", null, "{{.c.schluessel}}"), ". Werte können frühere Ergebnisse berücksichtigen, z. B. den tatsächlich vergebenen Benutzernamen."),
        computedEditor()),
      card("Abschlussmail an Antragsteller/in", h("p", { class: "small muted" }, "Leer = Standardtext. Zusätzlich verfügbar: {{.link}}."),
        fieldRow("Betreff", tplEditor(def.done, "subject", null, true)), fieldRow("Text", tplEditor(def.done, "body", null))));
    canvasEl.append(wrap);
  }
  function computedEditor() {
    const box = h("div");
    def.computed.forEach((c, i) => {
      const prev = h("div");
      box.append(h("div", { class: "condbox", style: "margin-bottom:8px" },
        h("div", { class: "opt-row" }, h("input", { value: c.label, placeholder: "Bezeichnung", disabled: RO, oninput: (e) => live(() => { c.label = e.target.value; }) }),
          h("input", { value: c.key, class: "mono", style: "max-width:140px", placeholder: "schluessel", disabled: RO, oninput: (e) => live(() => { c.key = e.target.value; }) }),
          i > 0 && !RO ? h("button", { class: "x", onclick: () => mutate(() => def.computed.splice(i - 1, 0, def.computed.splice(i, 1)[0])) }, "↑") : null,
          RO ? null : h("button", { class: "x", onclick: () => mutate(() => def.computed.splice(i, 1)) }, "×")),
        h("input", { value: c.tpl, class: "mono", disabled: RO, oninput: (e) => live(() => { c.tpl = e.target.value; }) }),
        h("div", { class: "code-tools" }, RO ? null : h("button", { class: "btn tiny ghost", onclick: (e) => varPicker(null, (t) => { const inp = e.target.closest(".condbox").querySelector("input.mono:last-of-type"); mutate(() => { c.tpl = (c.tpl || "") + t; }); }) }, "{ } Variable"),
          h("button", { class: "btn tiny ghost", onclick: () => previewTpl(null, [c.tpl], prev, false) }, "▶ Vorschau")), prev));
    });
    if (!RO) box.append(h("button", { class: "btn tiny", onclick: () => mutate(() => def.computed.push({ key: "wert" + (def.computed.length + 1), label: "Neuer Wert", tpl: "" })) }, "+ Berechneter Wert"));
    return box;
  }

  // ======================= Test & Vorschau =======================
  function renderTest() {
    const wrap = h("div", { style: "padding:20px;max-width:1500px;margin:0 auto" });
    const planBox = h("div", { class: "simplan" }, h("p", { class: "muted" }, "wird berechnet …"));
    const compBox = h("div");
    const formBox = h("div");
    wrap.append(h("div", { class: "pagehead", style: "margin-bottom:12px" }, h("div", null, h("h1", null, "Test & Vorschau"),
      h("p", { class: "muted", style: "margin:0" }, "Formular ausfüllen und sofort sehen, welche Freigaben und Aufgaben entstehen, wer zuständig ist und was parallel läuft. Die Testdaten nutzt auch die Skript-Vorschau.")),
      h("div", { class: "actions" }, h("button", { class: "btn ghost", onclick: () => { S.test = null; store("awd-test-" + KEY, "null"); renderCanvas(); } }, "Testdaten zurücksetzen"))));
    wrap.append(h("div", { class: "simgrid" }, h("div", null, formBox), h("div", null,
      h("div", { class: "card", style: "position:sticky;top:10px" }, h("h2", null, "Ablauf für diese Daten"), planBox, h("h3", null, "Betreff & berechnete Werte"), compBox))));
    canvasEl.append(wrap);
    const update = async (d) => {
      try {
        const [sim, ren] = await Promise.all([
          AW.api("POST", "/api/designer/simulate", { def: clean(def), data: d }),
          AW.api("POST", "/api/designer/render", { def: clean(def), data: d, tpls: [def.title_tpl || def.name] }),
        ]);
        drawPlan(planBox, sim.plan || []);
        compBox.innerHTML = "";
        const t = (ren.results || [])[0] || {};
        compBox.append(h("dl", { class: "kv" }, h("dt", null, "Betreff"), h("dd", null, h("b", null, t.out || "–"), t.err ? h("div", { class: "hint err" }, t.err) : null),
          def.computed.map((c) => [h("dt", null, c.label || c.key), h("dd", { class: "mono" }, (ren.computed || {})[c.key] || "–")])));
      } catch (e) { planBox.innerHTML = ""; planBox.append(h("div", { class: "hint err" }, e.message)); }
    };
    let t;
    AW.mountForm(formBox, { mode: "preview", key: KEY, def: clean(def), catalogs: catalogsMap(), data: testData(), me: {} }, {
      preview: true, noSide: true,
      onChange: (d) => { S.test = d; store("awd-test-" + KEY, JSON.stringify(d)); clearTimeout(t); t = setTimeout(() => update(d), 400); },
      simulate: (d) => AW.api("POST", "/api/designer/simulate", { def: clean(def), data: d }),
    });
    update(testData());
  }
  function drawPlan(box, plan) {
    box.innerHTML = "";
    const lv = {};
    plan.forEach((p) => { (lv[p.level] = lv[p.level] || []).push(p); });
    Object.keys(lv).sort((a, b) => a - b).forEach((l, n) => {
      const items = lv[l];
      const active = items.filter((p) => !p.skipped).length;
      box.append(h("div", { class: "lv" }, h("div", { class: "lvh" }, "Stufe " + (n + 1) + (active > 1 ? " · " + active + " parallel" : "")),
        items.map((p) => h("div", { class: "it" + (p.skipped ? " sk" : ""), style: "--kc:" + (KCOL[p.kind] || "#64748b"), title: p.reason || "" },
          (skByKey[p.kind] || {}).icon, h("span", null, h("b", null, p.title), p.item ? " · " + p.item : "", p.skipped ? h("div", { class: "small muted" }, "entfällt – " + (p.reason || "")) : null),
          h("small", null, p.skipped ? "" : (p.assignee && p.assignee.label) || "")))));
    });
    if (!plan.length) box.append(h("p", { class: "muted" }, "Keine Schritte."));
  }

  // ======================= Prüfung =======================
  function renderCheck() {
    const wrap = h("div", { style: "padding:24px;max-width:900px;margin:0 auto" });
    const err = S.issues.filter((i) => i.level === "error"), warn = S.issues.filter((i) => i.level !== "error");
    wrap.append(h("h1", null, "Prüfung"), h("p", { class: "muted" }, err.length ? err.length + " Fehler verhindern das Veröffentlichen." : "Keine Fehler – der Ablauf kann veröffentlicht werden.", warn.length ? " " + warn.length + " Hinweise." : ""));
    if (!S.issues.length) wrap.append(h("div", { class: "card empty-state" }, h("div", { class: "big-ico" }, "✓"), h("h2", null, "Alles in Ordnung")));
    else wrap.append(h("ul", { class: "issues" }, S.issues.map((i) => h("li", { class: i.level, onclick: () => gotoIssue(i) }, i.level === "error" ? "✕" : "!", h("span", null, i.msg, h("small", { class: "muted", style: "display:block" }, i.ref))))));
    canvasEl.append(wrap);
  }
  function gotoIssue(i) {
    const [t, id] = i.ref.split(":");
    if (t === "step") { S.tab = "flow"; S.sel = { t: "step", id }; }
    else if (t === "field") { S.tab = "form"; S.preview = false; const k = id.split(".").pop(); const f = allFields().flatMap((x) => [x].concat(x.fields || [])).find((x) => x.key === k); S.sel = f ? { t: "field", u: f._u } : null; }
    else if (t === "section") { S.tab = "form"; const s = def.form.sections.find((x) => x.id === id); S.sel = s ? { t: "section", u: s._u } : null; }
    else if (t === "settings" || t === "computed") { S.tab = "settings"; S.sel = null; }
    else { S.tab = "flow"; S.sel = null; }
    render();
  }

  mount();
})();
