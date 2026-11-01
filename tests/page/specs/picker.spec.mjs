import { test, expect, get, post, reported } from "../lib/fixture.mjs";
import { SOLO, VARIANTS, BULK_COUNT } from "../lib/models.mjs";

const A = SOLO["4.0"];
const cells = (page) => page.locator("#list .cell");
const setSize = (page) => cells(page).first().getAttribute("aria-setsize").then(Number);

async function pickerOpen(open, page) {
  await open({ model: A, edit: true });
  await expect(page.locator("#picker")).toBeVisible();
  await expect(cells(page).first()).toBeVisible();
}

test("the picker opens in edit mode and hides in normal mode", async ({ open, page }) => {
  await open({ model: A });
  await expect(page.locator("#picker")).toBeHidden();
  await get("/edit/1");
  await expect(page.locator("#picker")).toBeVisible();
  await get("/edit/0");
  await expect(page.locator("#picker")).toBeHidden();
});

test("the list windows its cells", async ({ open, page }) => {
  await pickerOpen(open, page);
  const total = await setSize(page);
  expect(total).toBeGreaterThan(BULK_COUNT);
  expect(await cells(page).count()).toBeLessThan(total);
});

test("the Characters, Scenes, and All filter changes the list", async ({ open, page }) => {
  await pickerOpen(open, page);
  const characters = await setSize(page);
  const kinds = page.getByRole("group", { name: "Model kind" });
  await kinds.getByRole("button", { name: /^Scenes/ }).click();
  await expect(kinds.getByRole("button", { name: /^Scenes/ })).toHaveAttribute("aria-pressed", "true");
  await expect.poll(() => setSize(page)).toBe(2);
  await kinds.getByRole("button", { name: /^All/ }).click();
  await expect.poll(() => setSize(page)).toBe(characters + 2);
  await kinds.getByRole("button", { name: /^Characters/ }).click();
  await expect.poll(() => setSize(page)).toBe(characters);
});

test("fuzzy search narrows the list and Escape clears it", async ({ open, page }) => {
  await pickerOpen(open, page);
  const search = page.getByRole("combobox", { name: "Search models" });
  const all = await setSize(page);
  await search.fill("slo");
  await expect.poll(() => setSize(page)).toBeLessThan(all);
  await expect(cells(page).first()).toContainText("Solo");
  await search.fill("ols4");
  await expect(cells(page).filter({ hasText: "Solo 4.0" })).toHaveCount(0);
  await search.fill("zzzzqq");
  await expect(page.locator("#list .cell")).toHaveCount(0);
  await expect(page.getByText("No models match.")).toBeVisible();
  await search.press("Escape");
  await expect(search).toHaveValue("");
  await expect.poll(() => setSize(page)).toBe(all);
});

test("a fuzzy match is typo tolerant in order, so letters need not touch", async ({ open, page }) => {
  await pickerOpen(open, page);
  await page.getByRole("combobox", { name: "Search models" }).fill("sl42");
  await expect(cells(page).first()).toContainText("Solo 4.2");
});

test("Alt+F toggles a favorite and the server keeps it", async ({ open, page }) => {
  await pickerOpen(open, page);
  const search = page.getByRole("combobox", { name: "Search models" });
  await search.fill("Solo 4.1");
  await expect(cells(page).first()).toContainText("Solo 4.1");
  await search.press("Alt+f");
  await expect.poll(async () => (await get("/prefs")).favorites).toContain(SOLO["4.1"]);
  await expect(page.getByRole("option", { name: /^Solo 4\.1.*, favorite$/ })).toBeVisible();
  await search.press("Alt+f");
  await expect.poll(async () => (await get("/prefs")).favorites).not.toContain(SOLO["4.1"]);
});

