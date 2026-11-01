#!/usr/bin/env python3
import base64
import binascii
import json
import os
import random
import re
import secrets
import sys
import urllib.parse
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("SPINEWALL_PORT", "8777"))
MODELS_DIR = os.environ.get(
    "SPINEWALL_MODELS", os.path.expanduser("~/.local/share/spinewall/models")
)
STATE_FILE = os.environ.get(
    "SPINEWALL_STATE", os.path.expanduser("~/.local/state/spinewall.json")
)

THUMB_DIR = os.environ.get(
    "SPINEWALL_THUMBS", os.path.expanduser("~/.cache/spinewall/thumbs")
)
THUMB_MAX_BYTES = 512 * 1024
PREFS_FILE = os.environ.get(
    "SPINEWALL_PREFS", os.path.expanduser("~/.local/state/spinewall/prefs.json")
)
LAYOUT_FILE = os.environ.get(
    "SPINEWALL_LAYOUT", os.path.expanduser("~/.local/state/spinewall/layout.json")
)
RECENT_MAX = 20
UID_PATTERN = re.compile(r"^[A-Za-z0-9_-]{1,32}$")
ANIMATION_MAX_CHARS = 200
PREF_CHOICES = {
    "layout": ("grid", "list"),
    "sort": ("name", "recent", "source"),
    "kind": ("character", "scene", "all"),
}
PNG_MAGIC = b"\x89PNG\r\n\x1a\n"
VARIANT_SUFFIX = re.compile(r"^(.+?)[_-]\d+$")
VERSION_PATTERN = re.compile(r"^\d+\.\d+(\.\d+)?")

# Maps the Spine editor version of a model to the player runtime that can load it.
RUNTIME_BY_VERSION = {"4.0": "spine40", "4.1": "spine41", "4.2": "spine42", "3.8": "spine38"}

KINDS = ("character", "scene")
SCENE_NAME = re.compile(r"^(bg|background|scene|diorama)|bg_", re.IGNORECASE)
VIEW_DEFAULT = {"scale": 1.0, "x": 0.0, "y": 0.0}
INPUT_KEYS = ("wheel", "pointerdown", "pointermove", "pointerup", "fired", "keydown")


def read_binary_version(path):
    # 4.x: an 8-byte hash, then the version string. 3.x: the hash is a string too.
    # A string is a varint length plus 1, then utf8. Both layouts are tried.
    with open(path, "rb") as f:
        head = f.read(64)
    starts = [8]
    if head and 0 < head[0] < 0x80:
        starts.append(head[0])
    for start in starts:
        length = head[start] - 1 if start < len(head) else 0
        if not 0 < length < 32:
            continue
        text = head[start + 1 : start + 1 + length].decode("ascii", "replace")
        if VERSION_PATTERN.match(text):
            return text
    return None


def read_json_version(path):
    with open(path) as f:
        skeleton = json.load(f).get("skeleton", {})
    # Spine's own JSON export writes "spine". Some converters write "version".
    return skeleton.get("spine") or skeleton.get("version")


def read_atlas_regions(path):
    # Region names are the unindented lines without a colon. Page lines end in an image extension.
    names = []
    with open(path, errors="replace") as f:
        for line in f:
            text = line.rstrip("\r\n")
            if not text or text[0] in " \t" or ":" in text:
                continue
            if text.lower().endswith((".png", ".jpg", ".jpeg", ".webp")):
                continue
            names.append(text)
    return names


def read_json_slots(path):
    with open(path) as f:
        return [s.get("name", "") for s in json.load(f).get("slots", [])]


def classify_kind(names):
    # Bare "back" is left out on purpose: F_Coat_Back and similar are clothing parts.
    if not names:
        return "character"
    hits = sum(1 for n in names if SCENE_NAME.search(n))
    return "scene" if hits * 2 > len(names) else "character"


