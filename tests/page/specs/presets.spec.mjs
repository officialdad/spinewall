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

const cards = (page) => pane(page).locator(".pload");
const manage = (page, name) => pane(page).getByRole("button", { name: `Manage preset ${name}` }).click();

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
  await expect(pane(page).getByRole("button", { name: "duo 2 characters" })).toBeVisible();

  await cmd("clear-characters");
  await expect.poll(async () => (await chars()).length).toBe(0);
  await pane(page).getByRole("button", { name: "duo 2 characters" }).click();
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
  await pane(page).getByRole("button", { name: "solo 1 character" }).click();
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
  await manage(page, "gone");
  await pane(page).getByRole("button", { name: "Delete preset gone" }).click();
  await pane(page).getByRole("button", { name: "Confirm: delete preset gone" }).click();
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
  await expect(pane(page).getByRole("button", { name: "keep 1 character" })).toBeVisible();
});

test("each new name adds an option to the list", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await saveThroughPicker(page, "first");
  await saveThroughPicker(page, "second");
  await expect(pane(page).getByRole("status")).toHaveText('Saved "second".');
  await expect(pane(page).getByRole("textbox", { name: "Preset name" })).toHaveValue("");
  await expect(cards(page)).toHaveText(["first1 character", "second1 character"]);
  expect((await get("/prefs")).presets.map((p) => p.name)).toEqual(["first", "second"]);
});

test("a preset saved from the CLI shows after the pane opens", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await cmd("save-preset/from-cli");
  await openPresets(page);
  await expect(pane(page).getByRole("group", { name: "Saved presets" }).locator(".pload")).toHaveText(["from-cli1 character"]);
});

test("a card shows one picture per character and a +N chip past three", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await post("/layout", { replace: true, characters: [A, B, A, B, A].map((model, i) => ({ model, x: i * 0.1 - 0.2, y: 0, scale: 0.5 })) });
  await saveThroughPicker(page, "crowd");
  await cmd("clear-characters");
  await saveThroughPicker(page, "alone");
  const crowd = pane(page).locator(".pcard", { hasText: "crowd" });
  await expect(crowd.locator(".pth:not(.more)")).toHaveCount(2);
  await expect(crowd.locator(".pth.more")).toHaveAttribute("data-more", "+3");
  const alone = pane(page).locator(".pcard", { hasText: "alone" });
  await expect(alone.locator(".pth")).toHaveCount(1);
  // The picture is the model thumbnail, drawn once the card is on screen.
  await expect(alone.locator(".pth[data-ready] img")).toBeVisible();
  await expect(alone.locator(".pth img")).toHaveAttribute("src", /^blob:/);
});

test("Rename keeps the position, the layout, and the loaded mark", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await saveThroughPicker(page, "one");
  await saveThroughPicker(page, "two");
  await saveThroughPicker(page, "three");
  await manage(page, "two");
  await pane(page).getByRole("button", { name: "Rename preset two" }).click();
  const box = pane(page).getByRole("textbox", { name: "New name for preset two" });
  await expect(box).toBeFocused();
  await expect(box).toHaveValue("two");
  await box.fill("middle");
  await box.press("Enter");
  await expect(pane(page).getByRole("status")).toHaveText('Renamed "two" to "middle".');
  await expect(cards(page)).toHaveText(["one1 character", "middle1 character", "three1 character"]);
  expect((await get("/prefs")).presets.map((p) => p.name)).toEqual(["one", "middle", "three"]);
  // The loaded mark stays with the renamed preset.
  await page.getByRole("button", { name: "Manage preset three" }).click();
  await pane(page).getByRole("button", { name: "Rename preset three" }).click();
  await pane(page).getByRole("textbox", { name: "New name for preset three" }).fill("middle");
  await pane(page).getByRole("button", { name: "Save the new name of preset three" }).click();
  await expect(pane(page).getByRole("status")).toHaveText('Cannot rename. "middle" is taken.');
  expect((await get("/prefs")).presets.map((p) => p.name)).toEqual(["one", "middle", "three"]);
});

