import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import AxeBuilder from "@axe-core/playwright";
import { test as base, expect } from "@playwright/test";

const DEMO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../docs/demo");
const TYPES = { ".html": "text/html", ".json": "application/json", ".png": "image/png", ".atlas": "text/plain", ".skel": "application/octet-stream" };

// The demo has no server of its own. Pages serves it as plain files, so this does the same.
const test = base.extend({
  demoUrl: async ({}, use) => {
    const server = http.createServer((req, res) => {
      const rel = decodeURIComponent(new URL(req.url, "http://x").pathname);
      const file = path.join(DEMO, rel.endsWith("/") ? rel + "index.html" : rel);
      if (!file.startsWith(DEMO) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) { res.writeHead(404).end(); return; }
      res.writeHead(200, { "content-type": TYPES[path.extname(file)] || "application/octet-stream" });
      fs.createReadStream(file).pipe(res);
    });
    await new Promise((r) => server.listen(0, "127.0.0.1", r));
    await use(`http://127.0.0.1:${server.address().port}/`);
    await new Promise((r) => server.close(r));
  },
});

async function ready(page, url) {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
  await page.goto(url);
  await page.waitForFunction(() => window.__demoReady === true, null, { timeout: 45_000 });
  return errors;
}

// Count of non-black pixels in the canvas. A blank stage means nothing drew.
const drawn = (page) => page.evaluate(() => {
  const c = document.getElementById("canvas");
  const snap = document.createElement("canvas");
  snap.width = c.width; snap.height = c.height;
  const g = snap.getContext("2d");
  return new Promise((resolve) => requestAnimationFrame(() => {
    g.drawImage(c, 0, 0);
    const d = g.getImageData(0, 0, snap.width, snap.height).data;
    let n = 0;
    for (let i = 0; i < d.length; i += 16) if (d[i] + d[i + 1] + d[i + 2] > 30) n++;
    resolve(n);
  }));
});

test("the demo loads two characters and draws them", async ({ page, demoUrl }) => {
  const errors = await ready(page, demoUrl);
  await expect(page.locator("#chars .row")).toHaveCount(2);
  await page.waitForTimeout(500);
  expect(await drawn(page)).toBeGreaterThan(500);
  expect(errors).toEqual([]);
});

test("switching a variant changes the selected character", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  const before = await page.locator("#chars .row button").first().textContent();
  await page.getByRole("button", { name: "Variant 3, waterfront" }).click();
  await expect(page.locator("#chars .row button").first()).not.toHaveText(before);
  await expect(page.locator("#chars .row button").first()).toContainText("Variant 3, waterfront");
});

test("an animation plays on click and previews on hover", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  const names = await page.locator("#anims button").allTextContents();
  expect(names.length).toBeGreaterThan(1);
  const other = page.locator("#anims button[aria-pressed=false]").first();
  const name = await other.textContent();
  await other.hover();
  await other.click();
  await expect(page.locator("#anims button[aria-pressed=true]")).toHaveText(name);
});

test("add and remove character", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  await page.getByRole("button", { name: "Add character" }).click();
  await expect(page.locator("#chars .row")).toHaveCount(3);
  await page.getByRole("button", { name: /^Remove/ }).last().click();
  await expect(page.locator("#chars .row")).toHaveCount(2);
});

test("wheel scales and drag moves the selected character", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  const box = await page.locator("#stage").boundingBox();
  const before = await drawn(page);
  await page.mouse.move(box.x + box.width * 0.5, box.y + box.height * 0.5);
  await page.mouse.wheel(0, -400);
  await page.waitForTimeout(300);
  expect(await drawn(page)).not.toBe(before);
});

test("the notice and links are on the page", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  await expect(page.getByText("property of SHIFT UP Corporation")).toBeVisible();
  await expect(page.getByText("SHIFT UP does not sponsor, endorse, or affiliate with it")).toBeVisible();
  await expect(page.getByRole("link", { name: "Spinewall on GitHub" })).toHaveAttribute("href", "https://github.com/officialdad/spinewall");
  await expect(page.getByRole("link", { name: "README" })).toBeVisible();
});

test("the demo has no serious or critical WCAG AA violations", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  await page.emulateMedia({ reducedMotion: "reduce" });
  const { violations } = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  const blocking = violations.filter((v) => ["serious", "critical"].includes(v.impact));
  expect(blocking.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(" | ")}`)).toEqual([]);
});

test("the demo fits a phone width", async ({ page, demoUrl }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await ready(page, demoUrl);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});
