// Antragswerk – gemeinsame Oberfläche
"use strict";
(function () {
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => Array.from(el.querySelectorAll(s));
  const AW = (window.AW = window.AW || {});
  AW.csrf = document.body.dataset.csrf || "";
  try { AW.texts = JSON.parse(document.getElementById("aw-texts").textContent) || {}; } catch (e) { AW.texts = {}; }
  AW.t = (k, ...a) => { let s = AW.texts[k] != null ? AW.texts[k] : k; a.forEach((x) => { s = s.replace(/%[sd]/, x); }); return s; };

  // ---------- Darstellung (Hell/Dunkel, Akzentfarbe) ----------
  const store = (k, v) => { try { if (v === undefined) return localStorage.getItem(k); if (v === null) localStorage.removeItem(k); else localStorage.setItem(k, v); } catch (e) { /* */ } return null; };
  const html = document.documentElement;
  function applyMode(m) {
    if (m === "light" || m === "dark") html.setAttribute("data-theme", m); else html.removeAttribute("data-theme");
    $$("[data-mode-switch] button").forEach((b) => b.classList.toggle("on", b.dataset.mode === m));
  }
  function applyAccent(k) {
    const old = document.getElementById("aw-user-theme");
    if (old) old.remove();
    let presets = {};
    try { presets = JSON.parse(document.getElementById("aw-presets").textContent); } catch (e) { /* */ }
    const p = k && presets[k];
    if (p) {
      const st = document.createElement("style");
      st.id = "aw-user-theme";
      st.textContent = "html:root{" + p[0] + "}html:root[data-theme=dark]{" + p[1] + "}@media (prefers-color-scheme: dark){html:root:not([data-theme=light]){" + p[1] + "}}";
      document.head.append(st);
    }
    $$("[data-accent-switch] button").forEach((b) => b.classList.toggle("on", (b.dataset.accent || "") === (k || "")));
  }
  const ms = $("[data-mode-switch]");
  if (ms) {
    applyMode(store("aw-mode") || html.dataset.mode || "auto");
    ms.addEventListener("click", (e) => { const b = e.target.closest("button"); if (!b) return; store("aw-mode", b.dataset.mode); applyMode(b.dataset.mode); });
  }
  const as = $("[data-accent-switch]");
  if (as) {
    let presets = {};
    try { presets = JSON.parse(document.getElementById("aw-presets").textContent); } catch (e) { /* */ }
    Object.keys(presets).forEach((k) => {
      const m = presets[k][0].match(/--accent:(#[0-9a-f]+)/i);
      const b = document.createElement("button");
      b.type = "button"; b.dataset.accent = k; b.title = k; b.style.background = m ? m[1] : "#888";
      as.append(b);
    });
    applyAccent(store("aw-accent") || "");
    as.addEventListener("click", (e) => { const b = e.target.closest("button"); if (!b) return; store("aw-accent", b.dataset.accent || null); applyAccent(b.dataset.accent); });
  }

  // ---------- Toasts ----------
  AW.toast = function (msg, kind) {
    const box = $("#toasts");
    if (!box) return;
    const t = document.createElement("div");
    t.className = "toast " + (kind || "");
    t.textContent = msg;
    box.append(t);
    setTimeout(() => { t.style.opacity = "0"; t.style.transition = "opacity .3s"; }, 3200);
    setTimeout(() => t.remove(), 3600);
  };

  AW.api = async function (method, url, body) {
    const res = await fetch(url, {
      method, credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF": AW.csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    let data = {};
    try { data = await res.json(); } catch (e) { /* leer */ }
    if (!res.ok && !data.errors) throw new Error(data.error || "Fehler " + res.status);
    data._status = res.status;
    return data;
  };

  // ---------- Syntaxhervorhebung (PowerShell + Vorlagen) ----------
  const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const KW = new Set(["if", "else", "elseif", "foreach", "for", "while", "do", "switch", "function", "param", "return", "throw", "try", "catch", "finally", "in", "break", "continue", "begin", "process", "end", "range", "with", "define", "template", "block"]);
  AW.highlight = function (code, lang) {
    if (lang && lang !== "powershell" && lang !== "tpl") return esc(code);
    const re = /(\{\{[\s\S]*?\}\})|(<#[\s\S]*?#>|#[^\n]*)|('(?:[^']|'')*'|"(?:[^"`]|`.)*")|(\$[\w:]+|\$\{[^}]+\})|(\b[A-Z][a-zA-Z]+-[A-Z][A-Za-z]+\b)|(\s-[A-Za-z][\w]*)|(\b\d+(?:\.\d+)?\b)|(\b[a-zA-Z]+\b)/g;
    let out = "", last = 0, m;
    while ((m = re.exec(code))) {
      out += esc(code.slice(last, m.index));
      const t = m[0];
      if (m[1]) out += '<span class="tk-t">' + esc(t) + "</span>";
      else if (m[2]) out += '<span class="tk-c">' + esc(t) + "</span>";
      else if (m[3]) out += '<span class="tk-s">' + esc(t) + "</span>";
      else if (m[4]) out += '<span class="tk-v">' + esc(t) + "</span>";
      else if (m[5]) out += '<span class="tk-m">' + esc(t) + "</span>";
      else if (m[6]) out += '<span class="tk-p">' + esc(t) + "</span>";
      else if (m[7]) out += '<span class="tk-n">' + esc(t) + "</span>";
      else if (m[8] && KW.has(t.toLowerCase())) out += '<span class="tk-k">' + esc(t) + "</span>";
      else out += esc(t);
      last = m.index + t.length;
    }
    return out + esc(code.slice(last));
  };
  $$("pre.code code").forEach((c) => { c.innerHTML = AW.highlight(c.textContent, c.parentElement.dataset.lang); });

  // ---------- Kopieren ----------
  async function copyText(text) {
    try { await navigator.clipboard.writeText(text); return true; } catch (e) {
      const ta = document.createElement("textarea");
      ta.value = text; ta.style.position = "fixed"; ta.style.opacity = "0";
      document.body.append(ta); ta.select();
      let ok = false;
      try { ok = document.execCommand("copy"); } catch (e2) { ok = false; }
      ta.remove();
      return ok;
    }
  }
  AW.copy = copyText;
  document.addEventListener("click", async (e) => {
    const b = e.target.closest("[data-copy]");
    if (!b) return;
    const src = $(b.dataset.copy);
    if (!src) return;
    if (await copyText(src.textContent)) {
      b.classList.add("copied");
      const old = b.textContent;
      b.textContent = "✓";
      setTimeout(() => { b.classList.remove("copied"); b.textContent = old; }, 1600);
    } else AW.toast("Kopieren nicht möglich – bitte manuell markieren.", "err");
  });

  // ---------- Allgemeines ----------
  document.addEventListener("click", (e) => {
    const t = e.target.closest("[data-toggle]");
    if (t) { const el = $(t.dataset.toggle); if (el) el.classList.toggle("open"); }
    const p = e.target.closest("[data-print]");
    if (p) window.print();
    const row = e.target.closest("tr[data-href]");
    if (row && !e.target.closest("a,button")) location.href = row.dataset.href;
    const c = e.target.closest("[data-confirm]");
    if (c && !confirm(c.dataset.confirm)) e.preventDefault();
    // offene Dropdowns schließen
    $$("details.dropdown[open], details.usermenu[open]").forEach((d) => { if (!d.contains(e.target)) d.open = false; });
  });
  $$("[data-autosubmit]").forEach((s) => s.addEventListener("change", () => s.form.submit()));
  $$("[data-filter]").forEach((inp) => {
    inp.addEventListener("input", () => {
      const q = inp.value.trim().toLowerCase();
      $$(inp.dataset.filter).forEach((el) => { el.hidden = q && !(el.dataset.text || el.textContent).toLowerCase().includes(q); });
      $$(".grouphead").forEach((h) => { const g = h.nextElementSibling; h.hidden = g && $$(inp.dataset.filter, g).every((x) => x.hidden); });
    });
  });

  // ---------- Reiter ----------
  $$("[data-tabs]").forEach((box) => {
    const btns = $$("[data-tab]", box);
    btns.forEach((b) => b.addEventListener("click", () => {
      btns.forEach((x) => x.classList.toggle("on", x === b));
      $$("[data-pane]", box).forEach((p) => { p.hidden = p.dataset.pane !== b.dataset.tab; });
    }));
  });

  // ---------- Schritt-Aktionen ----------
  $$("[data-act-form]").forEach((f) => {
    f.addEventListener("click", (e) => {
      const b = e.target.closest("button[name=action]");
      if (!b) return;
      const cmt = f.querySelector("textarea[name=comment]");
      if (b.dataset.needComment && cmt && !cmt.value.trim()) {
        e.preventDefault();
        cmt.focus();
        cmt.classList.add("shake");
        setTimeout(() => cmt.classList.remove("shake"), 400);
        AW.toast(b.dataset.needComment, "err");
        return;
      }
      if (b.hasAttribute("data-check-list")) {
        const open = $$(".checklist input[type=checkbox]", f).filter((x) => !x.checked);
        if (open.length && !b.dataset.confirmed) {
          e.preventDefault();
          b.dataset.confirmed = "1";
          b.textContent = "Trotzdem erledigen (" + open.length + " Punkte offen)";
          b.classList.add("shake");
          setTimeout(() => b.classList.remove("shake"), 400);
          return;
        }
      }
      if (b.value === "ablehnen" && !confirm(AW.t("step.reject_confirm"))) e.preventDefault();
    });
  });

  // ---------- Ablaufkarte ----------
  const fm = $("#flowmap");
  if (fm) {
    const svg = $("svg.edges", fm);
    let edges = [];
    try { edges = JSON.parse(fm.dataset.edges || "[]") || []; } catch (e) { edges = []; }
    const NS = "http://www.w3.org/2000/svg";
    function draw() {
      svg.innerHTML = "";
      svg.setAttribute("width", fm.scrollWidth);
      svg.setAttribute("height", fm.scrollHeight);
      const base = fm.getBoundingClientRect();
      const vis = (el) => el.offsetParent !== null;
      const cards = (id) => $$('.fcard[data-step="' + id + '"]', fm).filter(vis);
      edges.forEach((ed) => {
        const from = cards(ed.from), to = cards(ed.to);
        if (!from.length || !to.length) return;
        // je Quell- und Zielschritt nur die mittlere Karte verbinden (Stapel bei "je Eintrag")
        const pick = (l) => {
          const r = l.map((c) => c.getBoundingClientRect());
          const top = Math.min(...r.map((x) => x.top)), bot = Math.max(...r.map((x) => x.bottom));
          return { x1: r[0].left, x2: r[0].right, y: (top + bot) / 2 };
        };
        const a = pick(from), b = pick(to);
        const x1 = a.x2 - base.left + fm.scrollLeft, y1 = a.y - base.top + fm.scrollTop;
        const x2 = b.x1 - base.left + fm.scrollLeft, y2 = b.y - base.top + fm.scrollTop;
        const dx = Math.max(24, (x2 - x1) / 2);
        const p = document.createElementNS(NS, "path");
        p.setAttribute("d", `M${x1} ${y1} C${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`);
        const done = from.every((c) => c.classList.contains("s-ok"));
        if (done) p.classList.add("done");
        p.dataset.from = ed.from; p.dataset.to = ed.to;
        svg.append(p);
      });
    }
    function select(id, scroll) {
      $$(".fcard", fm).forEach((c) => c.classList.toggle("sel", c.dataset.card === String(id) || (!c.dataset.card && c.dataset.step === id)));
      let shown = false;
      $$("[data-panel]").forEach((p) => { const on = p.dataset.panel === String(id); p.hidden = !on; shown = shown || on; });
      $$("[data-panel-step]").forEach((p) => { const on = !shown && p.dataset.panelStep === id; p.hidden = !on; shown = shown || on; });
      const empty = $("#step-empty");
      if (empty) empty.hidden = shown;
      $$("path", svg).forEach((p) => p.classList.remove("hi"));
      const card = $$(".fcard.sel", fm)[0];
      if (card) $$('path[data-from="' + card.dataset.step + '"], path[data-to="' + card.dataset.step + '"]', svg).forEach((p) => p.classList.add("hi"));
      if (scroll && shown) {
        const panel = $("[data-panel]:not([hidden]), [data-panel-step]:not([hidden])");
        if (panel && panel.getBoundingClientRect().top > window.innerHeight * 0.6) panel.scrollIntoView({ behavior: "smooth", block: "start" });
      }
    }
    fm.addEventListener("click", (e) => {
      const c = e.target.closest(".fcard");
      if (!c) return;
      const id = c.dataset.card || c.dataset.step;
      select(id, true);
      if (c.dataset.card) history.replaceState(null, "", "?s=" + c.dataset.card);
    });
    $$("[data-open-card]").forEach((a) => a.addEventListener("click", (e) => { e.preventDefault(); select(a.dataset.openCard, true); history.replaceState(null, "", "?s=" + a.dataset.openCard); }));
    const tg = $("[data-toggle-skipped]");
    if (tg) tg.addEventListener("change", () => { fm.classList.toggle("show-skipped", tg.checked); draw(); });
    window.addEventListener("resize", draw);
    draw();
    if (fm.dataset.sel && fm.dataset.sel !== "0") {
      select(fm.dataset.sel, false);
      const c = $(".fcard.sel", fm);
      if (c) fm.scrollLeft = Math.max(0, c.offsetLeft - fm.clientWidth / 2 + c.offsetWidth / 2);
      draw();
    }
  }

  // ---------- Hauptmenü: was nicht passt, wandert in „Mehr ▾“ ----------
  const nav = $("#mainnav"), more = $(".navmore");
  if (nav && more) {
    const menu = $(".navmore-menu", more);
    const fit = () => {
      while (menu.firstChild) nav.appendChild(menu.firstChild); // alles zurück
      more.hidden = true; more.classList.remove("active"); more.open = false;
      if (getComputedStyle($(".navtoggle") || nav).display !== "none" && $(".navtoggle")) return; // schmal: Burger-Menü
      const items = () => [...nav.children].filter((x) => x.tagName === "A" || x.classList.contains("navsep"));
      if (nav.scrollWidth <= nav.clientWidth + 1) return;
      more.hidden = false;
      while (nav.scrollWidth > nav.clientWidth + 1 && items().length > 1) menu.prepend(items().pop());
      while (menu.firstChild && menu.firstChild.classList.contains("navsep")) nav.appendChild(menu.firstChild);
      const tail = items().pop();
      if (tail && tail.classList.contains("navsep")) menu.prepend(tail);
      more.classList.toggle("active", !!$("a.active", menu));
    };
    let raf = 0;
    window.addEventListener("resize", () => { cancelAnimationFrame(raf); raf = requestAnimationFrame(fit); });
    if (document.fonts && document.fonts.ready) document.fonts.ready.then(fit);
    fit();
    document.addEventListener("click", (e) => { if (more.open && !more.contains(e.target)) more.open = false; });
  }

  // ---------- Live-Aktualisierung ----------
  // Browser erlauben je Server nur 6 gleichzeitige HTTP/1.1-Verbindungen. Hielte jeder Tab eine eigene
  // Live-Verbindung offen, hinge ab ~6 Tabs jede weitere Seite. Deshalb: genau EIN Tab hält die Verbindung
  // (Web Locks), alle anderen bekommen die Meldungen per BroadcastChannel. Ohne Web Locks (http:// außer
  // localhost) verbinden sich nur sichtbare Tabs.
  if (document.body.dataset.live && window.EventSource && !document.getElementById("designer") && !document.getElementById("master")) {
    const badge = $("#livebadge");
    let pending = false;
    const busy = () => {
      const a = document.activeElement;
      if (a && /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName)) return true;
      return $$("textarea").some((t) => t.value.trim() !== "") || $$("details.dropdown[open]").length > 0 || $("#aw-form") !== null;
    };
    const reloadSoon = () => {
      if (pending) return;
      pending = true;
      const tryIt = () => { if (busy()) { setTimeout(tryIt, 3000); return; } location.reload(); };
      setTimeout(tryIt, 600);
    };
    const handle = (m) => {
      if (!m || m.type !== "request") return;
      const path = location.pathname;
      const vm = path.match(/^\/v\/(\d+)$/);
      if (vm) { if (+vm[1] === m.id) { AW.toast("Vorgang wurde aktualisiert …"); reloadSoon(); } return; }
      if (path === "/" || path === "/aufgaben" || path === "/vorgaenge") reloadSoon();
    };
    const setBadge = (on) => badge && badge.classList.toggle("on", !!on);
    const bc = "BroadcastChannel" in window ? new BroadcastChannel("aw-live") : null;
    let es = null, leader = false;
    const open = () => {
      if (es) return;
      es = new EventSource("/live");
      es.onopen = () => { setBadge(true); bc && bc.postMessage({ k: "state", on: true }); };
      es.onerror = () => { setBadge(false); bc && bc.postMessage({ k: "state", on: false }); };
      es.onmessage = (ev) => {
        let m = null;
        try { m = JSON.parse(ev.data); } catch (e) { return; }
        handle(m);
        bc && bc.postMessage({ k: "msg", m });
      };
    };
    const close = () => { if (es) { es.close(); es = null; } };
    if (bc) {
      bc.onmessage = (e) => {
        const d = e.data || {};
        if (d.k === "msg") handle(d.m);
        else if (d.k === "state") setBadge(d.on);
        else if (d.k === "hello" && es) bc.postMessage({ k: "state", on: es.readyState === 1 });
      };
    }
    window.addEventListener("pagehide", close);
    if (navigator.locks && bc) {
      const elect = () => navigator.locks.request("aw-live", () => new Promise((release) => {
        leader = true; open();
        window.addEventListener("pagehide", () => { close(); leader = false; release(); }, { once: true });
      }));
      elect();
      bc.postMessage({ k: "hello" });
      window.addEventListener("pageshow", (e) => { if (e.persisted && !leader) elect(); }); // aus dem Zurück-Cache
    } else {
      const sync = () => (document.visibilityState === "visible" ? open() : close());
      document.addEventListener("visibilitychange", sync);
      window.addEventListener("pageshow", sync);
      sync();
      bc && bc.postMessage({ k: "hello" });
    }
  }
})();
