// Farbschema früh setzen (vor dem ersten Zeichnen): Hell/Dunkel und ggf. persönliche Akzentfarbe.
"use strict";
(function () {
  var html = document.documentElement;
  var get = function (k) { try { return localStorage.getItem(k); } catch (e) { return null; } };
  var allow = html.getAttribute("data-user-theme") === "1";
  var mode = (allow && get("aw-mode")) || html.getAttribute("data-mode") || "auto";
  if (mode === "light" || mode === "dark") html.setAttribute("data-theme", mode);
  var acc = allow && get("aw-accent");
  if (acc) {
    try {
      var p = JSON.parse(document.getElementById("aw-presets").textContent)[acc];
      if (p) {
        var st = document.createElement("style");
        st.id = "aw-user-theme";
        st.textContent = "html:root{" + p[0] + "}html:root[data-theme=dark]{" + p[1] + "}@media (prefers-color-scheme: dark){html:root:not([data-theme=light]){" + p[1] + "}}";
        document.head.appendChild(st);
      }
    } catch (e) { /* ohne */ }
  }
})();
