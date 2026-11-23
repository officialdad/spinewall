// The native layer-shell view needs two things from the page that the Plasma
// patch and the webwallpaper plugin used to provide: an Edit button, and a click
// mask, so a click on the character reaches the page and a click elsewhere reaches
// the desktop. The server keeps the mask per screen; the launcher polls it.
//
// These tests cover the page's side: it reports the character's region, reports
// the full window in edit mode, and offers the Edit button.
import { test, expect, get, THEME_URL, reported } from "../lib/fixture.mjs";
import { SOLO } from "../lib/models.mjs";

const id = SOLO["4.0"];

// Open the page the way the native view does, with ?view=native so both features
// turn on. `open` from the fixture loads the bare URL, so this navigates directly.
async function openNative(page, opts = {}) {
  if (opts.model) {
    await get(`/cmd/set-model/${encodeURIComponent(opts.model)}`);
  }
  if (opts.edit) await get("/edit/1");
  else await get("/edit/0");
  // A screen key so the mask is stored per stage, as the launcher asks for it.
  await page.goto(`${THEME_URL}?view=native&screen=1920x1080%2B0%2B0`);
  return page;
}

const maskFor = async () => (await get("/mask?screen=1920x1080%2B0%2B0"));

test("the native view reports a click mask around the character", async ({ page, server }) => {
  await openNative(page, { model: id });
  await expect.poll(reported(id)).toBeTruthy();

  // The page reports the character's box, not the whole window, so a click on the
  // wallpaper reaches Plasma. At least one positive rectangle is the contract.
  await expect.poll(async () => {
    const mask = await maskFor();
    return mask.full ? 0 : mask.rects.length;
  }, { timeout: 6000 }).toBeGreaterThan(0);

  const mask = await maskFor();
  expect(mask.full).toBe(false);
  for (const r of mask.rects) {
    expect(r.w).toBeGreaterThan(0);
    expect(r.h).toBeGreaterThan(0);
  }
  // The box is a sensible fraction of the window, not the whole viewport.
  const total = mask.rects.reduce((sum, r) => sum + r.w * r.h, 0);
  expect(total).toBeLessThan(1920 * 1080 * 0.9);
});

test("the normal-mode mask covers the Edit button so a click on it reaches the page", async ({ page, server }) => {
  await openNative(page, { model: id });
  await expect.poll(reported(id)).toBeTruthy();
  const box = await page.locator("#editToggle").boundingBox();
  expect(box).not.toBeNull();
  // The launcher passes clicks outside the mask to the desktop, so the button's
  // whole box must sit inside one reported rectangle.
  const covers = r => r.x <= box.x && r.y <= box.y &&
    r.x + r.w >= box.x + box.width && r.y + r.h >= box.y + box.height;
  await expect.poll(async () => {
    const mask = await maskFor();
    return !mask.full && mask.rects.some(covers);
  }, { timeout: 6000 }).toBe(true);
});

test("edit mode reports the full window so the dock and handle work", async ({ page, server }) => {
  await openNative(page, { model: id });
  await expect.poll(reported(id)).toBeTruthy();
  // Enter edit mode through the page's own Edit button, which is the native view's
  // only way in without the containment patch.
  await expect(page.locator("#editToggle")).toBeVisible();
  await page.locator("#editToggle").click();
  await expect.poll(async () => (await get("/state")).edit).toBe(1);
  await expect.poll(async () => (await maskFor()).full, { timeout: 6000 }).toBe(true);
});

test("a bare page with no ?view=native hides the Edit button and sends no mask", async ({ page, server }) => {
  await get(`/cmd/set-model/${encodeURIComponent(id)}`);
  await get("/edit/0");
  await page.goto(THEME_URL);
  await expect.poll(reported(id)).toBeTruthy();
  await expect(page.locator("#editToggle")).toBeHidden();
  // The mask stays at its full default because the page never reports.
  const mask = await get("/mask");
  expect(mask.full).toBe(true);
});
