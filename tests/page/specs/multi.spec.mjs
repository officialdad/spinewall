import { test, expect, get, post, cmd, reported } from "../lib/fixture.mjs";
import { SOLO, SCENES } from "../lib/models.mjs";

const A = SOLO["4.0"], B = SOLO["4.1"], C = SOLO["4.2"], LEGACY = SOLO["3.8"];
const W = 1280, H = 800;
const at = (x, y) => [W / 2 + x * W, H / 2 + y * H];

// `replace` sets exact positions, so every click lands where the test expects.
const setLayout = (characters) => post("/layout", { replace: true, characters });
const chars = async () => (await get("/state")).characters;
const stageReady = (page, canvases) => () => page.evaluate((n) => document.querySelectorAll("#layers canvas").length >= n, canvases);

test("add-character and remove-character change the stage", async ({ open, page }) => {
  await open({ model: A });
  await expect.poll(reported(A)).toBeTruthy();
  await cmd(`add-character/${B}`);
  await expect.poll(async () => (await chars()).length).toBe(2);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  const [first, second] = await chars();
  expect([first.model, second.model]).toEqual([A, B]);
  await cmd(`remove-character/${second.uid}`);
  await expect.poll(async () => (await chars()).length).toBe(0);
  const state = await get("/state");
  expect(state.model).toBe(A);
});

test("a click changes only the topmost character under the pointer", async ({ open, page }) => {
  await open({ model: A });
  await setLayout([
    { model: A, x: 0, y: 0, scale: 1 },
    { model: A, x: 0, y: 0, scale: 1 },
  ]);
  const [bottom, top] = await chars();
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(1);
  await expect.poll(async () => (await chars()).every((c) => c.animation === null || c.animation)).toBe(true);
  const [px, py] = at(0, 0);
  await page.mouse.click(px, py);
  await expect.poll(async () => (await chars()).find((c) => c.uid === top.uid).animation).not.toBeNull();
  const after = await chars();
  expect(after.find((c) => c.uid === bottom.uid).animation).toBe(bottom.animation);
});

test("a click on empty space does nothing", async ({ open, page }) => {
  await open({ model: A });
  await setLayout([
    { model: A, x: -0.3, y: 0, scale: 1 },
    { model: B, x: 0.3, y: 0, scale: 1 },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  const before = await chars();
  const [px, py] = at(0, 0.45);
  await page.mouse.click(px, py);
  await page.waitForTimeout(800);
  expect(await chars()).toEqual(before);
});

test("a scene draws first and passes the click to the character", async ({ open, page }) => {
  const scene = SCENES["4.0"];
  await open({ model: A });
  await setLayout([
    { model: A, x: 0, y: 0, scale: 1 },
    { model: scene, x: 0, y: 0, scale: 1 },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(1);
  const [character] = await chars();
  const [px, py] = at(0, 0);
  await page.mouse.click(px, py);
  await expect.poll(async () => (await chars())[0].animation).not.toBe(character.animation);
  const sceneEntry = (await chars())[1];
  expect(sceneEntry.animation).toBe(null);
});

// `layers` is a top-level const of the page script, so evaluate can read it by name.
const zByRuntime = (page) => page.evaluate(() => Object.fromEntries([...layers.values()].map((l) => [l.key, Number(l.canvas.style.zIndex)])));

test("canvases stack by the topmost character of each runtime", async ({ open, page }) => {
  await open({ model: A });
  await setLayout([
    { model: A, x: -0.2, y: 0, scale: 1 },
    { model: C, x: 0.2, y: 0, scale: 1 },
    { model: LEGACY, x: 0, y: 0.2, scale: 1 },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(3);
  await expect.poll(async () => Object.values(await zByRuntime(page)).sort()).toEqual([0, 1, 2]);
  const top = async () => { const z = await zByRuntime(page); return Object.keys(z).find((k) => z[k] === 2); };
  const [first, ...rest] = await chars();
  await expect.poll(top).not.toBe(Object.keys(await zByRuntime(page)).find((k) => k.includes("40")));
  await setLayout([...rest, first]);
  await expect.poll(async () => (await zByRuntime(page))[await top()]).toBe(2);
  await expect.poll(async () => (await top()) ?? "").toMatch(/40/);
});

test("a scene-only layer ranks lowest", async ({ open, page }) => {
  await open({ model: A });
  await setLayout([
    { model: A, x: 0, y: 0, scale: 1 },
    { model: SCENES["4.2"], x: 0, y: 0, scale: 1 },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  // The scene is last in the list, yet its 4.2 layer holds no character, so it still sits below the 4.0 layer.
  await expect.poll(async () => {
    const z = await zByRuntime(page);
    const keys = Object.keys(z);
    const scene = keys.find((k) => k.includes("42"));
    const character = keys.find((k) => k.includes("40"));
    return scene && character ? z[scene] < z[character] : null;
  }).toBe(true);
});

test("edit-mode drag moves the selected character in multi mode", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await setLayout([
    { model: A, x: -0.3, y: 0, scale: 1 },
    { model: B, x: 0.3, y: 0, scale: 1 },
  ]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  const [left] = await chars();
  const [px, py] = at(-0.3, 0);
  await page.mouse.move(px, py);
  await page.mouse.down();
  await page.mouse.move(px + 100, py + 40, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await chars())[0].x).toBeGreaterThan(left.x);
  expect((await chars())[1].x).toBeCloseTo(0.3, 5);
});
