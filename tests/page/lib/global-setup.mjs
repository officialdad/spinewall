import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { generateModels } from "./models.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
export const ROOT = path.resolve(here, "..");
export const WORK = path.join(ROOT, ".work");
export const HOME = path.join(WORK, "home");
export const MODELS = path.join(WORK, "models");

export default function globalSetup() {
  execFileSync("bash", [path.join(ROOT, "setup.sh")], { stdio: "inherit" });
  if (!fs.existsSync(path.join(HOME, ".local/share/webwallpaper/themes/spinewall/index.html"))) {
    throw new Error("setup.sh did not install the theme");
  }
  fs.rmSync(path.join(WORK, "thumbs"), { recursive: true, force: true });
  generateModels(MODELS);
  // The installed copy is what a user runs, so refresh it from the working tree each run.
  const repo = path.resolve(ROOT, "../..");
  const theme = path.join(HOME, ".local/share/webwallpaper/themes/spinewall");
  // The theme hardcodes port 8777. Only the temp copy changes, so a busy 8777 never blocks a local run.
  const port = process.env.SPINEWALL_PORT_TEST || "8777";
  const html = fs.readFileSync(path.join(repo, "theme/index.html"), "utf8");
  if (!html.includes("http://127.0.0.1:8777")) throw new Error("theme/index.html no longer hardcodes port 8777. Update tests/page/lib/global-setup.mjs.");
  fs.writeFileSync(path.join(theme, "index.html"), html.replace("http://127.0.0.1:8777", `http://127.0.0.1:${port}`));
  fs.copyFileSync(path.join(repo, "theme/local-host.js"), path.join(theme, "local-host.js"));
}
