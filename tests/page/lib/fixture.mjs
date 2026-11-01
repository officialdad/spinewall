import { test as base, expect } from "@playwright/test";
import { startServer, THEME_URL, API, get, post, cmd } from "./server.mjs";

// Console lines the page writes on purpose, and browser noise that is not a page error.
const EXPECTED_CONSOLE = [/spine load error/];

export const test = base.extend({
  server: async ({}, use) => {
    const server = await startServer();
    await use(server);
    await server.stop();
  },
  // `consoleErrors` holds every error the page raised. The "no console errors" test reads it.
  consoleErrors: async ({}, use) => { await use([]); },
  open: async ({ page, server, consoleErrors }, use) => {
    page.on("pageerror", (e) => consoleErrors.push(`pageerror: ${e.message}`));
    page.on("console", (m) => {
      if (m.type() !== "error") return;
      const text = m.text();
      if (EXPECTED_CONSOLE.some((re) => re.test(text))) return;
      consoleErrors.push(text);
    });
    await use(async ({ edit = false, model } = {}) => {
      if (model) await cmd(`set-model/${model}`);
      if (edit) await get("/edit/1");
      await page.goto(THEME_URL);
      return page;
    });
  },
});

export { expect, API, get, post, cmd, THEME_URL };

// The model reported its animations, so the player or stage is ready.
export const reported = (model) => async () => {
  const info = await get("/info");
  return info.state.model === model && info.animations.length > 0 ? info.animations : null;
};
