const { test, expect } = require("@playwright/test");

const BASE = process.env.BASE_URL || "https://openrmslist.ku0hn.radio";

test("deployed UI loads with map tiles and data", async ({ page }) => {
  const consoleErrors = [];
  const badTileResponses = [];

  page.on("console", (m) => {
    if (m.type() === "error") consoleErrors.push(m.text());
  });
  page.on("response", (r) => {
    const u = r.url();
    if (/tile|basemap|cartocdn|openstreetmap/i.test(u) && r.status() >= 400) {
      badTileResponses.push(`${r.status()} ${u}`);
    }
  });

  await page.goto(BASE, { waitUntil: "networkidle", timeout: 60_000 });

  // The app must report that it loaded gateways from the API.
  await expect(page.locator("#status")).toContainText("gateways", { timeout: 30_000 });

  // Keyless tiles must actually load (no "API key required" placeholder/403).
  await expect
    .poll(() => page.locator("img.leaflet-tile-loaded").count(), { timeout: 30_000 })
    .toBeGreaterThan(0);

  // The station list should be populated.
  expect(await page.locator("#list li").count()).toBeGreaterThan(0);

  expect(badTileResponses, badTileResponses.join("\n")).toEqual([]);

  // Cloudflare injects its analytics beacon into proxied HTML; our strict CSP
  // blocks it (by design). Ignore only that known third-party script.
  const benign = (m) => /cloudflareinsights\.com|beacon\.min\.js/i.test(m);
  const realErrors = consoleErrors.filter((m) => !benign(m));
  expect(realErrors, realErrors.join("\n")).toEqual([]);
});

test("deployed API endpoints respond", async ({ request }) => {
  for (const p of ["/api/health", "/api/config", "/api/servicecodes", "/api/conditions", "/api/gateways"]) {
    const res = await request.get(BASE + p);
    expect(res.ok(), `${p} -> ${res.status()}`).toBeTruthy();
  }
  const health = await (await request.get(BASE + "/api/health")).json();
  expect(health.ok).toBe(true);
});

test("client setup guide is served", async ({ page }) => {
  const res = await page.goto(BASE + "/clients.html", { waitUntil: "domcontentloaded" });
  expect(res.ok()).toBeTruthy();
  await expect(page.locator("h1")).toContainText("Configuring service codes");
  for (const name of ["Winlink Express", "PAT", "WoAD", "RadioMail", "HTCommander"]) {
    await expect(page.locator("h2", { hasText: name })).toHaveCount(1);
  }
});
