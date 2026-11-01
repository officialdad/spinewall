import { defineConfig } from "@playwright/test";

// The theme hardcodes port 8777, and every test starts its own server there, so tests run one at a time.
export default defineConfig({
  testDir: "./specs",
  workers: 1,
  fullyParallel: false,
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [["list"]],
  globalSetup: "./lib/global-setup.mjs",
  use: {
    headless: true,
    viewport: { width: 1280, height: 800 },
    launchOptions: {
      args: [
        "--use-gl=angle",
        "--use-angle=swiftshader",
        "--enable-unsafe-swiftshader",
        "--ignore-gpu-blocklist",
        "--allow-file-access-from-files",
      ],
    },
  },
});