test("Rename of the loaded preset moves the loaded mark", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await saveThroughPicker(page, "old");
  await expect(pane(page).getByRole("button", { name: "old 1 character" })).toHaveAttribute("aria-current", "true");
  await manage(page, "old");
  await pane(page).getByRole("button", { name: "Rename preset old" }).click();
  await pane(page).getByRole("textbox", { name: "New name for preset old" }).fill("fresh");
  await pane(page).getByRole("button", { name: "Save the new name of preset old" }).click();
  await expect(pane(page).getByRole("button", { name: "fresh 1 character" })).toHaveAttribute("aria-current", "true");
  expect((await get("/state")).preset).toBe("fresh");
});

test("Update replaces a preset with the layout on screen after a second press", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await saveThroughPicker(page, "desk");
  await cmd(`add-character/${B}`);
  await expect.poll(async () => (await chars()).length).toBe(2);
  await manage(page, "desk");
  await pane(page).getByRole("button", { name: "Update preset desk" }).click();
  // The first press only asks. Nothing changes yet.
  expect((await get("/prefs")).presets[0].characters).toHaveLength(1);
  await pane(page).getByRole("button", { name: "Confirm: update preset desk" }).click();
  await expect(pane(page).getByRole("status")).toHaveText('Updated "desk" with the layout on screen.');
  const [preset] = (await get("/prefs")).presets;
  expect(preset.characters.map((c) => c.model)).toEqual([A, B]);
  await expect(pane(page).getByRole("button", { name: "desk 2 characters" })).toBeVisible();
  expect((await get("/prefs")).presets).toHaveLength(1);
});

test("Escape backs out of a confirm, then the card, then the search, then the pane", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  await saveThroughPicker(page, "desk");
  await manage(page, "desk");
  await pane(page).getByRole("button", { name: "Delete preset desk" }).click();
  await expect(pane(page).getByRole("button", { name: "Confirm: delete preset desk" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(pane(page).getByRole("button", { name: "Delete preset desk" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(pane(page).getByRole("button", { name: "Delete preset desk" })).toHaveCount(0);
  await pane(page).getByRole("searchbox", { name: "Search presets" }).fill("zz");
  await page.keyboard.press("Escape");
  await expect(pane(page).getByRole("searchbox", { name: "Search presets" })).toHaveValue("");
  await page.keyboard.press("Escape");
  await expect(pane(page)).toBeHidden();
  await expect(page.getByRole("button", { name: "Presets", exact: true })).toBeFocused();
});

test("a long preset list scrolls inside the pane and a search narrows it", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  for (let i = 1; i <= 60; i++) await cmd(`save-preset/preset ${String(i).padStart(2, "0")}`);
  await cmd("save-preset/Sunset walk");
  await openPresets(page);
  await expect(pane(page).locator(".count")).toHaveText("61");
  // The open pane takes the panel height, so the models list steps aside and the cards scroll.
  await expect(page.locator("#models-pane")).toBeHidden();
  const list = pane(page).locator("#presetlist");
  const box = await list.boundingBox();
  expect(box.height).toBeGreaterThan(300);
  expect(await list.evaluate((el) => el.scrollHeight > el.clientHeight)).toBe(true);
  // Two columns or more, so the list is not one long column.
  const first = await cards(page).nth(0).boundingBox(), second = await cards(page).nth(1).boundingBox();
  expect(second.y).toBe(first.y);

  await pane(page).getByRole("searchbox", { name: "Search presets" }).fill("sunset");
  await expect(cards(page)).toHaveText(["Sunset walk1 character"]);
  await expect(pane(page).locator(".count")).toHaveText("1 of 61");
  await pane(page).getByRole("searchbox", { name: "Search presets" }).fill("nothing here");
  await expect(pane(page).getByText('No preset matches "nothing here".')).toBeVisible();
  await pane(page).getByRole("searchbox", { name: "Search presets" }).fill("");
  await expect(cards(page)).toHaveCount(61);
  // Only the cards near the viewport load pictures.
  const loaded = await pane(page).locator(".pth[data-ready]").count();
  expect(loaded).toBeLessThan(61);
});

test("Next scrolls the loaded card into view", async ({ open, page }) => {
  await open({ model: A, edit: true });
  await expect.poll(reported(A)).toBeTruthy();
  for (let i = 1; i <= 40; i++) await cmd(`save-preset/p${String(i).padStart(2, "0")}`);
  await cmd("load-preset/p01");
  await openPresets(page);
  for (let i = 0; i < 39; i++) await pane(page).getByRole("button", { name: "Next" }).click();
  const current = pane(page).locator(".pcard[data-current]");
  await expect(current).toContainText("p40");
  await expect(current).toBeInViewport();
});
