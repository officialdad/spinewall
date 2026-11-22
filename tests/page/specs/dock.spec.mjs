import { test, expect, get, post } from "../lib/fixture.mjs";
import { SOLO } from "../lib/models.mjs";

const A = SOLO["4.0"], B = SOLO["4.1"];
const W = 1280, H = 800;
// A character this far right sat under the old right-hand panel, so it could not be grabbed.
const UNDER_OLD_PANEL = 0.38;

const setLayout = (characters) => post("/layout", { replace: true, characters });
const chars = async () => (await get("/state")).characters;
const fit = (page) => page.evaluate(() => ({ ...fit }));
const where = async (page, x, y) => {
  const f = await fit(page);
  return [f.ox + (W / 2 + x * W) * f.s, f.oy + (H / 2 + y * H) * f.s];
};
const foldButton = (page) => page.locator("#dockFold");

async function openDock(open, page) {
  await open({ model: A, edit: true });
  await setLayout([
    { model: A, x: UNDER_OLD_PANEL, y: 0, scale: 1 },
    { model: B, x: -0.3, y: 0, scale: 1 },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  await expect(page.locator("#picker")).toBeVisible();
  await expect.poll(async () => (await fit(page)).s).toBeLessThan(1);
}

test("a character under where the panel used to sit can be grabbed and dragged", async ({ open, page }) => {
  await openDock(open, page);
  const [px, py] = await where(page, UNDER_OLD_PANEL, 0);
  const panel = await page.locator("#picker").boundingBox();
  expect(px).toBeLessThan(panel.x);
  expect(await page.evaluate(([x, y]) => !!document.elementFromPoint(x, y).closest("#picker"), [px, py])).toBe(false);
  const before = (await chars())[0];
  await page.mouse.move(px, py);
  await page.mouse.down();
  await page.mouse.move(px - 60, py + 30, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await chars())[0].x).toBeLessThan(before.x);
  expect((await chars())[1].x).toBeCloseTo(-0.3, 5);
});

test("a dragged character follows the pointer one to one while the dock is open", async ({ open, page }) => {
  await openDock(open, page);
  const [px, py] = await where(page, UNDER_OLD_PANEL, 0);
  await page.mouse.move(px, py);
  await page.mouse.down();
  await page.mouse.move(px - 80, py, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await chars())[0].x).toBeLessThan(UNDER_OLD_PANEL);
  const [after] = await where(page, (await chars())[0].x, 0);
  expect(Math.abs(after - (px - 80))).toBeLessThan(3);
});

test("the wheel scales a character anywhere on the stage", async ({ open, page }) => {
  await openDock(open, page);
  const [px, py] = await where(page, UNDER_OLD_PANEL, 0);
  await page.mouse.click(px, py);
  await page.mouse.wheel(0, -300);
  await expect.poll(async () => (await chars())[0].scale).toBeGreaterThan(1);
  expect((await chars())[1].scale).toBe(1);
});

test("the selection box follows the shrunken stage", async ({ open, page }) => {
  await openDock(open, page);
  const [px, py] = await where(page, UNDER_OLD_PANEL, 0);
  await page.mouse.click(px, py);
  const box = await page.locator("#selbox").boundingBox();
  expect(px).toBeGreaterThan(box.x);
  expect(px).toBeLessThan(box.x + box.width);
  expect(py).toBeGreaterThan(box.y);
  expect(py).toBeLessThan(box.y + box.height);
});

test("the fold button hides the dock and gives the stage its whole window back", async ({ open, page }) => {
  await openDock(open, page);
  const button = foldButton(page);
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(button).toHaveAccessibleName("Fold editor");
  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(button).toHaveAccessibleName("Show editor");
  await expect(page.locator("#picker")).toHaveAttribute("data-folded", "");
  expect(await fit(page)).toEqual({ s: 1, ox: 0, oy: 0 });
  const box = await button.boundingBox();
  expect(box.width).toBeGreaterThanOrEqual(44);
  expect(box.height).toBeGreaterThanOrEqual(44);
  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect.poll(async () => (await fit(page)).s).toBeLessThan(1);
});

