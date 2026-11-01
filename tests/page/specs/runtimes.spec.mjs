import { test, expect, cmd, get, reported } from "../lib/fixture.mjs";
import { SOLO, ANIMATION_NAMES, BLANK } from "../lib/models.mjs";

for (const [version, id] of Object.entries(SOLO)) {
  test(`a ${version} model loads and reports animations`, async ({ open, consoleErrors }) => {
    await open({ model: id });
    await expect.poll(reported(id)).toEqual(expect.arrayContaining(ANIMATION_NAMES));
    expect(consoleErrors).toEqual([]);
  });
}

test("switching models through /cmd/set-model changes the loaded model", async ({ open }) => {
  await open({ model: SOLO["4.0"] });
  await expect.poll(reported(SOLO["4.0"])).toBeTruthy();
  await cmd(`set-model/${SOLO["3.8"]}`);
  await expect.poll(async () => (await get("/info")).state.model).toBe(SOLO["3.8"]);
  await expect.poll(async () => (await get("/info")).animations.length).toBeGreaterThan(0);
});

test("a state change reaches the page within 2 s", async ({ open, page }) => {
  await open({ model: SOLO["4.0"] });
  await expect.poll(reported(SOLO["4.0"])).toBeTruthy();
  await cmd("set-anim/spin");
  await expect.poll(() => page.evaluate(() => document.querySelectorAll("canvas").length), { timeout: 2000 }).toBeGreaterThan(0);
  await expect.poll(async () => (await get("/state")).animation, { timeout: 2000 }).toBe("spin");
});

test("a missing runtime shows an on-page notice", async ({ open, page, context }) => {
  await context.route("**/spine-player40.js", (r) => r.abort());
  await open({ model: SOLO["4.0"] });
  await expect(page.locator("#notice")).toContainText("runtime for");
  await expect(page.locator("#notice")).toContainText("scripts/install.sh");
});

test("webglcontextlost reloads the page", async ({ open, page }) => {
  await open({ model: SOLO["4.0"] });
  await expect.poll(reported(SOLO["4.0"])).toBeTruthy();
  await page.evaluate(() => { window.__marker = 1; });
  await page.evaluate(() => document.querySelector("canvas").dispatchEvent(new Event("webglcontextlost", { cancelable: true })));
  await expect.poll(() => page.evaluate(() => window.__marker), { timeout: 8000 }).toBeUndefined();
});

test("an animation that draws nothing stays out of the animation list", async ({ open }) => {
  await open({ model: BLANK.id });
  await expect.poll(reported(BLANK.id)).toEqual(expect.arrayContaining(["idle", "wave", "spin"]));
  expect(await get("/info").then((i) => i.animations)).not.toContain(BLANK.animation);
});
