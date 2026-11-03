import { test, expect, get, post, cmd, reported } from "../lib/fixture.mjs";
import { SOLO } from "../lib/models.mjs";

const A = SOLO["4.0"], B = SOLO["4.1"];
const pane = (page) => page.locator("#presets-pane");
const chars = async () => (await get("/state")).characters;

// The pane starts closed, so the models list keeps its height.
async function openPresets(page) {
  const toggle = page.getByRole("button", { name: "Presets", exact: true });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(pane(page)).toBeVisible();
}

async function saveThroughPicker(page, name) {
  if (await pane(page).isHidden()) await openPresets(page);
  await pane(page).getByRole("textbox", { name: "Preset name" }).fill(name);
  await pane(page).getByRole("button", { name: "Save" }).click();
}

test("the picker saves a two-character layout and loads it back", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await post("/layout", { replace: true, characters: [
    { model: A, x: -0.3, y: 0.1, scale: 1.2 },
    { model: B, x: 0.3, y: -0.1, scale: 0.6 },
  ] });
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
  await saveThroughPicker(page, "duo");
  await expect(pane(page).getByRole("status")).toHaveText('Saved "duo".');
  await expect(pane(page).getByRole("group", { name: "Saved presets" })).toContainText("duo (2)");

  await cmd("clear-characters");
  await expect.poll(async () => (await chars()).length).toBe(0);
  await pane(page).getByRole("button", { name: "duo (2)" }).click();
  await expect(pane(page).getByRole("status")).toHaveText('Loaded "duo".');
  await expect.poll(async () => (await chars()).length).toBe(2);
  const [first, second] = await chars();
  expect([first.model, first.x, first.scale]).toEqual([A, -0.3, 1.2]);
  expect([second.model, second.x, second.scale]).toEqual([B, 0.3, 0.6]);
  await expect.poll(() => page.locator("#layers canvas").count()).toBe(2);
});

test("a single-character preset brings the single player back", async ({ open, page }) => {
  await open({ model: B, edit: true });
  await expect.poll(reported(B)).toBeTruthy();
  await post("/view", { scale: 2, x: 0.2, y: 0.1 });
  await saveThroughPicker(page, "solo");
  await cmd(`add-character/${A}`);
  await expect.poll(async () => (await chars()).length).toBe(2);
  await pane(page).getByRole("button", { name: "solo (1)" }).click();
  await expect.poll(async () => (await chars()).length).toBe(0);
  await expect.poll(() => page.locator("#p canvas").count()).toBe(1);
  const state = await get("/state");
  expect([state.model, state.scale, state.x, state.y]).toEqual([B, 2, 0.2, 0.1]);
});

test("a preset deletes, and an empty name asks for one", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await openPresets(page);
  await pane(page).getByRole("button", { name: "Save" }).click();
  await expect(pane(page).getByRole("status")).toHaveText("Type a name first.");
  await saveThroughPicker(page, "gone");
  await pane(page).getByRole("button", { name: "Delete preset gone" }).click();
  await expect(pane(page).getByRole("status")).toHaveText('Deleted "gone".');
  await expect(pane(page).getByText("No presets yet.")).toBeVisible();
  expect((await get("/prefs")).presets).toEqual([]);
});

test("a preset stays after a reload of the page", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await saveThroughPicker(page, "keep");
  await page.reload();
  await openPresets(page);
  await expect(pane(page).getByRole("group", { name: "Saved presets" })).toContainText("keep (1)");
});

test("each new name adds an option to the list", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await saveThroughPicker(page, "first");
  await saveThroughPicker(page, "second");
  await expect(pane(page).getByRole("status")).toHaveText('Saved "second".');
  await expect(pane(page).getByRole("textbox", { name: "Preset name" })).toHaveValue("");
  const rows = pane(page).getByRole("group", { name: "Saved presets" }).locator(".sel");
  await expect(rows).toHaveText(["first (1)", "second (1)"]);
  expect((await get("/prefs")).presets.map((p) => p.name)).toEqual(["first", "second"]);
});

test("a preset saved from the CLI shows after the pane opens", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await cmd("save-preset/from-cli");
  await openPresets(page);
  await expect(pane(page).getByRole("group", { name: "Saved presets" }).locator(".sel")).toHaveText(["from-cli (1)"]);
});
