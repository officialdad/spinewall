import AxeBuilder from "@axe-core/playwright";
import { test, expect, get, post, reported } from "../lib/fixture.mjs";
import { SOLO, VARIANTS } from "../lib/models.mjs";

const A = SOLO["4.0"];
const BLOCKING = ["serious", "critical"];
async function scan(page) {
  const { violations } = await new AxeBuilder({ page })
    .include("#picker")
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  const blocking = violations.filter((v) => BLOCKING.includes(v.impact));
  return blocking.map((v) => `${v.id} (${v.impact}): ${v.nodes.map((n) => n.target.join(" ")).slice(0, 5).join(" | ")}`);
}

async function openPicker(open, page) {
  await open({ model: A, edit: true });
  await expect(page.locator("#list .cell").first()).toBeVisible();
  // Thumbnails shimmer until ready, and the animation would change sampled colors.
  await page.emulateMedia({ reducedMotion: "reduce" });
}

test("the picker has no serious or critical WCAG AA violations", async ({ open, page }) => {
  await openPicker(open, page);
  expect(await scan(page)).toEqual([]);
});

test("the list layout has no serious or critical WCAG AA violations", async ({ open, page }) => {
  await openPicker(open, page);
  await page.getByRole("button", { name: "List" }).click();
  await expect(page.locator("#list .cell.list").first()).toBeVisible();
  expect(await scan(page)).toEqual([]);
});

test("a search with expanded variants has no serious or critical violations", async ({ open, page }) => {
  await openPicker(open, page);
  const search = page.getByRole("combobox", { name: "Search models" });
  await search.fill("Alice");
  await search.press("Shift+Enter");
  // The list draws only rows near the viewport, so the count of rendered options depends on the list height. The listbox label counts every row.
  await expect(page.getByRole("listbox", { name: `Models, ${VARIANTS.length + 1} shown` })).toBeVisible();
  await expect(page.getByRole("option", { name: /variant of Alice/ }).first()).toBeVisible();
  expect(await scan(page)).toEqual([]);
});

test("the characters strip has no serious or critical violations", async ({ open, page }) => {
  await openPicker(open, page);
  await post("/layout", { replace: true, characters: [{ model: A, x: -0.2, y: 0, scale: 1 }, { model: SOLO["4.1"], x: 0.2, y: 0, scale: 1 }] });
  await expect(page.locator("#chars .charrow")).toHaveCount(2);
  expect(await scan(page)).toEqual([]);
});

test("the presets pane has no serious or critical violations", async ({ open, page }) => {
  await openPicker(open, page);
  await page.getByRole("button", { name: "Presets", exact: true }).click();
  await page.getByRole("textbox", { name: "Preset name" }).fill("desk");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible();
  expect(await scan(page)).toEqual([]);
});

test("a favorite cell has no serious or critical violations", async ({ open, page }) => {
  await openPicker(open, page);
  await post(`/fav/${A}`, {});
  await page.reload();
  await expect(page.getByRole("option", { name: /^Solo 4\.0.*, favorite$/ }).first()).toBeVisible();
  expect(await scan(page)).toEqual([]);
});

test("the on-page notice has enough contrast", async ({ open, page, context }) => {
  await context.route("**/spine-player40.js", (r) => r.abort());
  await open({ model: A });
  await expect(page.locator("#notice")).toBeVisible();
  const { violations } = await new AxeBuilder({ page }).include("#notice").withTags(["wcag2aa"]).analyze();
  expect(violations.filter((v) => BLOCKING.includes(v.impact)).map((v) => v.id)).toEqual([]);
});

test("the page raises no console errors in edit mode", async ({ open, page, consoleErrors }) => {
  await openPicker(open, page);
  await expect.poll(reported(A)).toBeTruthy();
  await page.mouse.click(640, 400);
  await expect.poll(async () => (await get("/info")).input.pointerdown).toBeGreaterThan(0);
  expect(consoleErrors).toEqual([]);
});

test("the Add character button name contains its visible text", async ({ open, page }) => {
  await openPicker(open, page);
  const { violations } = await new AxeBuilder({ page }).include("#picker").withRules(["label-content-name-mismatch"]).analyze();
  expect(violations).toEqual([]);
});

test("an option cell holds no nested interactive control", async ({ open, page }) => {
  await openPicker(open, page);
  const { violations } = await new AxeBuilder({ page }).include("#picker").withRules(["nested-interactive"]).analyze();
  expect(violations).toEqual([]);
  expect(await page.locator('#list [role="option"] button, #list [role="option"] [tabindex]').count()).toBe(0);
});
