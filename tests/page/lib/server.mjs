import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { HOME, MODELS, ROOT } from "./global-setup.mjs";

export const PORT = Number(process.env.SPINEWALL_PORT_TEST || 8777);
export const API = `http://127.0.0.1:${PORT}`;
export const THEME_URL = "file://" + path.join(HOME, ".local/share/webwallpaper/themes/spinewall/index.html");
const SERVER = path.resolve(ROOT, "../../server/spinewall-server.py");
const THUMBS = path.join(ROOT, ".work/thumbs");

async function until(fn, ms = 10_000) {
  const end = Date.now() + ms;
  for (;;) {
    try {
      const v = await fn();
      if (v) return v;
    } catch {}
    if (Date.now() > end) throw new Error("timed out waiting for the server");
    await new Promise((r) => setTimeout(r, 50));
  }
}

// A fresh state directory per server, so no test sees another test's favorites or layout.
export async function startServer({ models = MODELS, wait = true } = {}) {
  const state = fs.mkdtempSync(path.join(os.tmpdir(), "spinewall-state-"));
  const env = {
    ...process.env,
    SPINEWALL_PORT: String(PORT),
    SPINEWALL_MODELS: models,
    SPINEWALL_STATE: path.join(state, "state.json"),
    SPINEWALL_PREFS: path.join(state, "prefs.json"),
    SPINEWALL_LAYOUT: path.join(state, "layout.json"),
    SPINEWALL_THUMBS: THUMBS,
    HOME: state,
  };
  fs.mkdirSync(THUMBS, { recursive: true });
  // Another server on this port would answer instead of ours, and every test would read the wrong state.
  const taken = await fetch(`${API}/state`).then(() => true, () => false);
  if (taken) throw new Error(`port ${PORT} is in use. Stop that server or set SPINEWALL_PORT_TEST to a free port.`);
  const child = spawn("python3", ["-I", SERVER], { env, stdio: ["ignore", "ignore", "pipe"] });
  let log = "";
  child.stderr.on("data", (d) => { log += d; });
  const server = {
    log: () => log,
    ready: () => until(async () => (await fetch(`${API}/state`)).ok),
    async stop() {
      if (child.exitCode === null) {
        child.kill("SIGTERM");
        await new Promise((r) => child.once("exit", r));
      }
      fs.rmSync(state, { recursive: true, force: true });
    },
  };
  if (wait) await server.ready();
  return server;
}

export const get = async (p) => (await fetch(`${API}${p}`)).json();
export const post = async (p, body) =>
  (await fetch(`${API}${p}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })).json();
export const cmd = (p) => get(`/cmd/${p}`);
