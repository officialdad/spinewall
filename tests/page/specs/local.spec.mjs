import fs from "node:fs";
import path from "node:path";
import { test, expect } from "@playwright/test";
import { MODELS } from "../lib/global-setup.mjs";
import { THEME_URL } from "../lib/server.mjs";
import { SOLO, VARIANTS } from "../lib/models.mjs";

// Local mode has no server, so these tests start none. The index sits beside the model folders.
const A = SOLO["4.0"], B = SOLO["4.1"];

function writeIndex() {
  const models = fs.readdirSync(MODELS, { withFileTypes: true }).filter((d) => d.isDirectory()).map((d) => {
    const dir = path.join(MODELS, d.name);
    const meta = fs.existsSync(path.join(dir, "model.json")) ? JSON.parse(fs.readFileSync(path.join(dir, "model.json"), "utf8")) : {};
    const skeleton = JSON.parse(fs.readFileSync(path.join(dir, `${d.name}.json`), "utf8"));
    return { id: d.name, skeleton: `${d.name}.json`, atlas: `${d.name}.atlas`, spine: skeleton.skeleton.spine, ...meta };
  });
  fs.writeFileSync(path.join(MODELS, "index.json"), JSON.stringify({ models }));
}

const LOCAL_URL = () => `${THEME_URL}?local=${encodeURIComponent("file://" + path.join(MODELS, "index.json"))}`;
const stateOf = (page) => page.evaluate(async () => (await api("/state")).json());
const command = (page, c) => page.evaluate((p) => cmd(p), c);

test.beforeEach(() => writeIndex());

test("local mode plays a model without a server", async ({ page }) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(LOCAL_URL());
  await expect.poll(async () => (await stateOf(page)).animation).toBeTruthy();
  await expect(page.locator("#p canvas, #layers canvas").first()).toBeVisible();
  expect(errors).toEqual([]);
});

test("a click advances the animation and the state survives a reload", async ({ page }) => {
  await page.goto(LOCAL_URL());
  await expect.poll(async () => (await stateOf(page)).animation).toBeTruthy();
  const before = (await stateOf(page)).animation;
  await page.mouse.click(640, 400);
  await expect.poll(async () => (await stateOf(page)).animation).not.toBe(before);
  const after = (await stateOf(page)).animation;
  await page.reload();
  await expect.poll(async () => (await stateOf(page)).animation).toBe(after);
});

test("the edit button opens the picker and set-model switches the model", async ({ page }) => {
  await page.goto(LOCAL_URL());
  const toggle = page.getByRole("button", { name: "Edit" });
  await expect(toggle).toHaveAttribute("aria-pressed", "false");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("#picker")).toBeVisible();
  await command(page, `set-model/${B}`);
  await expect.poll(async () => (await stateOf(page)).model).toBe(B);
  await toggle.click();
  await expect(page.locator("#picker")).toBeHidden();
});

test("characters and favorites persist in localStorage", async ({ page }) => {
  await page.goto(LOCAL_URL());
  await expect.poll(async () => (await stateOf(page)).model).toBeTruthy();
  await command(page, `add-character/${A}`);
  await expect.poll(async () => (await stateOf(page)).characters.length).toBe(2);
  await page.evaluate((id) => post(`/fav/${id}`, {}), VARIANTS[0]);
  await page.reload();
  await expect.poll(async () => (await stateOf(page)).characters.length).toBe(2);
  expect(await page.evaluate(async () => (await (await api("/prefs")).json()).favorites)).toEqual([VARIANTS[0]]);
});

test("the edit button is absent in server mode", async ({ page }) => {
  await page.goto(THEME_URL);
  await expect(page.locator("#editToggle")).toBeHidden();
});

test("a preset saves and loads in local mode and survives a reload", async ({ page }) => {
  await page.goto(LOCAL_URL());
  await expect.poll(async () => (await stateOf(page)).model).toBeTruthy();
  await command(page, `add-character/${A}`);
  await expect.poll(async () => (await stateOf(page)).characters.length).toBe(2);
  await command(page, "save-preset/duo");
  await command(page, "clear-characters");
  await expect.poll(async () => (await stateOf(page)).characters.length).toBe(0);
  await page.reload();
  await command(page, "load-preset/duo");
  await expect.poll(async () => (await stateOf(page)).characters.length).toBe(2);
  const presets = await page.evaluate(async () => (await (await api("/presets")).json()));
  expect(presets).toEqual([{ name: "duo", characters: 2 }]);
});

test("update and rename preset work in local mode", async ({ page }) => {
  await page.goto(LOCAL_URL());
  await expect.poll(async () => (await stateOf(page)).model).toBeTruthy();
  await command(page, "save-preset/a/b");
  await command(page, "save-preset/other");
  await command(page, "update-preset/typo");
  await command(page, `add-character/${A}`);
  await expect.poll(async () => (await stateOf(page)).characters.length).toBe(2);
  await command(page, "update-preset/a%2Fb");
  await command(page, `rename-preset/${encodeURIComponent("a/b")}/${encodeURIComponent("c/d e")}`);
  await command(page, `rename-preset/other/${encodeURIComponent("c/d e")}`);
  const presets = await page.evaluate(async () => (await (await api("/presets")).json()));
  expect(presets).toEqual([{ name: "c/d e", characters: 2 }, { name: "other", characters: 1 }]);
  expect((await stateOf(page)).preset).toBe("c/d e");
});

test("next and previous preset cycle in local mode and wrap", async ({ page }) => {
  await page.goto(LOCAL_URL());
  await expect.poll(async () => (await stateOf(page)).model).toBeTruthy();
  await command(page, `set-model/${A}`);
  await command(page, "save-preset/one");
  await command(page, `set-model/${B}`);
  await command(page, "save-preset/two");
  await command(page, "next-preset");
  await expect.poll(async () => (await stateOf(page)).preset).toBe("one");
  await command(page, "next-preset");
  await expect.poll(async () => (await stateOf(page)).preset).toBe("two");
  await command(page, "next-preset");
  await expect.poll(async () => (await stateOf(page)).preset).toBe("one");
  await command(page, "prev-preset");
  await expect.poll(async () => (await stateOf(page)).preset).toBe("two");
  expect((await stateOf(page)).model).toBe(B);
});
