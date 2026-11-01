// Synthetic Spine models. Each is a flat colored square, so no game data is involved.
import fs from "node:fs";
import path from "node:path";
import { PNG } from "pngjs";

// The editor version string the server reads to choose a runtime.
const VERSIONS = { "3.8": "3.8.99", "4.0": "4.0.64", "4.1": "4.1.24", "4.2": "4.2.43" };

const ANIMATIONS = { idle: 5, wave: 15, spin: 30 };

function squarePng(rgb) {
  const png = new PNG({ width: 64, height: 64 });
  for (let i = 0; i < png.data.length; i += 4) {
    png.data[i] = rgb[0];
    png.data[i + 1] = rgb[1];
    png.data[i + 2] = rgb[2];
    png.data[i + 3] = 255;
  }
  return PNG.sync.write(png);
}

function atlas(id, regions) {
  const head = `${id}.png\nsize: 64,64\nformat: RGBA8888\nfilter: Linear,Linear\nrepeat: none\n`;
  const body = regions
    .map((r) => `${r}\n  rotate: false\n  xy: 0, 0\n  size: 64, 64\n  orig: 64, 64\n  offset: 0, 0\n  index: -1\n`)
    .join("");
  return head + body;
}

function skeleton(version, slots) {
  const editor = VERSIONS[version];
  const attachments = {};
  for (const s of slots) attachments[s.name] = { [s.name]: { x: 0, y: s.y ?? 50, width: s.size ?? 100, height: s.size ?? 100 } };
  const animations = {};
  for (const [name, angle] of Object.entries(ANIMATIONS)) {
    const end = name === "idle" ? 2 : 1;
    animations[name] = {
      bones: {
        root: {
          rotate: [
            { time: 0, angle: 0 },
            { time: end / 2, angle },
            { time: end, angle: 0 },
          ],
        },
      },
    };
  }
  return {
    skeleton: { hash: "synthetic", spine: editor, x: -50, y: 0, width: 100, height: 100 },
    bones: [{ name: "root" }],
    slots: slots.map((s) => ({ name: s.name, bone: "root", attachment: s.name })),
    skins: [{ name: "default", attachments }],
    animations,
  };
}

function writeModel(dir, id, { version, slots, meta = {}, color = [200, 80, 80] }) {
  const modelDir = path.join(dir, id);
  fs.mkdirSync(modelDir, { recursive: true });
  fs.writeFileSync(path.join(modelDir, `${id}.json`), JSON.stringify(skeleton(version, slots)));
  fs.writeFileSync(path.join(modelDir, `${id}.atlas`), atlas(id, slots.map((s) => s.name)));
  fs.writeFileSync(path.join(modelDir, `${id}.png`), squarePng(color));
  if (Object.keys(meta).length) fs.writeFileSync(path.join(modelDir, "model.json"), JSON.stringify(meta));
}

const BODY = [{ name: "body" }];
const SCENE = [
  { name: "bg_sky", size: 400, y: 200 },
  { name: "bg_floor", size: 400, y: 0 },
];

export const SOLO = { "3.8": "v38-solo", "4.0": "v40-solo", "4.1": "v41-solo", "4.2": "v42-solo" };
export const SCENES = { "4.0": "scene-v40", "4.2": "scene-v42" };
export const HIDE = "hide-v40";
export const VARIANTS = ["alice_01", "alice_02", "alice_03"];
export const BULK_COUNT = 60;
export const ANIMATION_NAMES = Object.keys(ANIMATIONS);

const letters = "abdefghijklmnopqrstuvwxyz";
export const bulkId = (n) => `pk${letters[Math.floor(n / letters.length)]}${letters[n % letters.length]}`;

// Returns the number of models written.
export function generateModels(dir) {
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  let count = 0;
  const add = (id, spec) => { writeModel(dir, id, spec); count++; };
  for (const [version, id] of Object.entries(SOLO)) {
    add(id, { version, slots: BODY, meta: { name: `Solo ${version}`, group: version === "3.8" ? "Legacy" : "Modern" } });
  }
  for (const [version, id] of Object.entries(SCENES)) {
    add(id, { version, slots: SCENE, color: [60, 90, 200], meta: { name: `Scene ${version}`, group: "Modern" } });
  }
  add(HIDE, {
    version: "4.0",
    slots: [{ name: "bg_back", size: 300, y: 150 }, ...BODY],
    meta: { name: "Hidden Backdrop", group: "Modern", hideSlots: ["bg"], kind: "character" },
  });
  VARIANTS.forEach((id, i) => add(id, { version: "4.0", slots: BODY, color: [80, 200, 120], meta: { name: `Alice ${"ABC"[i]}`, group: "Variants" } }));
  for (let n = 0; n < BULK_COUNT; n++) {
    add(bulkId(n), { version: "4.1", slots: BODY, color: [200, 200, 80], meta: { name: `Pack ${n + 1}`, group: "Bulk" } });
  }
  return count;
}

export const TOTAL_MODELS = Object.keys(SOLO).length + Object.keys(SCENES).length + 1 + VARIANTS.length + BULK_COUNT;