def scan_model(directory):
    """Returns (model, None) or (None, reason)."""
    files = sorted(os.listdir(directory))
    overrides = {}
    if "model.json" in files:
        with open(os.path.join(directory, "model.json")) as f:
            overrides = json.load(f)
    skeleton = overrides.get("skeleton") or next(
        (n for n in files if n.endswith(".skel")),
        next((n for n in files if n.endswith(".json") and n != "model.json"), None),
    )
    atlas = overrides.get("atlas") or next((n for n in files if n.endswith(".atlas")), None)
    if not skeleton or not atlas:
        return None, "needs one skeleton (.skel or .json) and one .atlas file"
    path = os.path.join(directory, skeleton)
    version = read_json_version(path) if skeleton.endswith(".json") else read_binary_version(path)
    runtime = RUNTIME_BY_VERSION.get(".".join((version or "").split(".")[:2]))
    if not runtime:
        return None, f"unsupported Spine version {version!r}, supported: 3.8.x, 4.0.x, 4.1.x, 4.2.x"
    model_id = os.path.basename(directory)
    kind = overrides.get("kind")
    if kind not in KINDS:
        try:
            names = read_json_slots(path) if skeleton.endswith(".json") else []
            names = names or read_atlas_regions(os.path.join(directory, atlas))
        except (OSError, ValueError):
            names = []
        kind = classify_kind(names)
    hide_slots = overrides.get("hideSlots")
    if not isinstance(hide_slots, list):
        hide_slots = []
    return {
        "id": model_id,
        "name": overrides.get("name", model_id),
        "group": overrides.get("group"),
        "character": overrides.get("character"),
        "skeleton": skeleton,
        "atlas": atlas,
        "version": version,
        "runtime": runtime,
        "skin": overrides.get("skin", "default"),
        "kind": kind,
        "hideSlots": [s for s in hide_slots if isinstance(s, str) and s],
        "defaultAnimation": overrides.get("defaultAnimation", "idle"),
    }, None


def scan_models():
    models, skipped = [], {}
    if not os.path.isdir(MODELS_DIR):
        return models, skipped
    for name in sorted(os.listdir(MODELS_DIR)):
        directory = os.path.join(MODELS_DIR, name)
        if not os.path.isdir(directory):
            continue
        try:
            model, reason = scan_model(directory)
        except (OSError, ValueError) as e:
            model, reason = None, str(e)
        if model:
            models.append(model)
        else:
            skipped[name] = reason
            print(f"skipped model {name}: {reason}", file=sys.stderr)
    assign_characters(models)
    return models, skipped


def assign_characters(models):
    # A stem counts only when it names a shared character, so a lone "x_01" is not renamed.
    ids = {m["id"] for m in models}
    stems = {}
    for m in models:
        match = VARIANT_SUFFIX.match(m["id"])
        if match:
            stems.setdefault(match.group(1), []).append(m)
    for m in models:
        if m["character"]:
            continue
        match = VARIANT_SUFFIX.match(m["id"])
        stem = match.group(1) if match else None
        shared = stem and (len(stems[stem]) >= 2 or stem in ids)
        m["character"] = stem if shared else m["id"]


def first_character_id(models):
    # A scene as the fallback would show a background on a fresh install.
    for m in models:
        if m["kind"] == "character":
            return m["id"]
    return models[0]["id"] if models else None


MODELS, SKIPPED = scan_models()
IDS = [m["id"] for m in MODELS]
ANIMS = {}
EDIT = {"on": 0}
INPUT = dict.fromkeys(INPUT_KEYS, 0)


def load_state():
    state = {"model": first_character_id(MODELS), "animation": None, "rev": 0}
    try:
        with open(STATE_FILE) as f:
            saved = json.load(f)
        if saved.get("model") in IDS:
            state.update(saved)
    except (OSError, ValueError):
        pass
    for key, value in VIEW_DEFAULT.items():
        state.setdefault(key, value)
    state.setdefault("vrev", 0)
    return state