test("a click on the star favorites the model", async ({ open, page }) => {
  await pickerOpen(open, page);
  await page.getByRole("combobox", { name: "Search models" }).fill("Solo 4.1");
  const cell = cells(page).first();
  await expect(cell).toContainText("Solo 4.1");
  await cell.locator(".star").click();
  await expect.poll(async () => (await get("/prefs")).favorites).toContain(SOLO["4.1"]);
  await expect(cell.locator(".star")).toHaveClass(/on/);
});

test("a favorite survives a reload", async ({ open, page }) => {
  await pickerOpen(open, page);
  await post("/fav/" + SOLO["4.2"], {});
  await page.reload();
  await expect(page.getByRole("option", { name: /^Solo 4\.2.*, favorite$/ })).toBeVisible();
});

test("variants group under one character and expand", async ({ open, page }) => {
  await pickerOpen(open, page);
  const search = page.getByRole("combobox", { name: "Search models" });
  await search.fill("Alice");
  const group = cells(page).filter({ hasText: `${VARIANTS.length} variants` });
  await expect(group).toHaveCount(1);
  await expect(group).toHaveAttribute("aria-label", /hidden/);
  await search.press("Shift+Enter");
  await expect(group).toHaveAttribute("aria-label", /shown/);
  await expect(page.getByRole("listbox", { name: `Models, ${VARIANTS.length + 1} shown` })).toBeVisible();
});

test("the Variants toggle lists each variant on its own", async ({ open, page }) => {
  await pickerOpen(open, page);
  const search = page.getByRole("combobox", { name: "Search models" });
  await search.fill("Alice");
  await expect(cells(page).filter({ hasText: "variants" })).toHaveCount(1);
  await page.getByRole("button", { name: "Variants" }).click();
  await expect(cells(page).filter({ hasText: "variants" })).toHaveCount(0);
  await expect(cells(page)).toHaveCount(VARIANTS.length);
  await expect.poll(async () => (await get("/prefs")).variants).toBe(false);
});

test("arrow keys move the highlight and Enter plays", async ({ open, page }) => {
  await pickerOpen(open, page);
  const search = page.getByRole("combobox", { name: "Search models" });
  await search.fill("Solo");
  await expect(cells(page)).toHaveCount(4);
  await search.press("ArrowDown");
  await search.press("ArrowDown");
  const active = page.locator("#list .cell.active");
  await expect(active).toHaveCount(1);
  const name = (await active.locator(".name").textContent()).trim();
  await search.press("Enter");
  const id = Object.values(SOLO).find((s) => name.endsWith(s.slice(1, 3).replace(/^(\d)(\d)$/, "$1.$2")));
  await expect.poll(async () => (await get("/state")).model).toBe(id);
});

test("Ctrl+Enter adds the highlighted model as a character", async ({ open, page }) => {
  await pickerOpen(open, page);
  const search = page.getByRole("combobox", { name: "Search models" });
  await search.fill("Solo 4.2");
  await search.press("Control+Enter");
  await expect.poll(async () => (await get("/state")).characters.length).toBe(2);
  await expect(page.locator("#chars .charrow")).toHaveCount(2);
});

test("a list item exposes position and set size", async ({ open, page }) => {
  await pickerOpen(open, page);
  const first = cells(page).first();
  await expect(first).toHaveAttribute("aria-posinset", "1");
  await expect(page.locator("#list")).toHaveAttribute("aria-label", /^Models, \d+ shown$/);
  await expect(page.locator('#list .cell[aria-current="true"]')).toHaveCount(1);
});

test("the models list retries until a late server starts", async ({ page }) => {
  const { startServer, THEME_URL, cmd } = await import("../lib/server.mjs");
  const server = await startServer({ wait: false });
  try {
    await page.goto(THEME_URL);
    await new Promise((r) => setTimeout(r, 300));
    await server.ready();
    await cmd(`set-model/${A}`);
    await fetch(`${(await import("../lib/server.mjs")).API}/edit/1`);
    await expect(cells(page).first()).toBeVisible({ timeout: 15_000 });
    expect(await setSize(page)).toBeGreaterThan(BULK_COUNT);
  } finally {
    await server.stop();
  }
});