test("P folds and shows the dock, and typing a p in a field does not", async ({ open, page }) => {
  await openDock(open, page);
  await page.getByRole("combobox", { name: "Search models" }).focus();
  await page.keyboard.type("pp");
  await expect(foldButton(page)).toHaveAttribute("aria-expanded", "true");
  await page.getByRole("combobox", { name: "Search models" }).fill("");
  await page.getByRole("combobox", { name: "Search models" }).blur();
  await page.keyboard.press("p");
  await expect(foldButton(page)).toHaveAttribute("aria-expanded", "false");
  await page.keyboard.press("P");
  await expect(foldButton(page)).toHaveAttribute("aria-expanded", "true");
});

test("folding from inside the dock moves focus to the fold button", async ({ open, page }) => {
  await openDock(open, page);
  await page.getByRole("button", { name: "Grid" }).focus();
  await page.keyboard.press("p");
  await expect(foldButton(page)).toBeFocused();
});

test("the fold state survives a reload without touching rev or vrev", async ({ open, page }) => {
  await openDock(open, page);
  const before = await get("/state");
  await foldButton(page).click();
  await page.waitForTimeout(900);
  const after = await get("/state");
  expect([after.rev, after.vrev, after.crev]).toEqual([before.rev, before.vrev, before.crev]);
  await page.reload();
  await expect(foldButton(page)).toHaveAttribute("aria-expanded", "false");
  expect(await fit(page)).toEqual({ s: 1, ox: 0, oy: 0 });
});

test("the dock can move to the left edge and the choice persists", async ({ open, page }) => {
  await openDock(open, page);
  await page.getByRole("button", { name: "Dock on the left" }).click();
  await expect(page.locator("#picker")).toHaveAttribute("data-dock", "left");
  const [px, py] = await where(page, UNDER_OLD_PANEL, 0);
  const panel = await page.locator("#picker").boundingBox();
  expect(px).toBeGreaterThan(panel.x + panel.width);
  await page.reload();
  await expect(page.locator("#picker")).toHaveAttribute("data-dock", "left");
  expect(py).toBeGreaterThan(0);
});

test("a portrait phone docks at the bottom and the stage keeps the free area above it", async ({ open, page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await open({ model: A, edit: true });
  await expect(page.locator("#picker")).toHaveAttribute("data-dock", "bottom");
  const panel = await page.locator("#picker").boundingBox();
  expect(panel.y).toBeGreaterThan(844 * 0.3);
  expect(panel.y + panel.height).toBeLessThanOrEqual(844);
  await expect.poll(async () => (await fit(page)).s).toBeLessThan(1);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  await expect(page.locator(".dockhead button")).toBeHidden();
});

test("a landscape phone keeps a free stage beside the dock", async ({ open, page }) => {
  await page.setViewportSize({ width: 844, height: 390 });
  await open({ model: A, edit: true });
  await expect(page.locator("#picker")).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  const f = await fit(page);
  expect(f.s).toBeGreaterThan(0.4);
});

test("character rows tell two variants of one model apart", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await setLayout([
    { model: A, x: -0.2, y: 0, scale: 1 },
    { model: A, x: 0.2, y: 0, scale: 1 },
  ]);
  const rows = page.locator("#chars .charrow .sel");
  await expect(rows).toHaveCount(2);
  const names = await rows.evaluateAll((els) => els.map((e) => e.getAttribute("aria-label")));
  expect(new Set(names).size).toBe(2);
  expect(names[0]).toContain("1 of 2");
  expect(names[1]).toContain("2 of 2");
});

test("a selected character row shows a black focus ring on its white fill", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await setLayout([{ model: A, x: -0.2, y: 0, scale: 1 }, { model: B, x: 0.2, y: 0, scale: 1 }]);
  const selected = page.locator('#chars .sel[aria-pressed="true"]');
  await expect(selected).toHaveCount(1);
  await selected.focus();
  const ring = await selected.evaluate((e) => getComputedStyle(e).outlineColor);
  expect(ring).toBe("rgb(0, 0, 0)");
  const width = await selected.evaluate((e) => parseFloat(getComputedStyle(e).outlineWidth));
  expect(width).toBeGreaterThanOrEqual(3);
});
