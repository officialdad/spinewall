import { test, expect, get, post } from "../lib/fixture.mjs";
import { SOLO } from "../lib/models.mjs";

const A = SOLO["4.0"], B = SOLO["4.1"], C = SOLO["4.2"];

const setLayout = (characters) => post("/layout", { replace: true, characters });
const chars = async () => (await get("/state")).characters;
const fit = (page) => page.evaluate(() => ({ ...fit }));
const chip = (page, n) => page.locator("#chips .chip").nth(n);
const browse = (page) => page.getByRole("button", { name: "Browse models and presets" });
const target = async (page, x, y) => {
  const f = await fit(page);
  const { width, height } = page.viewportSize();
  return [f.ox + (width / 2 + x * width) * f.s, f.oy + (height / 2 + y * height) * f.s];
};

// The sheet opens only on request, so a multi-character edit session starts with the bar and handle alone.
async function openMulti(open, page, characters = [
  { model: A, x: -0.3, y: 0, scale: 1 },
  { model: B, x: 0.3, y: 0, scale: 1 },
]) {
  await open({ model: A });
  await setLayout(characters);
  await expect.poll(() => page.evaluate(() => stageChars.size)).toBe(characters.length);
  await get("/edit/1");
  await expect(page.locator("#picker")).toHaveAttribute("data-folded", "");
  await expect(page.locator("#bar")).toBeVisible();
  await expect(page.locator("#handle")).toBeVisible();
}

const outsideCharacter = (handle, box) =>
  handle.y + handle.height <= box.y + 1 || handle.y >= box.y + box.height - 1;

test("the sheet starts folded and the handle sits beside the selected character", async ({ open, page }) => {
  await openMulti(open, page);
  await expect(page.locator("#bar")).toBeVisible();
  await expect(page.locator("#handle")).toBeVisible();
  await expect(browse(page)).toHaveAttribute("aria-expanded", "false");
  await expect(page.locator("#chips .chip[aria-pressed=\"true\"]")).toHaveCount(1);
  expect(await page.locator("#handleName").textContent()).toContain("Solo 4.1");
  const handle = await page.locator("#handle").boundingBox();
  const box = await page.locator("#selbox").boundingBox();
  const bar = await page.locator("#bar").boundingBox();
  expect(outsideCharacter(handle, box)).toBe(true);
  expect(handle.y + handle.height).toBeLessThanOrEqual(bar.y + 1);
});

test("a chip selects a character and the handle follows a drag and a scale", async ({ open, page }) => {
  await openMulti(open, page);
  await chip(page, 0).click();
  await expect(chip(page, 0)).toHaveAttribute("aria-pressed", "true");
  expect(await page.locator("#handleName").textContent()).toContain("Solo 4.0");

  const before = (await chars())[0];
  const [px, py] = await target(page, -0.3, 0);
  await page.mouse.move(px, py);
  await page.mouse.down();
  await page.mouse.move(px + 70, py + 30, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await chars())[0].x).toBeGreaterThan(before.x);
  expect((await chars())[1].x).toBeCloseTo(0.3, 5);

  const moved = await page.locator("#handle").boundingBox();
  const box = await page.locator("#selbox").boundingBox();
  expect(outsideCharacter(moved, box)).toBe(true);

  const scale = (await chars())[0].scale;
  const [nx, ny] = await target(page, (await chars())[0].x, 0);
  await page.mouse.move(nx, ny);
  await page.mouse.wheel(0, 300);
  await expect.poll(async () => (await chars())[0].scale).toBeLessThan(scale);
  await expect(page.locator("#handle")).toBeVisible();
  const scaled = await page.locator("#handle").boundingBox();
  const after = await page.locator("#selbox").boundingBox();
  const bar = await page.locator("#bar").boundingBox();
  expect(outsideCharacter(scaled, after)).toBe(true);
  expect(scaled.y + scaled.height).toBeLessThanOrEqual(bar.y + 1);
});

test("the handle steps the selected animation and leaves the other character alone", async ({ open, page }) => {
  await openMulti(open, page);
  await expect.poll(() => page.evaluate(() => [...stageChars.values()].every((c) => c.names.length > 0))).toBe(true);
  // The page posts its animation list after a load. Give the server that report before stepping.
  await page.waitForTimeout(400);
  await chip(page, 1).click();
  const before = await chars();
  await page.getByRole("button", { name: "Next animation" }).click();
  await expect.poll(async () => (await chars())[1].animation).not.toBe(before[1].animation);
  expect((await chars())[0].animation).toBe(before[0].animation);
  await expect(page.locator("#handleName")).toContainText((await chars())[1].animation);

  const next = (await chars())[1].animation;
  await page.getByRole("button", { name: "Previous animation" }).click();
  await expect.poll(async () => (await chars())[1].animation).not.toBe(next);
  expect((await chars())[0].animation).toBe(before[0].animation);
});

test("the handle removes only the selected character", async ({ open, page }) => {
  await openMulti(open, page, [
    { model: A, x: -0.3, y: 0, scale: 1 },
    { model: B, x: 0, y: 0, scale: 1 },
    { model: C, x: 0.3, y: 0, scale: 1 },
  ]);
  await chip(page, 2).click();
  await page.getByRole("button", { name: "Remove character" }).click();
  await expect.poll(async () => (await chars()).length).toBe(2);
  await expect(page.locator("#chips .chip")).toHaveCount(2);
  expect((await chars()).map((c) => c.model)).toEqual([A, B]);
  await expect(page.locator("#picker")).toHaveAttribute("data-folded", "");
  await expect(page.locator("#handle")).toBeVisible();
});

