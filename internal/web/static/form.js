// Antragswerk – Formular-Renderer (Antrag stellen, Rückfrage, Designer-Vorschau)
"use strict";
(function () {
  const AW = (window.AW = window.AW || {});
  const T = (k, d) => (AW.t ? AW.t(k) : d || k);

  function h(tag, attrs, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === "class") el.className = v;
      else if (k === "style") el.setAttribute("style", v);
      else if (k === "value") el.value = v;
      else if (k === "checked") el.checked = !!v;
      else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
      else el.setAttribute(k, v === true ? "" : v);
    }
    for (const c of kids.flat(4)) {
      if (c == null || c === false || c === true) continue;
      el.append(c.nodeType ? c : document.createTextNode(String(c)));
    }
    return el;
  }
  AW.h = h;

  const isEmpty = (v) => v == null || v === "" || v === false || (Array.isArray(v) && v.length === 0) || (typeof v === "object" && !Array.isArray(v) && !v.sam && !v.name && !v.mail);

  // ---------- Bedingungen (gleiche Semantik wie internal/flow/cond.go) ----------
  function makeCond(catalogs, fieldIndex) {
    function resolve(data, item, path) {
      const parts = path.split(".");
      let cur, field;
      if (parts[0] === "item") { cur = item; parts.shift(); }
      else if (item && Object.prototype.hasOwnProperty.call(item, parts[0])) { cur = item; }
      else cur = data;
      let fdefs = fieldIndex;
      for (let i = 0; i < parts.length; i++) {
        const p = parts[i];
        if (cur == null) return undefined;
        if (typeof cur === "string" && field && field.type === "catalog") {
          const c = catalogs[field.catalog];
          const e = c && c.entries.find((x) => x.key === cur);
          if (!e) return "";
          cur = p === "label" ? e.label : p === "key" ? e.key : (e.attrs || {})[p];
          field = null;
          continue;
        }
        if (Array.isArray(cur)) return undefined;
        cur = cur[p];
        field = fdefs ? fdefs[p] : null;
        fdefs = field && field.fields ? Object.fromEntries(field.fields.map((x) => [x.key, x])) : null;
      }
      return cur;
    }
    function norm(v) {
      if (v == null || v === "") return [];
      if (typeof v === "boolean") return [v ? "true" : "false"];
      if (Array.isArray(v)) return v.flatMap(norm);
      if (typeof v === "object") { if ("sam" in v) return v.sam ? [v.sam] : []; return Object.entries(v).filter(([k]) => k !== "nr").flatMap(([, x]) => norm(x)); }
      return [String(v)];
    }
    function rule(r, data, item) {
      const raw = resolve(data, item, r.field);
      const vals = norm(raw), first = (r.values || [])[0] || "";
      const has = (x) => vals.some((v) => v.toLowerCase() === String(x).toLowerCase());
      const count = Array.isArray(raw) ? raw.length : vals.length;
      switch (r.op) {
        case "eq": return has(first);
        case "ne": return !has(first);
        case "in": return (r.values || []).some(has);
        case "nin": return !(r.values || []).some(has);
        case "set": return vals.length > 0 && !(vals.length === 1 && vals[0] === "false");
        case "unset": return vals.length === 0 || (vals.length === 1 && vals[0] === "false");
        case "true": return has("true");
        case "false": return !has("true");
        case "contains": return has(first) || vals.some((v) => v.toLowerCase().includes(first.toLowerCase()));
        case "ncontains": return !(has(first) || vals.some((v) => v.toLowerCase().includes(first.toLowerCase())));
        case "count_gte": return count >= (parseInt(first, 10) || 0);
        case "count_lt": return count < (parseInt(first, 10) || 0);
        case "gte": case "lte": {
          if (!vals.length) return false;
          const a = parseFloat(vals[0].replace(",", ".")), b = parseFloat(first.replace(",", "."));
          if (isNaN(a) || isNaN(b) || /-/.test(vals[0])) return r.op === "gte" ? vals[0] >= first : vals[0] <= first;
          return r.op === "gte" ? a >= b : a <= b;
        }
        case "same": { const o = norm(resolve(data, item, first)); return vals.length > 0 && vals.join("|") === o.join("|"); }
        case "changed": { const o = norm(resolve(data, item, first)); return vals.join("|") !== o.join("|"); }
      }
      return false;
    }
    return function match(c, data, item) {
      if (!c || !c.rules || !c.rules.length) return true;
      return c.mode === "any" ? c.rules.some((r) => rule(r, data, item)) : c.rules.every((r) => rule(r, data, item));
    };
  }
  AW.makeCond = makeCond;

  function defaultValue(f) {
    switch (f.type) {
      case "checkbox": return f.default === "true" || f.default === "ja";
      case "checkboxes": return f.default ? f.default.split(/[;,]/).map((x) => x.trim()).filter(Boolean) : [];
      case "catalog": return f.multi ? (f.default ? f.default.split(/[;,]/).map((x) => x.trim()) : []) : (f.default || "");
      case "group": return [];
      case "user": return null;
      case "info": return undefined;
    }
    return f.default || "";
  }
  function newItem(g) {
    const it = {};
    (g.fields || []).forEach((c) => { if (c.type !== "info") it[c.key] = defaultValue(c); });
    return it;
  }
  AW.newItem = newItem;

  // ---------- Personensuche ----------
  async function searchUsers(q) {
    const r = await fetch("/api/users?q=" + encodeURIComponent(q), { credentials: "same-origin" });
    const d = await r.json().catch(() => ({}));
    return d.results || [];
  }

  // ======================= Formular =======================
  AW.mountForm = function (root, P, opts) {
    opts = opts || {};
    const def = P.def;
    const catalogs = P.catalogs || {};
    const sections = (def.form && def.form.sections) || [];
    const fieldIndex = {};
    sections.forEach((s) => (s.fields || []).forEach((f) => { fieldIndex[f.key] = f; }));
    const match = makeCond(catalogs, fieldIndex);
    const draftKey = "aw-draft-" + P.key;
    let data = {};
    let touched = new Set();
    let serverErr = {};
    let submitted = false;
    let plan = null;

    // Initialwerte
    if (P.data) data = JSON.parse(JSON.stringify(P.data));
    sections.forEach((s) => (s.fields || []).forEach((f) => {
      if (f.type === "info") return;
      if (data[f.key] === undefined) data[f.key] = defaultValue(f);
      if (f.type === "group" && !P.data) {
        const n = Math.max(f.min || 0, f.required ? 1 : 0);
        while (data[f.key].length < n) data[f.key].push(newItem(f));
      }
    }));
    if (P.mode === "new" && !opts.preview) {
      try {
        const d = JSON.parse(localStorage.getItem(draftKey) || "null");
        if (d && d.data && Date.now() - d.at < 14 * 864e5) { data = Object.assign(data, d.data); setTimeout(() => AW.toast && AW.toast("Ihr Entwurf wurde wiederhergestellt."), 300); }
      } catch (e) { /* kein Speicher */ }
    }
    if (opts.initial) data = Object.assign(data, opts.initial);

    const saveDraft = () => {
      if (P.mode !== "new" || opts.preview) return;
      try { localStorage.setItem(draftKey, JSON.stringify({ at: Date.now(), data })); } catch (e) { /* egal */ }
    };

    // ---------- Validierung ----------
    function fieldErrors(f, v, path) {
      const out = {};
      if (f.type === "group") {
        const lst = Array.isArray(v) ? v : [];
        lst.forEach((it, i) => {
          (f.fields || []).forEach((c) => {
            if (c.type === "info" || !match(c.show, data, it)) return;
            Object.assign(out, fieldErrors(c, it[c.key], path + "." + i + "." + c.key));
          });
        });
        if (f.required && lst.length === 0) out[path] = "Mindestens einen Eintrag hinzufügen.";
        else if (f.min && lst.length < f.min) out[path] = "Mindestens " + f.min + " Einträge erforderlich.";
        else if (f.max && lst.length > f.max) out[path] = "Höchstens " + f.max + " Einträge.";
        return out;
      }
      if (isEmpty(v)) { if (f.required) out[path] = f.type === "checkbox" ? "Bitte bestätigen." : T("form.required"); return out; }
      const s = typeof v === "string" ? v.trim() : "";
      if (f.type === "email" && s && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s)) out[path] = "Keine gültige E-Mail-Adresse.";
      if (f.type === "number" && s && !/^-?\d+([.,]\d+)?$/.test(s)) out[path] = "Bitte eine Zahl eingeben.";
      if (f.pattern && s) { try { if (!new RegExp(f.pattern).test(s)) out[path] = f.pattern_msg || "Format ungültig."; } catch (e) { /* Muster kaputt */ } }
      return out;
    }
    function allErrors() {
      const out = {};
      sections.forEach((sec) => {
        if (!match(sec.show, data)) return;
        (sec.fields || []).forEach((f) => { if (f.type !== "info" && match(f.show, data)) Object.assign(out, fieldErrors(f, data[f.key], f.key)); });
      });
      return out;
    }
    function progress() {
      let req = 0, ok = 0;
      const per = {};
      sections.forEach((sec) => {
        const st = { req: 0, ok: 0, err: 0, visible: match(sec.show, data) };
        per[sec.id] = st;
        if (!st.visible) return;
        (sec.fields || []).forEach((f) => {
          if (f.type === "info" || !match(f.show, data)) return;
          if (f.required) { st.req++; if (!isEmpty(data[f.key])) st.ok++; }
          if (f.type === "group") (data[f.key] || []).forEach((it) => (f.fields || []).forEach((c) => { if (c.required && match(c.show, data, it)) { st.req++; if (!isEmpty(it[c.key])) st.ok++; } }));
        });
        req += st.req; ok += st.ok;
      });
      const errs = allErrors();
      Object.keys(errs).forEach((p) => {
        const root = p.split(".")[0];
        sections.forEach((s) => { if ((s.fields || []).some((f) => f.key === root)) per[s.id].err++; });
      });
      return { req, ok, per, errs };
    }

    // ---------- Rendering ----------
    let vis = [];
    const elByPath = {};
    let sideEl, navEl, ringEl, planEl;

    function changed(structural) {
      saveDraft();
      if (structural) render(); else refresh();
      if (opts.onChange) opts.onChange(data);
      schedulePlan();
    }

    function wrapField(f, path, item, body) {
      const w = h("div", { class: "ff w-" + (f.width || "full") + " t-" + f.type, "data-path": path },
        f.type === "checkbox" || f.type === "info" ? null : h("div", { class: "fl" }, f.label, f.required ? h("span", { class: "req", title: "Pflichtfeld" }, "*") : null),
        body,
        f.help && f.type !== "info" ? h("div", { class: "help" }, f.help) : null,
        h("div", { class: "ferr" }));
      if (f.show && f.show.rules && f.show.rules.length) vis.push({ el: w, cond: f.show, item });
      elByPath[path] = w;
      return w;
    }

    function input(f, get, set, path) {
      switch (f.type) {
        case "textarea":
          return h("textarea", { rows: 3, placeholder: f.placeholder || "", value: get() || "", oninput: (e) => { set(e.target.value); changed(); }, onblur: () => { touched.add(path); refresh(); } });
        case "select": {
          const s = h("select", { onchange: (e) => { set(e.target.value); touched.add(path); changed(); } },
            h("option", { value: "" }, T("form.choose")), (f.options || []).map((o) => h("option", { value: o.value }, o.label || o.value)));
          s.value = get() || "";
          return s;
        }
        case "radio": {
          const box = h("div", { class: "pills radio", role: "radiogroup" });
          (f.options || []).forEach((o) => {
            const on = get() === o.value;
            box.append(h("label", { class: on ? "on" : "" }, h("input", { type: "radio", name: path, checked: on, onchange: () => { set(o.value); touched.add(path); changed(true); } }), o.label || o.value));
          });
          return box;
        }
        case "checkboxes": {
          const box = h("div", { class: "pills" });
          (f.options || []).forEach((o) => {
            const cur = get() || [];
            const on = cur.includes(o.value);
            box.append(h("label", { class: on ? "on" : "" }, h("input", { type: "checkbox", checked: on, onchange: (e) => {
              const l = (get() || []).filter((x) => x !== o.value);
              if (e.target.checked) l.push(o.value);
              set((f.options || []).map((x) => x.value).filter((x) => l.includes(x)));
              touched.add(path); changed(true);
            } }), o.label || o.value));
          });
          return box;
        }
        case "checkbox":
          return h("label", { class: "switch" }, h("input", { type: "checkbox", checked: !!get(), onchange: (e) => { set(e.target.checked); touched.add(path); changed(true); } }),
            h("span", null, f.label, f.required ? h("span", { class: "req" }, " *") : null));
        case "catalog": return catalogInput(f, get, set, path);
        case "user": return userInput(f, get, set, path);
        case "info": return h("div", { class: "callout " + (f.tone || "info") }, f.label);
        default: {
          const type = { number: "text", date: "date", email: "email", phone: "tel" }[f.type] || "text";
          return h("input", { type, inputmode: f.type === "number" ? "decimal" : null, placeholder: f.placeholder || "", value: get() || "",
            oninput: (e) => { set(e.target.value); changed(); }, onchange: () => { touched.add(path); changed(); }, onblur: () => { touched.add(path); refresh(); } });
        }
      }
    }

    function entriesOf(f) {
      const c = catalogs[f.catalog];
      if (!c) return [];
      return (c.entries || []).filter((e) => !f.filter || match(f.filter, e.attrs || {}));
    }
    const INFO_ATTRS = ["pfad", "adresse", "rollen", "gebaeude", "hinweis"];
    function catalogInput(f, get, set, path) {
      const c = catalogs[f.catalog] || { attrs: [], entries: [] };
      const entries = entriesOf(f);
      const groups = [];
      entries.forEach((e) => { const g = e.group || ""; let x = groups.find((y) => y.n === g); if (!x) groups.push((x = { n: g, l: [] })); x.l.push(e); });
      if (f.multi) {
        const box = h("div", { class: "pills" });
        groups.forEach((g) => {
          if (g.n && groups.length > 1) box.append(h("div", { class: "pgroup" }, g.n));
          g.l.forEach((e) => {
            const on = (get() || []).includes(e.key);
            box.append(h("label", { class: on ? "on" : "", title: INFO_ATTRS.map((k) => (e.attrs || {})[k]).filter(Boolean).join(" · ") },
              h("input", { type: "checkbox", checked: on, onchange: (ev) => {
                const l = (get() || []).filter((x) => x !== e.key);
                if (ev.target.checked) l.push(e.key);
                set(l); touched.add(path); changed(true);
              } }), e.label));
          });
        });
        if (!entries.length) box.append(h("span", { class: "muted small" }, "Katalog „" + (c.name || f.catalog) + "“ ist leer."));
        return box;
      }
      const sel = h("select", { onchange: (e) => { set(e.target.value); touched.add(path); changed(true); } }, h("option", { value: "" }, T("form.choose")),
        groups.map((g) => (g.n ? h("optgroup", { label: g.n }, g.l.map((e) => h("option", { value: e.key }, e.label))) : g.l.map((e) => h("option", { value: e.key }, e.label)))));
      sel.value = get() || "";
      const cur = entries.find((e) => e.key === get());
      const info = cur ? INFO_ATTRS.filter((k) => cur.attrs && cur.attrs[k]).map((k) => {
        const a = (c.attrs || []).find((x) => x.key === k);
        return h("span", null, (a ? a.label : k) + ": ", h("b", null, cur.attrs[k]));
      }) : [];
      return h("div", { class: "catsel" }, sel, info.length ? h("div", { class: "catinfo" }, info) : null);
    }

    function userInput(f, get, set, path) {
      const v = get();
      const box = h("div", { class: "userbox" });
      const pick = (p) => {
        set(p);
        touched.add(path);
        // Vorbelegung abhängiger Felder
        sections.forEach((s) => (s.fields || []).forEach((x) => {
          if (x.prefill && x.prefill.from === f.key && p && isEmpty(data[x.key])) data[x.key] = p[x.prefill.attr] || "";
        }));
        changed(true);
      };
      if (v && (v.name || v.sam)) {
        box.append(h("div", { class: "userchip" }, h("span", { class: "person" }, h("span", { class: "avatar sm" }, initials(v.name || v.sam)),
          h("span", null, h("b", null, v.name || v.sam), h("small", null, [v.title, v.department, v.mail].filter(Boolean).join(" · ")))),
          opts.preview && opts.readonly ? null : h("button", { type: "button", title: "Entfernen", onclick: () => { set(null); changed(true); } }, "×")));
        return box;
      }
      const inp = h("input", { type: "search", placeholder: f.placeholder || T("form.user_search"), autocomplete: "off" });
      const sug = h("div", { class: "suggest", hidden: true });
      let timer, results = [], hl = -1;
      const show = () => {
        sug.innerHTML = "";
        if (!results.length) { sug.append(h("div", { class: "none" }, inp.value.length < 2 ? "Mindestens 2 Zeichen eingeben" : "Keine Treffer")); }
        results.forEach((p, i) => sug.append(h("button", { type: "button", class: i === hl ? "hl" : "", onmousedown: (e) => { e.preventDefault(); pick(p); } },
          h("span", { class: "person" }, h("span", { class: "avatar sm" }, initials(p.name)), h("span", null, h("b", null, p.name), h("small", null, [p.title, p.department, p.mail].filter(Boolean).join(" · ")))))));
        sug.hidden = false;
      };
      inp.addEventListener("input", () => {
        clearTimeout(timer);
        const q = inp.value.trim();
        timer = setTimeout(async () => { results = q.length >= 2 ? await searchUsers(q) : []; hl = results.length ? 0 : -1; show(); }, 220);
      });
      inp.addEventListener("keydown", (e) => {
        if (e.key === "ArrowDown") { hl = Math.min(results.length - 1, hl + 1); show(); e.preventDefault(); }
        else if (e.key === "ArrowUp") { hl = Math.max(0, hl - 1); show(); e.preventDefault(); }
        else if (e.key === "Enter") { if (results[hl]) { pick(results[hl]); } e.preventDefault(); }
        else if (e.key === "Escape") sug.hidden = true;
      });
      inp.addEventListener("blur", () => setTimeout(() => { sug.hidden = true; }, 150));
      inp.addEventListener("focus", () => { if (inp.value.length >= 2) show(); });
      box.append(inp, sug);
      if (P.me && P.me.sam && !opts.preview) {
        box.append(h("button", { type: "button", class: "linkbtn", style: "margin-top:4px", onclick: async () => {
          const r = await searchUsers(P.me.sam);
          pick(r.find((x) => x.sam === P.me.sam) || { sam: P.me.sam, name: P.me.name, mail: P.me.mail });
        } }, T("form.me")));
      }
      return box;
    }

    function initials(n) {
      const p = String(n || "?").replace(/\(.*\)/, "").trim().split(/\s+/);
      return ((p[0] || "?")[0] + (p.length > 1 ? p[p.length - 1][0] : "")).toUpperCase();
    }

    function renderGroup(f, path) {
      const lst = data[f.key] || (data[f.key] = []);
      const box = h("div", { class: "rgroup" });
      if (!lst.length) box.append(h("div", { class: "rempty" }, T("form.empty_group")));
      lst.forEach((it, i) => {
        const grid = h("div", { class: "fgrid" });
        (f.fields || []).forEach((c) => {
          const p = path + "." + i + "." + c.key;
          grid.append(wrapField(c, p, it, input(c, () => it[c.key], (v) => { it[c.key] = v; }, p)));
        });
        const title = (() => {
          const cat = (f.fields || []).find((c) => c.type === "catalog" && !c.multi && it[c.key]);
          if (cat) { const e = (catalogs[cat.catalog] || { entries: [] }).entries.find((x) => x.key === it[cat.key]); if (e) return e.label; }
          const t = (f.fields || []).find((c) => (c.type === "text" || c.type === "select") && it[c.key]);
          return t ? (t.type === "select" ? ((t.options || []).find((o) => o.value === it[t.key]) || {}).label : it[t.key]) : "";
        })();
        box.append(h("div", { class: "ritem" },
          h("div", { class: "ritem-head" }, h("span", { class: "nr" }, i + 1), h("b", null, (f.item_label || "Eintrag") + (title ? ": " + title : "")),
            i > 0 ? h("button", { type: "button", title: "nach oben", onclick: () => { lst.splice(i - 1, 0, lst.splice(i, 1)[0]); changed(true); } }, "↑") : null,
            i < lst.length - 1 ? h("button", { type: "button", title: "nach unten", onclick: () => { lst.splice(i + 1, 0, lst.splice(i, 1)[0]); changed(true); } }, "↓") : null,
            h("button", { type: "button", title: "duplizieren", onclick: () => { lst.splice(i + 1, 0, JSON.parse(JSON.stringify(it))); changed(true); } }, "⧉"),
            h("button", { type: "button", class: "del", title: "entfernen", onclick: () => { lst.splice(i, 1); changed(true); } }, "✕")),
          grid));
      });
      const full = f.max && lst.length >= f.max;
      box.append(h("button", { type: "button", class: "radd", disabled: full, onclick: () => { lst.push(newItem(f)); changed(true); setTimeout(() => { const last = box.querySelectorAll(".ritem"); const el = last[last.length - 1]; if (el) { const x = el.querySelector("input,select,textarea"); if (x) x.focus(); } }, 30); } },
        full ? "Maximum erreicht (" + f.max + ")" : "+ " + (f.add_label || T("form.add")) + (lst.length ? " (" + lst.length + (f.max ? "/" + f.max : "") + ")" : "")));
      return box;
    }

    function render() {
      const y = window.scrollY;
      vis = [];
      for (const k in elByPath) delete elByPath[k];
      root.innerHTML = "";
      root.classList.add("formapp");
      const main = h("div", { class: "fmain" });
      if (P.question) main.append(h("div", { class: "callout warn big" }, h("div", null, h("b", null, T("form.question")), h("div", { class: "pre" }, P.question), h("small", null, T("form.question_hint")))));
      if (def.intro) main.append(h("div", { class: "callout info fintro" }, def.intro));
      sections.forEach((sec) => {
        const grid = h("div", { class: "fgrid" });
        (sec.fields || []).forEach((f) => {
          if (f.type === "group") grid.append(wrapField(f, f.key, null, renderGroup(f, f.key)));
          else grid.append(wrapField(f, f.key, null, input(f, () => data[f.key], (v) => { data[f.key] = v; }, f.key)));
        });
        const card = h("section", { class: "card fsec", id: "sec-" + sec.id },
          h("div", { class: "fsec-head" }, h("span", { class: "qico", style: "--c:" + (def.color || "#1f5fbf") }, sec.icon || "•"),
            h("div", null, h("h2", null, sec.title), sec.description ? h("p", null, sec.description) : null)), grid);
        if (sec.show && sec.show.rules && sec.show.rules.length) vis.push({ el: card, cond: sec.show, item: null, sec: sec.id });
        main.append(card);
      });
      root.classList.toggle("noside", !!opts.noSide);
      root.append(main, opts.noSide ? h("span") : renderSide());
      refresh();
      window.scrollTo(0, y);
    }

    function renderSide() {
      ringEl = h("div", { class: "ring", style: "--p:0" }, h("span", null, "0%"));
      navEl = h("ul", { class: "fnav" });
      planEl = h("div", { class: "plan-box" }, h("p", { class: "muted small" }, "Wird berechnet …"));
      const parts = [h("div", { class: "card" }, h("div", { class: "fprog" }, ringEl, h("div", null, h("b", { class: "fprog-t" }, T("form.progress")), h("div", { class: "small muted fprog-s" }, ""))), navEl)];
      parts.push(h("div", { class: "card preview" }, h("h3", null, T("form.preview")), h("p", { class: "small muted", style: "margin:0" }, T("form.preview_lead")), planEl));
      if (!opts.preview) {
        const sb = h("div", { class: "card submitbar" });
        let answer;
        if (P.mode === "edit") {
          answer = h("textarea", { rows: 3, placeholder: "Ihre Antwort auf die Rückfrage (optional)" });
          sb.append(h("label", null, T("form.answer"), answer));
        }
        const btn = h("button", { class: "btn primary block", type: "button", onclick: () => submit(btn, answer) }, P.submit || "Absenden");
        sb.append(btn);
        if (P.mode === "new") sb.append(h("button", { class: "linkbtn", type: "button", onclick: () => { if (confirm("Alle Eingaben verwerfen?")) { try { localStorage.removeItem(draftKey); } catch (e) { /* */ } location.reload(); } } }, T("form.discard")),
          h("small", { class: "muted" }, T("form.draft_hint")));
        parts.push(sb);
      }
      sideEl = h("aside", { class: "fside" }, parts);
      return sideEl;
    }

    function refresh() {
      vis.forEach((v) => { v.el.hidden = !match(v.cond, data, v.item); });
      const pr = progress();
      Object.keys(elByPath).forEach((p) => {
        const w = elByPath[p];
        const msg = pr.errs[p] || serverErr[p];
        const showErr = msg && (submitted || touched.has(p) || serverErr[p]);
        w.classList.toggle("invalid", !!showErr);
        const fe = w.querySelector(":scope > .ferr");
        if (fe) fe.textContent = showErr ? msg : "";
      });
      const pct = pr.req ? Math.round((pr.ok / pr.req) * 100) : 100;
      if (ringEl) { ringEl.style.setProperty("--p", pct); ringEl.querySelector("span").textContent = pct + "%"; }
      const st = sideEl && sideEl.querySelector(".fprog-s");
      if (st) st.textContent = pr.ok + " von " + pr.req + " ausgefüllt" + (Object.keys(pr.errs).length && submitted ? " · " + Object.keys(pr.errs).length + " Hinweise" : "");
      if (navEl) {
        navEl.innerHTML = "";
        sections.forEach((sec) => {
          const s = pr.per[sec.id];
          if (!s.visible) return;
          const done = s.req > 0 ? s.ok === s.req && !s.err : false;
          const cls = s.err && submitted ? "err" : done ? "done" : "";
          navEl.append(h("li", { class: cls }, h("a", { href: "#sec-" + sec.id, onclick: (e) => { e.preventDefault(); const el = document.getElementById("sec-" + sec.id); if (el) el.scrollIntoView({ behavior: "smooth", block: "start" }); } },
            h("span", { class: "dot" }, cls === "done" ? "✓" : cls === "err" ? "!" : ""), sec.title, s.req ? h("small", null, s.ok + "/" + s.req) : null)));
        });
      }
    }

    // ---------- Vorschau des Ablaufs ----------
    let planTimer;
    function schedulePlan() {
      clearTimeout(planTimer);
      planTimer = setTimeout(loadPlan, 450);
    }
    async function loadPlan() {
      if (!planEl) return;
      try {
        const res = opts.simulate ? await opts.simulate(data) : await AW.api("POST", "/api/simulate/" + encodeURIComponent(P.key), { data });
        plan = res.plan || [];
      } catch (e) { planEl.innerHTML = ""; planEl.append(h("p", { class: "small muted" }, "Vorschau nicht verfügbar.")); return; }
      planEl.innerHTML = "";
      const levels = {};
      let skipped = 0;
      plan.forEach((p) => { if (p.skipped) { skipped++; return; } (levels[p.level] = levels[p.level] || []).push(p); });
      const ul = h("ul", { class: "plan" });
      let n = 0;
      Object.keys(levels).sort((a, b) => a - b).forEach((lv) => {
        const l = levels[lv];
        n++;
        l.forEach((p, i) => {
          ul.append(h("li", null, h("span", { class: "lvl" }, i === 0 ? n : ""), h("span", { class: "pt" },
            i === 0 && l.length > 1 ? h("span", { class: "par" }, T("form.parallel")) : null,
            h("b", null, ({ freigabe: "✔ ", aufgabe: "▢ ", info: "✉ ", warten: "⏱ " }[p.kind] || "") + p.title + (p.item ? " · " + p.item : "")),
            p.assignee && p.assignee.label ? h("small", null, p.assignee.label) : null)));
        });
      });
      if (!n) ul.append(h("li", { class: "muted" }, "Keine Schritte."));
      planEl.append(ul);
      if (skipped) planEl.append(h("p", { class: "small muted", style: "margin:6px 0 0" }, skipped + " Schritt" + (skipped > 1 ? "e entfallen" : " entfällt") + " nach Ihren Angaben."));
    }

    // ---------- Absenden ----------
    async function submit(btn, answer) {
      submitted = true;
      serverErr = {};
      const pr = progress();
      refresh();
      const errs = Object.keys(pr.errs);
      if (errs.length) {
        const first = document.querySelector(".ff.invalid");
        if (first) first.scrollIntoView({ behavior: "smooth", block: "center" });
        AW.toast(errs.length + " Angabe" + (errs.length > 1 ? "n fehlen oder sind" : " fehlt oder ist") + " ungültig.", "err");
        return;
      }
      btn.disabled = true;
      const old = btn.textContent;
      btn.textContent = T("form.sending");
      try {
        const url = P.mode === "edit" ? "/api/v/" + P.req_id + "/resubmit" : "/api/antrag/" + encodeURIComponent(P.key);
        const res = await AW.api("POST", url, P.mode === "edit" ? { data, answer: answer ? answer.value : "" } : { data });
        if (res.errors) {
          res.errors.forEach((e) => { serverErr[e.path] = e.msg; });
          refresh();
          AW.toast(T("form.check"), "err");
          const first = document.querySelector(".ff.invalid");
          if (first) first.scrollIntoView({ behavior: "smooth", block: "center" });
          btn.disabled = false; btn.textContent = old;
          return;
        }
        try { localStorage.removeItem(draftKey); } catch (e) { /* */ }
        location.href = res.url;
      } catch (e) {
        AW.toast(e.message, "err");
        btn.disabled = false; btn.textContent = old;
      }
    }

    render();
    schedulePlan();
    return { getData: () => data, setData: (d) => { data = d; render(); schedulePlan(); }, render };
  };

  const el = document.getElementById("form-data");
  const root = document.getElementById("aw-form");
  if (el && root) AW.mountForm(root, JSON.parse(el.textContent));
})();
