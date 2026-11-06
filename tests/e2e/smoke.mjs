// Rauchtest der Oberfläche gegen einen laufenden Demo-Server (Standard: http://localhost:8080).
//
//   node tests/e2e/smoke.mjs                 # lokal, Demo-Server muss laufen
//   AW_URL=http://host:8080 node tests/e2e/smoke.mjs
//
// Prüft:
//   1. jeder Menüpunkt ist klickbar (auch wenn er ins „Mehr ▾“-Menü gerutscht ist) und lädt schnell
//   2. mit 10 offenen Tabs hängt keine Navigation (Live-Verbindung wird geteilt, Browser-Limit 6/Server)
//   3. Live-Aktualisierung kommt in allen Tabs an
//   4. keine JavaScript-Fehler
import { chromium } from "playwright";

const B = process.env.AW_URL || "http://localhost:8080";
const LIMIT_MS = Number(process.env.AW_NAV_LIMIT_MS || 3000);
const PAGES = ["/", "/neu", "/aufgaben", "/vorgaenge", "/designer", "/stammdaten", "/einstellungen", "/postausgang", "/protokoll", "/"];

const errors = [];
const fail = (msg) => { errors.push(msg); console.error("✗ " + msg); };

async function login(ctx, user) {
  const p = await ctx.newPage();
  p.on("pageerror", (e) => errors.push(`${user}: ${e.message}`));
  p.on("console", (m) => { if (m.type() === "error") errors.push(`${user}: ${m.text()}`); });
  await p.goto(B + "/login");
  await p.fill("input[name=username]", user);
  await p.fill("input[name=password]", "demo");
  await p.click('form[action="/login"] button.primary');
  await p.waitForLoadState("load");
  return p;
}

async function navigate(p, label) {
  const times = [];
  for (const path of PAGES) {
    const t0 = Date.now();
    const link = p.locator(`#mainnav a[href="${path}"]`);
    try {
      if (await link.isVisible()) {
        await link.click({ timeout: LIMIT_MS });
      } else {
        await p.click(".navmore > summary", { timeout: LIMIT_MS });
        await p.click(`.navmore-menu a[href="${path}"]`, { timeout: LIMIT_MS });
      }
      await p.waitForURL((u) => new URL(u).pathname === path, { timeout: LIMIT_MS });
      await p.waitForLoadState("load", { timeout: LIMIT_MS });
    } catch (e) {
      fail(`${label}: ${path} nicht innerhalb ${LIMIT_MS} ms erreichbar (${e.message.split("\n")[0]})`);
      await p.goto(B + path).catch(() => {});
    }
    times.push(`${path}=${Date.now() - t0}ms`);
  }
  console.log(`${label}: ${times.join(" ")}`);
}

const browser = await chromium.launch();
try {
  for (const width of [1500, 1280, 1150]) {
    const ctx = await browser.newContext({ viewport: { width, height: 900 } });
    const p = await login(ctx, "admin");
    await navigate(p, `Menü ${width}px`);
    await ctx.close();
  }

  // viele Tabs + Live-Aktualisierung
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } });
  const main = await login(ctx, "admin");
  const tabs = [main];
  for (let i = 0; i < 9; i++) {
    const t = await ctx.newPage();
    await t.goto(B + "/vorgaenge");
    tabs.push(t);
  }
  await main.bringToFront();
  await navigate(main, "10 Tabs");
  for (const t of tabs) {
    if (new URL(t.url()).pathname !== "/vorgaenge") await t.goto(B + "/vorgaenge");
    await t.evaluate(() => { window.__aw_mark = 1; });
  }
  await main.waitForTimeout(800);

  // anderer Benutzer stellt einen Antrag → alle Tabs der Vorgangsliste laden neu
  const other = await login(await browser.newContext(), "a.becker");
  await other.goto(B + "/neu/eintritt");
  const f = (path) => `.ff[data-path="${path}"]`;
  await other.selectOption(f("anrede") + " select", "herr");
  await other.fill(f("vorname") + " input", "Erika");
  await other.fill(f("nachname") + " input", "Rauchtest");
  await other.fill(f("personalnummer") + " input", "100001");
  await other.fill(f("eintritt") + " input", "2030-01-01");
  await other.selectOption(f("abteilung") + " select", "32");
  await other.fill(f("funktion") + " input", "Sachbearbeitung");
  await other.fill(f("vorgesetzter") + " input", "schulz");
  await other.waitForTimeout(700);
  await other.keyboard.press("Enter");
  await other.selectOption(f("raum") + " select", { index: 1 });
  await other.click('button:has-text("Antrag stellen")');
  await other.waitForURL(/\/v\/\d+/, { timeout: 10000 });
  await main.waitForTimeout(3000);
  let reloaded = 0;
  for (const t of tabs) if (await t.evaluate(() => window.__aw_mark !== 1)) reloaded++;
  console.log(`Live: ${reloaded}/${tabs.length} Tabs aktualisiert`);
  if (reloaded !== tabs.length) fail(`Live-Aktualisierung nur in ${reloaded}/${tabs.length} Tabs angekommen`);
} finally {
  await browser.close();
}

if (errors.length) {
  console.error(`\n${errors.length} Problem(e):\n` + errors.join("\n"));
  process.exit(1);
}
console.log("\n✓ Rauchtest bestanden – keine JavaScript-Fehler");
