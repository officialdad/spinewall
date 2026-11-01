// Runs the server's state logic inside the page, for hosts that have no Spinewall server (browser, Lively).
// It answers the same paths as server/spinewall-server.py. Change both together.
(() => {
const SCALE_MIN = 0.3, SCALE_MAX = 8, POS_MAX = 2, RECENT_MAX = 20, ANIMATION_MAX_CHARS = 200;
const VIEW_DEFAULT = {scale: 1, x: 0, y: 0};
const PREF_CHOICES = {layout: ["grid", "list"], sort: ["name", "recent", "source"], kind: ["character", "scene", "all"]};
const UID_PATTERN = /^[A-Za-z0-9_-]{1,32}$/;
const RUNTIME_BY_VERSION = {"4.0": "spine40", "4.1": "spine41", "4.2": "spine42", "3.8": "spine38"};
const STORAGE_KEY = "spinewall";

const clamp = (value, low, high, fallback) => {
  if (typeof value === "string" && !value.trim()) return fallback;
  if (value === null || typeof value === "boolean") return fallback;
  const n = Number(value);
  return Number.isFinite(n) ? Math.max(low, Math.min(high, n)) : fallback;
};
const isAnimationName = v => typeof v === "string" && v.length > 0 && v.length <= ANIMATION_MAX_CHARS;
const randomUid = taken => {
  for (;;) {
    const uid = [...crypto.getRandomValues(new Uint8Array(3))].map(b => b.toString(16).padStart(2, "0")).join("");
    if (!taken.has(uid)) return uid;
  }
};
const json = (obj, status = 200) =>
  new Response(JSON.stringify(obj), {status, headers: {"Content-Type": "application/json"}});

// The static list may omit what a server scan derives, so the same defaults apply here.
function completeModels(list, runtimeVersion) {
  const models = list.map(m => ({
    kind: "character", skin: "default", hideSlots: [], defaultAnimation: "idle", group: null, ...m,
    runtime: m.runtime || RUNTIME_BY_VERSION[String(m.spine || m.version || runtimeVersion || "").split(".").slice(0, 2).join(".")],
  }));
  const stems = {}, ids = new Set(models.map(m => m.id));
  for (const m of models) {
    const match = /^(.+?)[_-]\d+$/.exec(m.id);
    if (match) (stems[match[1]] ||= []).push(m);
  }
  for (const m of models) {
    if (m.character) continue;
    const match = /^(.+?)[_-]\d+$/.exec(m.id);
    const stem = match && match[1];
    m.character = stem && (stems[stem].length >= 2 || ids.has(stem)) ? stem : m.id;
  }
  return models;
}

window.createLocalHost = function createLocalHost({models: list, runtimeVersion, storage = window.localStorage}) {
  const models = completeModels(list, runtimeVersion);
  const ids = models.map(m => m.id);
  const anims = {};
  const firstCharacter = () => (models.find(m => m.kind === "character") || models[0] || {}).id ?? null;

  const saved = (() => { try { return JSON.parse(storage.getItem(STORAGE_KEY)) || {}; } catch (e) { return {}; } })();
  const persist = () => {
    try { storage.setItem(STORAGE_KEY, JSON.stringify({state: persistedState(), characters: state.characters, crev: state.crev, prefs})); } catch (e) {}
  };

  function cleanCharacters(raw, allowedUids) {
    if (!Array.isArray(raw)) return [];
    const cleaned = [], seen = new Set();
    for (const entry of raw) {
      if (!entry || typeof entry !== "object" || !ids.includes(entry.model)) continue;
      let uid = entry.uid;
      if (uid == null) uid = randomUid(new Set([...seen, ...(allowedUids || [])]));
      else if (typeof uid !== "string" || !UID_PATTERN.test(uid) || seen.has(uid)) continue;
      else if (allowedUids && !allowedUids.has(uid)) continue;
      seen.add(uid);
      cleaned.push({
        uid, model: entry.model, animation: isAnimationName(entry.animation) ? entry.animation : null,
        x: clamp(entry.x, -POS_MAX, POS_MAX, 0), y: clamp(entry.y, -POS_MAX, POS_MAX, 0),
        scale: clamp(entry.scale, SCALE_MIN, SCALE_MAX, 1), auto: entry.auto === true,
      });
    }
    return cleaned;
  }

  const state = {model: firstCharacter(), animation: null, rev: 0, ...VIEW_DEFAULT, vrev: 0, auto: false, characters: [], crev: 0, edit: 0};
  const persistedState = () => { const {characters, crev, edit, ...rest} = state; return rest; };
  const s = saved.state;
  if (s && ids.includes(s.model)) {
    state.model = s.model;
    state.animation = isAnimationName(s.animation) ? s.animation : null;
    state.rev = Math.max(0, Number.isInteger(s.rev) ? s.rev : 0);
    state.vrev = Math.max(0, Number.isInteger(s.vrev) ? s.vrev : 0);
    state.auto = s.auto === true;
    state.scale = clamp(s.scale, SCALE_MIN, SCALE_MAX, 1);
    state.x = clamp(s.x, -POS_MAX, POS_MAX, 0);
    state.y = clamp(s.y, -POS_MAX, POS_MAX, 0);
  }
  state.characters = cleanCharacters(saved.characters);
  state.crev = Math.max(0, Number.isInteger(saved.crev) ? saved.crev : 0);

  const prefs = {favorites: [], recent: [], layout: "grid", sort: "name", kind: "character", variants: true};
  const sp = saved.prefs || {};
  for (const key of ["favorites", "recent"]) if (Array.isArray(sp[key])) prefs[key] = sp[key].filter(i => typeof i === "string");
  for (const [key, choices] of Object.entries(PREF_CHOICES)) if (choices.includes(sp[key])) prefs[key] = sp[key];
  if (typeof sp.variants === "boolean") prefs.variants = sp.variants;

  const setCharacters = chars => { state.characters = chars; state.crev++; persist(); };
  const findCharacter = uid => state.characters.find(c => c.uid === uid);
  const updateCharacter = (uid, changes) => {
    const chars = state.characters.map(c => (c.uid === uid ? {...c, ...changes} : c));
    if (JSON.stringify(chars) !== JSON.stringify(state.characters)) setCharacters(chars);
  };
  const defaultAnimation = id => {
    const names = anims[id] || [];
    const model = models.find(m => m.id === id) || {};
    return [model.defaultAnimation, "idle"].find(n => names.includes(n)) ?? names[0] ?? null;
  };
  const changeAnimation = name => { state.animation = name; state.rev++; persist(); };
  const changeModel = id => {
    if (id === state.model) return;
    state.model = id; state.animation = null; state.rev++;
    prefs.recent = [id, ...prefs.recent.filter(i => i !== id)].slice(0, RECENT_MAX);
    persist();
  };
  const setView = (view) => {
    if (view.scale != null) state.scale = clamp(view.scale, SCALE_MIN, SCALE_MAX, state.scale);
    if (view.x != null) state.x = clamp(view.x, -POS_MAX, POS_MAX, state.x);
    if (view.y != null) state.y = clamp(view.y, -POS_MAX, POS_MAX, state.y);
    state.vrev++; persist();
  };

  function addCharacter(id) {
    let chars = state.characters;
    if (!chars.length) {
      chars = [{uid: randomUid(new Set()), model: state.model, animation: state.animation, x: state.x, y: state.y, scale: state.scale, auto: state.auto}];
    }
    const n = chars.length;
    const side = n % 2 ? 1 : -1;
    const step = (Math.floor((n + 1) / 2) - 1) % 4 + 1;
    setCharacters([...chars, {
      uid: randomUid(new Set(chars.map(c => c.uid))), model: ids.includes(id) ? id : state.model,
      animation: null, x: side * step * 0.2, y: 0, scale: 0.7, auto: false,
    }]);
  }

  function removeCharacter(uid) {
    let chars = state.characters.filter(c => c.uid !== uid);
    if (chars.length === state.characters.length) return;
    if (chars.length === 1) {
      const last = chars[0];
      chars = [];
      if (last.model !== state.model || last.animation || last.auto !== state.auto) {
        Object.assign(state, {model: last.model, animation: last.animation, auto: last.auto});
        state.rev++;
      }
      Object.assign(state, {scale: last.scale, x: last.x, y: last.y});
      state.vrev++;
    }
    setCharacters(chars);
  }

  function cycleCharacterAnimation(uid, step) {
    const char = findCharacter(uid);
    const names = char && anims[char.model];
    if (!names || !names.length) return;
    const i = names.indexOf(char.animation || defaultAnimation(char.model));
    updateCharacter(uid, {animation: names[(i + step + names.length) % names.length]});
  }

  function setAutoCycle(arg) {
    const slash = arg.lastIndexOf("/");
    const value = arg.slice(slash + 1), uid = slash < 0 ? "" : arg.slice(0, slash);
    if (value !== "on" && value !== "off") return;
    const on = value === "on";
    if (slash >= 0) updateCharacter(uid, {auto: on});
    else if (state.characters.length) setCharacters(state.characters.map(c => ({...c, auto: on})));
    else if (state.auto !== on) { state.auto = on; state.rev++; persist(); }
  }

  function setLayout(body) {
    const existing = new Map(state.characters.map(c => [c.uid, c]));
    const replace = body.replace === true;
    let pushed = body.characters.filter(e => e && typeof e === "object");
    pushed = pushed.map(e => (typeof e.auto === "boolean" ? e : (({auto, ...rest}) => rest)(e)));
    if (!replace) pushed = pushed.filter(e => existing.has(e.uid)).map(({model, animation, ...rest}) => rest);
    const merged = pushed.map(e => ({...(existing.get(typeof e.uid === "string" ? e.uid : null) || {}), ...e}));
    let cleaned = cleanCharacters(merged, replace ? null : new Set(existing.keys()));
    if (!replace) {
      const mentioned = new Set(cleaned.map(c => c.uid));
      cleaned = [...cleaned, ...[...existing].filter(([uid]) => !mentioned.has(uid)).map(([, c]) => c)];
    }
    if (JSON.stringify(cleaned) !== JSON.stringify(state.characters)) setCharacters(cleaned);
  }

  function runCommand(name, arg) {
    if (!ids.length) return;
    const current = state.model;
    const names = anims[current];
    const stepAnimation = step => {
      if (!names || !names.length) return;
      const i = names.indexOf(state.animation);
      changeAnimation(names[(i + step + names.length) % names.length]);
    };
    if (name === "zoom-in") setView({scale: state.scale * 1.1});
    else if (name === "zoom-out") setView({scale: state.scale / 1.1});
    else if (name === "move-left") setView({x: state.x - 0.05});
    else if (name === "move-right") setView({x: state.x + 0.05});
    else if (name === "move-up") setView({y: state.y - 0.05});
    else if (name === "move-down") setView({y: state.y + 0.05});
    else if (name === "reset-view") setView(VIEW_DEFAULT);
    else if (name === "next-model") changeModel(ids[(ids.indexOf(current) + 1) % ids.length]);
    else if (name === "prev-model") changeModel(ids[(ids.indexOf(current) - 1 + ids.length) % ids.length]);
    else if (name === "random-model") changeModel(ids[Math.floor(Math.random() * ids.length)]);
    else if (name === "set-model" && ids.includes(arg)) changeModel(arg);
    else if (name === "next-anim") stepAnimation(1);
    else if (name === "prev-anim") stepAnimation(-1);
    else if (name === "random-anim") { if (names && names.length) changeAnimation(names[Math.floor(Math.random() * names.length)]); }
    else if (name === "set-anim" && arg) changeAnimation(arg.slice(0, ANIMATION_MAX_CHARS));
    else if (name === "add-character") addCharacter(arg);
    else if (name === "clear-characters" && state.characters.length) setCharacters([]);
    else if (name === "remove-character" && arg) removeCharacter(arg);
    else if ((name === "next-char-anim" || name === "prev-char-anim") && arg) cycleCharacterAnimation(arg, name === "next-char-anim" ? 1 : -1);
    else if (name === "auto-cycle" && arg) setAutoCycle(arg);
    else if ((name === "set-char-anim" || name === "set-char-model") && arg) {
      const slash = arg.indexOf("/");
      const uid = slash < 0 ? arg : arg.slice(0, slash), value = slash < 0 ? "" : arg.slice(slash + 1);
      if (name === "set-char-anim" && value) updateCharacter(uid, {animation: value.slice(0, ANIMATION_MAX_CHARS)});
      else if (name === "set-char-model" && ids.includes(value)) updateCharacter(uid, {model: value, animation: null});
    }
  }

  // Thumbnails stay in memory. The page renders them again on each load.
  const thumbs = new Map();

  function handle(path, init) {
    const method = (init && init.method) || "GET";
    const body = init && init.body ? JSON.parse(init.body) : {};
    if (method === "GET") {
      if (path === "/state") return json(state);
      if (path === "/prefs") return json(prefs);
      if (path === "/models" || path === "/rescan") return json(models);
      if (path.startsWith("/thumb/") && path.endsWith(".png")) {
        const blob = thumbs.get(decodeURIComponent(path.slice(7, -4)));
        return blob ? new Response(blob, {headers: {"Content-Type": "image/png"}}) : json({error: "no thumbnail"}, 404);
      }
      if (path.startsWith("/edit/")) { state.edit = path.endsWith("/1") ? 1 : 0; return json({on: state.edit}); }
      if (path.startsWith("/cmd/")) {
        const rest = path.slice(5), slash = rest.indexOf("/");
        runCommand(slash < 0 ? rest : rest.slice(0, slash), slash < 0 ? null : decodeURIComponent(rest.slice(slash + 1)) || null);
        return json(state);
      }
      return json({error: "not found"}, 404);
    }
    if (path === "/report") {
      if (!ids.includes(body.model) || !Array.isArray(body.animations) || !body.animations.every(isAnimationName)) return json({error: "bad report"}, 400);
      anims[body.model] = body.animations;
      if (body.model === state.model && !state.animation && isAnimationName(body.playing)) state.animation = body.playing;
      return json({ok: true});
    }
    if (path.startsWith("/thumb/")) {
      const id = decodeURIComponent(path.slice(7));
      if (!ids.includes(id) || typeof body.png !== "string") return json({error: "bad thumbnail"}, 400);
      thumbs.set(id, new Blob([Uint8Array.from(atob(body.png), c => c.charCodeAt(0))], {type: "image/png"}));
      return json({ok: true});
    }
    if (path.startsWith("/fav/")) {
      const id = decodeURIComponent(path.slice(5));
      if (!ids.includes(id)) return json({error: "unknown model"}, 404);
      prefs.favorites = prefs.favorites.includes(id) ? prefs.favorites.filter(f => f !== id) : [...prefs.favorites, id];
      persist();
      return json(prefs);
    }
    if (path === "/prefs") {
      let changed = false;
      for (const [key, choices] of Object.entries(PREF_CHOICES)) if (choices.includes(body[key])) { prefs[key] = body[key]; changed = true; }
      if (typeof body.variants === "boolean") { prefs.variants = body.variants; changed = true; }
      if (!changed) return json({error: "bad prefs"}, 400);
      persist();
      return json(prefs);
    }
    if (path === "/layout") {
      if (!Array.isArray(body.characters)) return json({error: "bad layout"}, 400);
      setLayout(body);
      return json({characters: state.characters, crev: state.crev});
    }
    if (path === "/view") { setView(body); return json({ok: true}); }
    if (path === "/input") return json({ok: true});
    return json({error: "not found"}, 404);
  }

  return {fetch: async (path, init) => handle(path, init)};
};
})();