STATE = load_state()


def clamp(value, low, high, fallback):
    try:
        number = float(value)
    except (TypeError, ValueError):
        return fallback
    # NaN compares unequal to itself. json.dump would write it and the page could not parse it back.
    if number != number:
        return fallback
    return max(low, min(high, number))


def new_uid(taken):
    while True:
        uid = secrets.token_hex(3)
        if uid not in taken:
            return uid


def clean_characters(raw, allowed_uids=None):
    """Return the valid characters of `raw`, in z order (last draws on top).

    An entry with a uid outside `allowed_uids` is dropped, so a stale page cannot bring back a removed character.
    An entry without a uid is new and gets one.
    """
    if not isinstance(raw, list):
        return []
    cleaned, seen = [], set()
    for entry in raw:
        if not isinstance(entry, dict) or entry.get("model") not in IDS:
            continue
        uid = entry.get("uid")
        if uid is None:
            uid = new_uid(seen | set(allowed_uids or ()))
        elif not (isinstance(uid, str) and UID_PATTERN.match(uid)) or uid in seen:
            continue
        elif allowed_uids is not None and uid not in allowed_uids:
            continue
        seen.add(uid)
        animation = entry.get("animation")
        if not isinstance(animation, str) or not animation or len(animation) > ANIMATION_MAX_CHARS:
            animation = None
        cleaned.append(
            {
                "uid": uid,
                "model": entry["model"],
                "animation": animation,
                "x": clamp(entry.get("x"), -2.0, 2.0, 0.0),
                "y": clamp(entry.get("y"), -2.0, 2.0, 0.0),
                "scale": clamp(entry.get("scale"), 0.3, 8.0, 1.0),
            }
        )
    return cleaned


def load_layout():
    try:
        with open(LAYOUT_FILE) as f:
            saved = json.load(f)
    except (OSError, ValueError):
        return [], 0
    crev = saved.get("crev")
    return clean_characters(saved.get("characters")), crev if isinstance(crev, int) else 0


# An empty list is single-character mode. Only a non-empty list switches the page to the shared stage.
STATE["characters"], STATE["crev"] = load_layout()


def load_prefs():
    prefs = {"favorites": [], "recent": [], "layout": "grid", "sort": "name", "kind": "character", "variants": True}
    try:
        with open(PREFS_FILE) as f:
            saved = json.load(f)
    except (OSError, ValueError):
        return prefs
    for key in ("favorites", "recent"):
        if isinstance(saved.get(key), list):
            prefs[key] = [i for i in saved[key] if isinstance(i, str)]
    for key, choices in PREF_CHOICES.items():
        if saved.get(key) in choices:
            prefs[key] = saved[key]
    if isinstance(saved.get("variants"), bool):
        prefs["variants"] = saved["variants"]
    return prefs


# Kept apart from STATE so a favorite never bumps a revision and never reloads the page.
PREFS = load_prefs()


def write_json_atomic(path, obj):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    # A rename is atomic, so a crash mid-write cannot leave a half-written file.
    tmp = path + ".tmp"
    with open(tmp, "w") as f:
        json.dump(obj, f)
    os.replace(tmp, path)


def save_prefs():
    write_json_atomic(PREFS_FILE, PREFS)


def toggle_favorite(model_id):
    if model_id not in IDS:
        return False
    favorites = PREFS["favorites"]
    if model_id in favorites:
        favorites.remove(model_id)
    else:
        favorites.append(model_id)
    save_prefs()
    return True


def record_recent(model_id):
    recent = [i for i in PREFS["recent"] if i != model_id]
    PREFS["recent"] = [model_id] + recent[: RECENT_MAX - 1]
    save_prefs()


def set_prefs(body):
    changed = False
    for key, choices in PREF_CHOICES.items():
        if body.get(key) in choices:
            PREFS[key] = body[key]
            changed = True
    if isinstance(body.get("variants"), bool):
        PREFS["variants"] = body["variants"]
        changed = True
    if changed:
        save_prefs()
    return changed


