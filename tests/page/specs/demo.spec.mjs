import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import AxeBuilder from "@axe-core/playwright";
import { test as base, expect } from "@playwright/test";

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const TYPES = { ".html": "text/html", ".js": "text/javascript", ".json": "application/json", ".png": "image/png", ".atlas": "text/plain", ".skel": "application/octet-stream" };

// The demo is built from the real page, and Pages serves it as plain files, so this does the same.
const test = base.extend({
  demoUrl: async ({}, use) => {
    const DEMO = fs.mkdtempSync(path.join(os.tmpdir(), "spinewall-demo-"));
    execFileSync("python3", ["-I", path.join(REPO, "scripts/build-demo.py"), DEMO]);
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
    fs.rmSync(DEMO, { recursive: true, force: true });
  },
});

async function ready(page, url) {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
  await page.goto(url);
  await page.waitForFunction(() => document.querySelector("#p canvas, #layers canvas") && models.length > 0, null, { timeout: 45_000 });
  return errors;
}

// Count of non-black pixels in the canvas. A blank stage means nothing drew.
const drawn = (page) => page.evaluate(() => {
  const c = document.querySelector("#p canvas, #layers canvas");
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

const openEdit = (page) => page.getByRole("button", { name: "Edit" }).click();

test("the demo is the real page and draws a character", async ({ page, demoUrl }) => {
  const errors = await ready(page, demoUrl);
  await expect.poll(() => drawn(page)).toBeGreaterThan(500);
  expect(errors).toEqual([]);
});

test("a click changes the animation", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  await expect.poll(() => page.evaluate(async () => (await api("/state")).json().then((s) => s.animation))).toBeTruthy();
  const before = await page.evaluate(async () => (await (await api("/state")).json()).animation);
  await page.mouse.click(640, 400);
  await expect.poll(() => page.evaluate(async () => (await (await api("/state")).json()).animation)).not.toBe(before);
});

test("edit mode opens the picker and a second character can be added", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  await openEdit(page);
  await expect(page.locator("#picker")).toBeVisible();
  await page.getByRole("button", { name: "Add character" }).click();
  await expect(page.locator("#chars .charrow")).toHaveCount(2);
  await page.getByRole("button", { name: /^Remove/ }).last().click();
  await expect(page.locator("#chars")).toBeHidden();
});

test("the notice and links are on the page", async ({ page, demoUrl }) => {
  await ready(page, demoUrl);
  await expect(page.getByText("property of SHIFT UP Corporation")).toBeVisible();
  await expect(page.getByText("SHIFT UP does not sponsor, endorse, or affiliate with it")).toBeVisible();
  await expect(page.getByRole("link", { name: "Spinewall on GitHub" })).toHaveAttribute("href", "https://github.com/officialdad/spinewall");
  await expect(page.getByRole("link", { name: "README" })).toBeVisible();
});

for (const edit of [false, true]) {
  test(`the demo has no serious or critical WCAG AA violations${edit ? " in edit mode" : ""}`, async ({ page, demoUrl }) => {
    await ready(page, demoUrl);
    if (edit) { await openEdit(page); await expect(page.locator("#picker")).toBeVisible(); }
    await page.emulateMedia({ reducedMotion: "reduce" });
    const { violations } = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
      .analyze();
    const blocking = violations.filter((v) => ["serious", "critical"].includes(v.impact));
    expect(blocking.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(" | ")}`)).toEqual([]);
  });
}

test("the demo fits a phone width", async ({ page, demoUrl }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await ready(page, demoUrl);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
});
