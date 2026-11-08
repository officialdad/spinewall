import { test, expect, get, cmd, THEME_URL } from "../lib/fixture.mjs";
import { SOLO } from "../lib/models.mjs";

const A = SOLO["4.0"], B = SOLO["4.1"];
const pane = (page) => page.locator("#presets-pane");
const rows = (page) => pane(page).getByRole("group", { name: "Saved presets" }).locator(".pload");

// Each monitor runs its own page. `?screen=` names the monitor, as the Plasma wallpaper cannot pass it any other way.
async function openScreen(context, name) {
  const page = await context.newPage();
  await page.goto(`${THEME_URL}?screen=${name}`);
  const toggle = page.getByRole("button", { name: "Presets", exact: true });
  await expect(toggle).toBeVisible();
  await toggle.click();
  await expect(pane(page)).toBeVisible();
  return page;
}

async function savePresets() {
  await cmd(`set-model/${A}`);
  await cmd("save-preset/alpha");
  await cmd(`set-model/${B}`);
  await cmd("save-preset/beta");
}

test("two screens load different presets and keep them apart", async ({ context, server }) => {
  await savePresets();
  await get("/edit/1");
  const left = await openScreen(context, "left");
  const right = await openScreen(context, "right");

  await rows(left).filter({ hasText: "alpha" }).click();
  await rows(right).filter({ hasText: "beta" }).click();

  await expect.poll(async () => (await get("/state?screen=left")).model).toBe(A);
  await expect.poll(async () => (await get("/state?screen=right")).model).toBe(B);
  await expect(rows(left).filter({ hasText: "alpha" })).toHaveAttribute("aria-current", "true");
  await expect(rows(left).filter({ hasText: "beta" })).not.toHaveAttribute("aria-current", "true");
  await expect(rows(right).filter({ hasText: "beta" })).toHaveAttribute("aria-current", "true");
  await expect(pane(left)).toContainText("Screen left.");
  expect((await get("/screens")).map((s) => [s.screen, s.preset])).toEqual([["left", "alpha"], ["right", "beta"]]);
});

test("Next and Previous cycle one screen and wrap", async ({ context, server }) => {
  await get("/edit/1");
  const left = await openScreen(context, "left");
  const right = await openScreen(context, "right");
  // The presets arrive after both screens exist, so neither starts on one.
  await savePresets();

  await pane(left).getByRole("button", { name: "Next", exact: true }).click();
  await expect(pane(left).getByRole("status")).toHaveText('Loaded "alpha".');
  await pane(left).getByRole("button", { name: "Next", exact: true }).click();
  await expect(pane(left).getByRole("status")).toHaveText('Loaded "beta".');
  await pane(left).getByRole("button", { name: "Next", exact: true }).click();
  await expect(pane(left).getByRole("status")).toHaveText('Loaded "alpha".');
  await pane(left).getByRole("button", { name: "Previous", exact: true }).click();
  await expect(pane(left).getByRole("status")).toHaveText('Loaded "beta".');

  expect((await get("/state?screen=left")).preset).toBe("beta");
  expect((await get("/state?screen=right")).preset).toBeNull();
  await expect(pane(right).locator('[aria-current="true"]')).toHaveCount(0);
});

test("a CLI cycle shows on the page of each screen", async ({ context, server }) => {
  await savePresets();
  await get("/edit/1");
  const left = await openScreen(context, "left");
  const right = await openScreen(context, "right");
  await cmd("load-preset/alpha");
  await get("/cmd/load-preset/beta?screen=right");

  await get("/cmd/next-preset");
  await expect(rows(left).filter({ hasText: "beta" })).toHaveAttribute("aria-current", "true");
  await expect(rows(right).filter({ hasText: "alpha" })).toHaveAttribute("aria-current", "true");
});

test("Next with no presets says so", async ({ context, server }) => {
  await get("/edit/1");
  const left = await openScreen(context, "left");
  await pane(left).getByRole("button", { name: "Next", exact: true }).click();
  await expect(pane(left).getByRole("status")).toHaveText("No presets yet.");
});