LAYOUT_KEYS = ("characters", "crev")


def save():
    os.makedirs(os.path.dirname(STATE_FILE), exist_ok=True)
    with open(STATE_FILE, "w") as f:
        json.dump({k: v for k, v in STATE.items() if k not in LAYOUT_KEYS}, f)


def save_layout():
    write_json_atomic(LAYOUT_FILE, {"characters": STATE["characters"], "crev": STATE["crev"]})


def set_characters(characters):
    STATE["characters"] = characters
    STATE["crev"] += 1
    save_layout()


def default_animation(model_id):
    names = ANIMS.get(model_id) or []
    model = next((m for m in MODELS if m["id"] == model_id), {})
    return next((n for n in (model.get("defaultAnimation"), "idle") if n in names), names[0] if names else None)


def find_character(uid):
    return next((c for c in STATE["characters"] if c["uid"] == uid), None)


def add_character(model_id):
    chars = STATE["characters"]
    if not chars:
        # The visible character keeps its place when the stage takes over, so adding one never makes it jump.
        chars = [
            {
                "uid": new_uid(set()),
                "model": STATE["model"],
                "animation": STATE["animation"],
                "x": STATE["x"],
                "y": STATE["y"],
                "scale": STATE["scale"],
            }
        ]
    n = len(chars)
    # New characters alternate right and left of centre in growing steps, so none lands on another.
    side = 1 if n % 2 else -1
    step = ((n + 1) // 2 - 1) % 4 + 1
    chars = chars + [
        {
            "uid": new_uid({c["uid"] for c in chars}),
            "model": model_id if model_id in IDS else STATE["model"],
            "animation": None,
            "x": side * step * 0.2,
            "y": 0.0,
            "scale": 0.7,
        }
    ]
    set_characters(chars)


def remove_character(uid):
    chars = [c for c in STATE["characters"] if c["uid"] != uid]
    if len(chars) == len(STATE["characters"]):
        return
    if len(chars) == 1:
        # One character left is the single-character layout, so the old keys carry it and the player returns.
        last = chars[0]
        chars = []
        if last["model"] != STATE["model"] or last["animation"]:
            STATE["model"] = last["model"]
            STATE["animation"] = last["animation"]
            STATE["rev"] += 1
        STATE.update(scale=last["scale"], x=last["x"], y=last["y"])
        STATE["vrev"] += 1
        save()
    set_characters(chars)


def update_character(uid, **changes):
    chars = [{**c, **changes} if c["uid"] == uid else c for c in STATE["characters"]]
    if chars != STATE["characters"]:
        set_characters(chars)


def cycle_character_animation(uid, step):
    char = find_character(uid)
    names = ANIMS.get(char["model"]) if char else None
    if not names:
        return
    current = char["animation"] or default_animation(char["model"])
    i = names.index(current) if current in names else -1
    update_character(uid, animation=names[(i + step) % len(names)])


def set_layout(body):
    """Merge a page push of positions and order, or replace the whole layout when `replace` is true.

    A merge keeps characters the push does not mention. A page holding an older list therefore never
    removes a character that a command added a moment earlier. Removal goes through `remove-character`.
    """
    existing = {c["uid"]: c for c in STATE["characters"]}
    replace = body.get("replace") is True
    pushed = [e for e in body["characters"] if isinstance(e, dict)]
    if not replace:
        pushed = [e for e in pushed if e.get("uid") in existing]
    # Pushed entries carry position, scale and order. The stored model and animation fill the rest, so a
    # stale push cannot overwrite what a command changed.
    merged = [
        {**existing.get(e.get("uid") if isinstance(e.get("uid"), str) else None, {}), **e}
        for e in pushed
    ]
    cleaned = clean_characters(merged, None if replace else set(existing))
    if not replace:
        mentioned = {c["uid"] for c in cleaned}
        cleaned += [c for uid, c in existing.items() if uid not in mentioned]
    if cleaned != STATE["characters"]:
        set_characters(cleaned)


def set_view(scale=None, x=None, y=None):
    if scale is not None:
        STATE["scale"] = max(0.3, min(8.0, float(scale)))
    if x is not None:
        STATE["x"] = max(-2.0, min(2.0, float(x)))
    if y is not None:
        STATE["y"] = max(-2.0, min(2.0, float(y)))
    STATE["vrev"] += 1
    save()


def change_animation(name):
    STATE["animation"] = name
    STATE["rev"] += 1
    save()


def change_model(model_id):
    if model_id == STATE["model"]:
        return
    STATE["model"] = model_id
    STATE["animation"] = None
    STATE["rev"] += 1
    save()
    record_recent(model_id)


def rescan():
    global MODELS, SKIPPED, IDS
    MODELS, SKIPPED = scan_models()
    IDS = [m["id"] for m in MODELS]
    # A removed model must not stay selected, or the page loads a missing directory.
    fallback = first_character_id(MODELS)
    if STATE["model"] not in IDS and STATE["model"] != fallback:
        STATE["model"] = fallback
        STATE["animation"] = None
        STATE["rev"] += 1
        save()
    kept = [c for c in STATE["characters"] if c["model"] in IDS]
    if len(kept) != len(STATE["characters"]):
        set_characters(kept)


def thumb_path(model_id):
    # Only ids from the scan reach the filesystem, so a request cannot escape THUMB_DIR.
    return os.path.join(THUMB_DIR, model_id + ".png") if model_id in IDS else None


def command(cmd, arg=None):
    if not IDS:
        return
    current = STATE["model"]
    if cmd == "zoom-in":
        set_view(scale=STATE["scale"] * 1.1)
    elif cmd == "zoom-out":
        set_view(scale=STATE["scale"] / 1.1)
    elif cmd in ("move-left", "move-right", "move-up", "move-down"):
        dx = {"move-left": -0.05, "move-right": 0.05}.get(cmd, 0)
        dy = {"move-up": -0.05, "move-down": 0.05}.get(cmd, 0)
        set_view(x=STATE["x"] + dx, y=STATE["y"] + dy)
    elif cmd == "reset-view":
        set_view(**VIEW_DEFAULT)
    elif cmd == "next-model":
        change_model(IDS[(IDS.index(current) + 1) % len(IDS)])
    elif cmd == "prev-model":
        change_model(IDS[(IDS.index(current) - 1) % len(IDS)])
    elif cmd == "random-model":
        change_model(random.choice(IDS))
    elif cmd == "set-model" and arg in IDS:
        change_model(arg)
    elif cmd in ("next-anim", "prev-anim", "random-anim"):
        names = ANIMS.get(current)
        if not names:
            return
        i = names.index(STATE["animation"]) if STATE["animation"] in names else -1
        if cmd == "next-anim":
            change_animation(names[(i + 1) % len(names)])
        elif cmd == "prev-anim":
            change_animation(names[(i - 1) % len(names)])
        else:
            change_animation(random.choice(names))
    elif cmd == "set-anim" and arg:
        change_animation(arg)
    elif cmd == "add-character":
        add_character(arg)
    elif cmd == "clear-characters" and STATE["characters"]:
        set_characters([])
    elif cmd == "remove-character" and arg:
        remove_character(arg)
    elif cmd in ("next-char-anim", "prev-char-anim") and arg:
        cycle_character_animation(arg, 1 if cmd == "next-char-anim" else -1)
    elif cmd in ("set-char-anim", "set-char-model") and arg:
        uid, _, value = arg.partition("/")
        if cmd == "set-char-anim" and value:
            update_character(uid, animation=value[:ANIMATION_MAX_CHARS])
        elif cmd == "set-char-model" and value in IDS:
            update_character(uid, model=value, animation=None)


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=MODELS_DIR, **kwargs)

    def log_message(self, *args):
        pass

    def end_headers(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Cache-Control", "no-cache")
        super().end_headers()

    def send_json(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def read_json_body(self):
        length = int(self.headers.get("Content-Length", 0))
        try:
            return json.loads(self.rfile.read(length))
        except ValueError:
            return {}

    def do_OPTIONS(self):
        self.send_response(204)
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "*")
        self.end_headers()

    def do_GET(self):
        if self.path == "/state":
            return self.send_json({**STATE, "edit": EDIT["on"]})
        if self.path == "/prefs":
            return self.send_json(PREFS)
        if self.path == "/models":
            return self.send_json(MODELS)
        if self.path.startswith("/thumb/") and self.path.endswith(".png"):
            path = thumb_path(urllib.parse.unquote(self.path[7:-4]))
            if not path or not os.path.isfile(path):
                return self.send_json({"error": "no thumbnail"}, 404)
            with open(path, "rb") as f:
                body = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "image/png")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if self.path == "/rescan":
            rescan()
            return self.send_json(MODELS)
        if self.path == "/info":
            return self.send_json(
                {
                    "state": STATE,
                    "animations": ANIMS.get(STATE["model"], []),
                    "models": len(MODELS),
                    "skipped": SKIPPED,
                    "modelsDir": MODELS_DIR,
                    "input": INPUT,
                }
            )
        if self.path.startswith("/edit/"):
            EDIT["on"] = 1 if self.path.endswith("/1") else 0
            return self.send_json(EDIT)
        if self.path.startswith("/cmd/"):
            name, _, arg = self.path[5:].partition("/")
            command(name, urllib.parse.unquote(arg) or None)
            return self.send_json(STATE)
        return super().do_GET()

    def do_POST(self):
        body = self.read_json_body()
        if self.path == "/report":
            if body.get("model") and isinstance(body.get("animations"), list):
                ANIMS[body["model"]] = body["animations"]
                if body["model"] == STATE["model"] and not STATE["animation"]:
                    STATE["animation"] = body.get("playing")
            return self.send_json({"ok": True})
        if self.path.startswith("/thumb/"):
            path = thumb_path(urllib.parse.unquote(self.path[7:]))
            try:
                png = base64.b64decode(body.get("png", ""), validate=True)
            except (binascii.Error, ValueError):
                png = b""
            if not path or not png.startswith(PNG_MAGIC) or len(png) > THUMB_MAX_BYTES:
                return self.send_json({"error": "bad thumbnail"}, 400)
            os.makedirs(THUMB_DIR, exist_ok=True)
            with open(path, "wb") as f:
                f.write(png)
            return self.send_json({"ok": True})
        if self.path.startswith("/fav/"):
            if not toggle_favorite(urllib.parse.unquote(self.path[5:])):
                return self.send_json({"error": "unknown model"}, 404)
            return self.send_json(PREFS)
        if self.path == "/prefs":
            if not set_prefs(body):
                return self.send_json({"error": "bad prefs"}, 400)
            return self.send_json(PREFS)
        if self.path == "/layout":
            if not isinstance(body.get("characters"), list):
                return self.send_json({"error": "bad layout"}, 400)
            set_layout(body)
            return self.send_json({"characters": STATE["characters"], "crev": STATE["crev"]})
        if self.path == "/view":
            set_view(body.get("scale"), body.get("x"), body.get("y"))
            return self.send_json({"ok": True})
        if self.path == "/input":
            for key in INPUT_KEYS:
                INPUT[key] += int(body.get(key, 0))
            return self.send_json({"ok": True})
        self.send_json({"error": "not found"}, 404)


if __name__ == "__main__":
    print(f"spinewall: {len(MODELS)} model(s) from {MODELS_DIR}, port {PORT}", file=sys.stderr)
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
