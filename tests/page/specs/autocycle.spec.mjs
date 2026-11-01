import { test, expect, get, post, cmd, reported } from "../lib/fixture.mjs";
import { SOLO } from "../lib/models.mjs";

const A = SOLO["4.0"], B = SOLO["4.1"];
// The theme plays an animation at least 6 s and has no hook to shorten it, so these waits are real.
const CYCLE_MS = 20_000;

const setLayout = (characters) => post("/layout", { replace: true, characters });
const chars = async () => (await get("/state")).characters;
const byModel = async (model) => (await chars()).find((c) => c.model === model);

test("single mode: the toggle makes the animation change, and off stops it", async ({ open, page }) => {
  test.setTimeout(90_000);
  await open({ model: A });
  await expect.poll(reported(A)).toBeTruthy();
  const start = (await get("/state")).animation;
  await cmd("auto-cycle/on");
  await expect.poll(async () => (await get("/state")).auto).toBe(true);
  await expect.poll(async () => (await get("/state")).animation, { timeout: CYCLE_MS }).not.toBe(start);
  await cmd("auto-cycle/off");
  await expect.poll(async () => (await get("/state")).auto).toBe(false);
  await page.waitForTimeout(1500);
  const settled = (await get("/state")).animation;
  await page.waitForTimeout(8000);
  expect((await get("/state")).animation).toBe(settled);
});

test("multi mode: only the character with the flag cycles", async ({ open, page }) => {
  test.setTimeout(90_000);
  await open({ model: A });
  await expect.poll(reported(A)).toBeTruthy();
  await setLayout([
    { model: A, x: -0.25, y: 0, scale: 1, animation: "idle", auto: true },
    { model: B, x: 0.25, y: 0, scale: 1, animation: "idle", auto: false },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  await expect.poll(async () => (await byModel(A)).animation, { timeout: CYCLE_MS }).not.toBe("idle");
  expect((await byModel(B)).animation).toBe("idle");
  expect((await byModel(B)).auto).toBe(false);
});

test("the flag survives a page reload", async ({ open, page }) => {
  await open({ model: A });
  await expect.poll(reported(A)).toBeTruthy();
  await setLayout([
    { model: A, x: -0.25, y: 0, scale: 1, auto: true },
    { model: B, x: 0.25, y: 0, scale: 1 },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  await page.reload();
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  expect((await byModel(A)).auto).toBe(true);
  expect((await byModel(B)).auto).toBe(false);
});

test("the checkbox in edit mode sets the flag for the selected character", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  const box = page.locator("#auto-target");
  await expect(box).toBeVisible();
  await box.check();
  await expect.poll(async () => (await get("/state")).auto).toBe(true);
  await box.uncheck();
  await expect.poll(async () => (await get("/state")).auto).toBe(false);
});

test("the Auto checkbox is hidden outside edit mode", async ({ open, page }) => {
  await open({ model: A });
  await expect.poll(reported(A)).toBeTruthy();
  await expect(page.locator("#auto-target")).toBeHidden();
});
