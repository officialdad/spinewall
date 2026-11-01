import { test, expect, get, cmd, reported } from "../lib/fixture.mjs";
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
