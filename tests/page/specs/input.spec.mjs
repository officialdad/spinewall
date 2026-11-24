import { test, expect, get, cmd, reported, THEME_URL } from "../lib/fixture.mjs";
import { SOLO } from "../lib/models.mjs";

const id = SOLO["4.0"];
const input = async () => (await get("/info")).input;

// The page sends counts every 2 s, so waits poll for the server to see them.
async function loaded(open) {
  const page = await open({ model: id });
  await expect.poll(reported(id)).toBeTruthy();
  return page;
}

test("a normal-mode click changes the animation and raises input.fired", async ({ open, page }) => {
  await loaded(open);
  const before = (await get("/state")).animation;
  const fired = (await input()).fired || 0;
  await page.mouse.click(640, 400);
  await expect.poll(async () => (await get("/state")).animation).not.toBe(before);
  await expect.poll(async () => (await input()).fired, { timeout: 6000 }).toBeGreaterThan(fired);
});

test("a normal-mode drag moves nothing", async ({ open, page }) => {
  await loaded(open);
  const before = await get("/state");
  await page.mouse.move(640, 400);
  await page.mouse.down();
  await page.mouse.move(800, 500, { steps: 5 });
  await page.mouse.up();
  await page.mouse.wheel(0, -400);
  await page.waitForTimeout(1000);
  const after = await get("/state");
  expect([after.x, after.y, after.scale]).toEqual([before.x, before.y, before.scale]);
});

test("edit-mode drag changes x and y", async ({ open, page }) => {
  await open({ model: id, edit: true });
  await expect.poll(reported(id)).toBeTruthy();
  const before = await get("/state");
  await page.mouse.move(900, 100);
  await page.mouse.down();
  await page.mouse.move(1000, 160, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await get("/state")).x).toBeGreaterThan(before.x);
  await expect.poll(async () => (await get("/state")).y).toBeGreaterThan(before.y);
});

test("edit-mode wheel changes scale and clamps it to 0.3 - 8", async ({ open, page }) => {
  await open({ model: id, edit: true });
  await expect.poll(reported(id)).toBeTruthy();
  await page.mouse.move(900, 100);
  const start = (await get("/state")).scale;
  await page.mouse.wheel(0, -300);
  await expect.poll(async () => (await get("/state")).scale).toBeGreaterThan(start);
  for (let i = 0; i < 12; i++) await page.mouse.wheel(0, -1000);
  await expect.poll(async () => (await get("/state")).scale).toBe(8);
  for (let i = 0; i < 30; i++) await page.mouse.wheel(0, 1000);
  await expect.poll(async () => (await get("/state")).scale).toBe(0.3);
});

test("a short edit-mode click still changes the animation", async ({ open, page }) => {
  await open({ model: id, edit: true });
  await expect.poll(reported(id)).toBeTruthy();
  const before = (await get("/state")).animation;
  await page.mouse.click(640, 400);
  await expect.poll(async () => (await get("/state")).animation).not.toBe(before);
});

test("a drag of 30 px or more is not a click", async ({ open, page }) => {
  await open({ model: id, edit: true });
  await expect.poll(reported(id)).toBeTruthy();
  const before = (await get("/state")).animation;
  await page.mouse.move(640, 400);
  await page.mouse.down();
  await page.mouse.move(700, 400, { steps: 4 });
  await page.mouse.up();
  await page.waitForTimeout(800);
  expect((await get("/state")).animation).toBe(before);
});

// A host can forward mouse and wheel events without ever synthesizing Pointer
// Events (the old webwallpaper host did this on the same Plasma the captain runs).
// CSS :hover still highlights and the wheel still arrives, so the page looks
// alive, yet a pointerdown-only listener counts every click as lost. Dispatch raw
// MouseEvents, which carry no accompanying pointerdown, and expect the click to
// still change the animation and raise input.fired.
const dispatchMouse = (page, type, x, y) => page.evaluate(([type, x, y]) => {
  document.dispatchEvent(new MouseEvent(type, {
    bubbles: true, cancelable: true, clientX: x, clientY: y, button: 0, buttons: type === "mouseup" ? 0 : 1,
  }));
}, [type, x, y]);

test("a click still fires when only mouse events arrive, with no pointer events", async ({ open, page }) => {
  await open({ model: id });
  await expect.poll(reported(id)).toBeTruthy();
  const before = (await get("/state")).animation;
  await dispatchMouse(page, "mousemove", 640, 400);
  await dispatchMouse(page, "mousedown", 640, 400);
  await dispatchMouse(page, "mouseup", 640, 400);
  await expect.poll(async () => (await get("/state")).animation).not.toBe(before);
  await expect.poll(async () => (await input()).fired).toBeGreaterThan(0);
});

// A page control stops the pointer events of its press, so the first press on one
// leaves the page with no pointer gesture seen yet. The mouse copy of that press
// must stop at the control too, or it counts as a stage click behind the control.
test("a click on the Edit button or a picker cell is not a stage click", async ({ page, server }) => {
  await cmd(`set-model/${id}`);
  await get("/edit/0");
  // The page runs on its own screen stage, so read that stage, not the default one.
  const screen = "screen=1920x1080%2B0%2B0";
  const animation = async () => (await get(`/state?${screen}`)).animation;
  await page.goto(`${THEME_URL}?view=native&${screen}`);
  await expect.poll(reported(id)).toBeTruthy();
  const before = await animation();
  const fired = (await input()).fired || 0;

  await page.locator("#editToggle").click();
  await expect.poll(async () => (await get("/state")).edit).toBe(1);
  const star = page.locator('#list .cell[aria-current="true"] .star');
  await expect(star).toBeVisible();
  await star.click();

  await page.waitForTimeout(2500);
  expect(await animation()).toBe(before);
  expect((await input()).fired || 0).toBe(fired);
});