test("change model opens the sheet on models and applies to the selected character", async ({ open, page }) => {
  await openMulti(open, page);
  await chip(page, 1).click();
  await page.getByRole("button", { name: "Change model" }).click();
  await expect(page.locator("#picker")).not.toHaveAttribute("data-folded", "");
  const search = page.getByRole("combobox", { name: "Search models" });
  await expect(search).toBeFocused();
  // The list draws only the rows near its viewport, so narrow it before picking a far one.
  await search.fill("Solo 4.2");
  await page.getByRole("option", { name: "Solo 4.2" }).click();
  await expect.poll(async () => (await chars())[1].model).toBe(C);
  expect((await chars())[0].model).toBe(A);
});

test("Add adds a character without opening the sheet", async ({ open, page }) => {
  await openMulti(open, page);
  await chip(page, 1).click();
  await page.getByRole("button", { name: "Add a character" }).click();
  await expect.poll(async () => (await chars()).length).toBe(3);
  expect((await chars())[2].model).toBe(B);
  await expect(page.locator("#picker")).toHaveAttribute("data-folded", "");
  await expect(page.locator("#chips .chip")).toHaveCount(3);
});

test("Browse opens the sheet and Escape closes it, returning focus to Browse", async ({ open, page }) => {
  await openMulti(open, page);
  await browse(page).click();
  await expect(page.locator("#picker")).not.toHaveAttribute("data-folded", "");
  await expect(browse(page)).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("combobox", { name: "Search models" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.locator("#picker")).toHaveAttribute("data-folded", "");
  await expect(browse(page)).toHaveAttribute("aria-expanded", "false");
  await expect(browse(page)).toBeFocused();
});

test("every common action works from the keyboard", async ({ open, page }) => {
  await openMulti(open, page);
  await expect.poll(() => page.evaluate(() => [...stageChars.values()].every((c) => c.names.length > 0))).toBe(true);

  const next = page.getByRole("button", { name: "Next animation" });
  await next.focus();
  await expect(next).toBeFocused();
  const before = (await chars())[1].animation;
  await page.keyboard.press("Enter");
  await expect.poll(async () => (await chars())[1].animation).not.toBe(before);

  await chip(page, 0).focus();
  await page.keyboard.press("Enter");
  await expect(chip(page, 0)).toHaveAttribute("aria-pressed", "true");

  await browse(page).focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#picker")).not.toHaveAttribute("data-folded", "");
  await page.keyboard.press("Escape");
  await expect(page.locator("#picker")).toHaveAttribute("data-folded", "");
});

test("chips tell two variants of one model apart", async ({ open, page }) => {
  await openMulti(open, page, [
    { model: A, x: -0.2, y: 0, scale: 1 },
    { model: A, x: 0.2, y: 0, scale: 1 },
  ]);
  const labels = await page.locator("#chips .chip").evaluateAll((els) => els.map((e) => e.getAttribute("aria-label")));
  expect(new Set(labels).size).toBe(2);
  expect(labels[0]).toContain("1 of 2");
  expect(labels[1]).toContain("2 of 2");
});

test("a selected chip keeps a black focus ring on its white fill", async ({ open, page }) => {
  await openMulti(open, page);
  const selected = page.locator('#chips .chip[aria-pressed="true"]');
  await selected.focus();
  const ring = await selected.evaluate((e) => getComputedStyle(e).outlineColor);
  expect(ring).toBe("rgb(0, 0, 0)");
  const width = await selected.evaluate((e) => parseFloat(getComputedStyle(e).outlineWidth));
  expect(width).toBeGreaterThanOrEqual(3);
});

const VIEWPORTS = [
  ["phone portrait", { width: 390, height: 844 }],
  ["phone landscape", { width: 844, height: 390 }],
  ["desktop", { width: 1280, height: 800 }],
];

for (const [label, viewport] of VIEWPORTS) {
  test(`the bar and handle fit ${label} with no sideways scroll`, async ({ open, page }) => {
    await page.setViewportSize(viewport);
    await openMulti(open, page);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);

    const bar = await page.locator("#bar").boundingBox();
    expect(bar.x).toBeGreaterThanOrEqual(-0.5);
    expect(bar.x + bar.width).toBeLessThanOrEqual(viewport.width + 0.5);
    expect(bar.y).toBeGreaterThanOrEqual(0);
    expect(bar.y + bar.height).toBeLessThanOrEqual(viewport.height + 0.5);

    const stage = await page.locator("#layers").boundingBox();
    expect(stage.y + stage.height).toBeLessThanOrEqual(bar.y + 1);

    const handle = await page.locator("#handle").boundingBox();
    expect(handle.x).toBeGreaterThanOrEqual(-0.5);
    expect(handle.x + handle.width).toBeLessThanOrEqual(viewport.width + 0.5);
    expect(handle.y + handle.height).toBeLessThanOrEqual(bar.y + 1);

    for (const name of ["Previous animation", "Next animation", "Change model", "Remove character", "Add a character", "Browse models and presets"]) {
      const box = await page.getByRole("button", { name }).boundingBox();
      expect(box.width).toBeGreaterThanOrEqual(24);
      expect(box.height).toBeGreaterThanOrEqual(24);
      expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(24);
    }
    const chipBox = await page.locator("#chips .chip").first().boundingBox();
    expect(chipBox.height).toBeGreaterThanOrEqual(44);
    expect(chipBox.width).toBeGreaterThanOrEqual(44);
  });
}
