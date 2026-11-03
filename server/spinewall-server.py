#!/usr/bin/env python3
import base64
import binascii
import contextlib
import copy
import json
import os
import random
import re
import secrets
import sys
import tempfile
import threading
import urllib.parse
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

try:
    PORT = int(os.environ.get("SPINEWALL_PORT", "8777"))
except ValueError:
    sys.exit("spinewall: SPINEWALL_PORT must be an integer")
MODELS_DIR = os.environ.get(
    "SPINEWALL_MODELS", os.path.expanduser("~/.local/share/spinewall/models")
)
STATE_FILE = os.environ.get(
    "SPINEWALL_STATE", os.path.expanduser("~/.local/state/spinewall/state.json")
)
# Older installs kept the state beside the directory. It is read once when the new file is absent.
CONFIG_FILE = os.environ.get(
    "SPINEWALL_CONFIG", os.path.expanduser("~/.config/spinewall/config.json")
)
LEGACY_STATE_FILE = os.path.expanduser("~/.local/state/spinewall.json")

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
# One stage per monitor. A stage holds what that monitor shows, so each monitor can run its own preset.
SCREENS_FILE = os.environ.get(
    "SPINEWALL_SCREENS", os.path.expanduser("~/.local/state/spinewall/screens.json")
)
SCREEN_MAX = 16
SCREEN_KEY_MAX_CHARS = 60
RECENT_MAX = 20
PRESET_MAX = 50
PRESET_NAME_MAX_CHARS = 40
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
RUNTIME_BY_VERSION = {"4.0": "spine40", "4.1": "spine41", "4.2": "spine42", "3.8": "spine38", "3.7": "spine37"}

KINDS = ("character", "scene")
SCENE_NAME = re.compile(r"^(bg|background|scene|diorama)|bg_", re.IGNORECASE)
VIEW_DEFAULT = {"scale": 1.0, "x": 0.0, "y": 0.0}
INPUT_KEYS = ("wheel", "pointerdown", "pointermove", "pointerup", "fired", "keydown")
BODY_MAX_BYTES = 1024 * 1024
INPUT_STEP_MAX = 1000
INPUT_TOTAL_MAX = 10**12
REPORT_ANIMATIONS_MAX = 500
# The page fetches only these from a model directory. model.json is read by the server, never by the page.
SERVED_EXTENSIONS = (".skel", ".json", ".atlas", ".png", ".jpg", ".jpeg", ".webp", ".txt")
HIDDEN_FILES = ("model.json",)

# One lock covers all state, because ThreadingHTTPServer runs each request on its own thread.
LOCK = threading.RLock()


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
        data = json.load(f)
    skeleton = data.get("skeleton") if isinstance(data, dict) else None
    if not isinstance(skeleton, dict):
        return None
    # Spine's own JSON export writes "spine". Some converters write "version".
    version = skeleton.get("spine") or skeleton.get("version")
    return None if version is None else str(version)


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
        data = json.load(f)
    slots = data.get("slots") if isinstance(data, dict) else None
    if not isinstance(slots, list):
        return []
    return [s["name"] for s in slots if isinstance(s, dict) and isinstance(s.get("name"), str)]


def classify_kind(names):
    # Bare "back" is left out on purpose: F_Coat_Back and similar are clothing parts.
    if not names:
        return "character"
    hits = sum(1 for n in names if SCENE_NAME.search(n))
    return "scene" if hits * 2 > len(names) else "character"


def text_or(value, default):
    return value if isinstance(value, str) and value else default


def is_plain_filename(value):
    # An override must name a file in the model directory, so it cannot point elsewhere.
    return isinstance(value, str) and value not in ("", ".", "..") and "/" not in value and "\\" not in value


def scan_model(directory):
    """Returns (model, None) or (None, reason)."""
    files = sorted(os.listdir(directory))
    overrides = {}
    if "model.json" in files:
        with open(os.path.join(directory, "model.json")) as f:
            overrides = json.load(f)
        if not isinstance(overrides, dict):
            return None, "model.json must be a JSON object"
    for key in ("skeleton", "atlas"):
        if overrides.get(key) is not None and not is_plain_filename(overrides[key]):
            return None, f"model.json {key} must be a file name in the model directory"
    skeleton = overrides.get("skeleton") or next(
        (n for n in files if n.endswith(".skel")),
        next((n for n in files if n.endswith(".json") and n != "model.json"), None),
    )
    atlas = overrides.get("atlas") or next((n for n in files if n.endswith(".atlas")), None)
    if not skeleton or not atlas:
        return None, "needs one skeleton (.skel or .json) and one .atlas file"
    if not os.path.isfile(os.path.join(directory, skeleton)) or not os.path.isfile(os.path.join(directory, atlas)):
        return None, "the skeleton or atlas named in model.json does not exist"
    path = os.path.join(directory, skeleton)
    version = read_json_version(path) if skeleton.endswith(".json") else read_binary_version(path)
    runtime = RUNTIME_BY_VERSION.get(".".join((version or "").split(".")[:2]))
    if not runtime:
        return None, f"unsupported Spine version {version!r}, supported: 3.7.x, 3.8.x, 4.0.x, 4.1.x, 4.2.x"
    # The 3.7 web player has no binary reader, so only a JSON export can play.
    if runtime == "spine37" and skeleton.endswith(".skel"):
        return None, "Spine 3.7 needs a JSON export, because the 3.7 player cannot read .skel files"
    model_id = os.path.basename(directory)
    kind = overrides.get("kind")
    if kind not in KINDS:
        try:
            names = read_json_slots(path) if skeleton.endswith(".json") else []
            names = names or read_atlas_regions(os.path.join(directory, atlas))
        except (OSError, ValueError, RecursionError):
            names = []
        kind = classify_kind(names)
    hide_slots = overrides.get("hideSlots")
    if not isinstance(hide_slots, list):
        hide_slots = []
    return {
        "id": model_id,
        "name": text_or(overrides.get("name"), model_id),
        "group": text_or(overrides.get("group"), None),
        "character": text_or(overrides.get("character"), None),
        "skeleton": skeleton,
        "atlas": atlas,
        "version": version,
        "runtime": runtime,
        "skin": text_or(overrides.get("skin"), "default"),
        "kind": kind,
        "hideSlots": [s for s in hide_slots if isinstance(s, str) and s],
        "defaultAnimation": text_or(overrides.get("defaultAnimation"), "idle"),
    }, None


def load_extra_dirs():
    """Reads additional_model_dirs from the config file. Only the user's own file can add a directory."""
    try:
        with open(CONFIG_FILE) as f:
            data = json.load(f)
    except OSError:
        return []
    except (ValueError, RecursionError):
        print(f"spinewall: {CONFIG_FILE} is not valid JSON, so additional_model_dirs is ignored", file=sys.stderr)
        return []
    raw = data.get("additional_model_dirs") if isinstance(data, dict) else None
    if raw is None:
        return []
    if not isinstance(raw, list):
        print("spinewall: additional_model_dirs must be a list of paths, so it is ignored", file=sys.stderr)
        return []
    dirs = []
    for entry in raw:
        path = os.path.expanduser(entry) if isinstance(entry, str) else ""
        if not os.path.isabs(path) or not os.path.isdir(path):
            print(f"spinewall: additional_model_dirs entry {entry!r} is not an existing absolute directory, skipped", file=sys.stderr)
        elif os.path.realpath(path) != os.path.realpath(MODELS_DIR) and path not in dirs:
            dirs.append(path)
    return dirs


# Maps each scanned model id to its directory, because models can live outside MODELS_DIR.
MODEL_DIRS = {}


def scan_models():
    models, skipped = [], {}
    MODEL_DIRS.clear()
    # The first root wins on a duplicate id, so the main directory always keeps its own models.
    for root in [MODELS_DIR, *load_extra_dirs()]:
        if not os.path.isdir(root):
            continue
        for name in sorted(os.listdir(root)):
            directory = os.path.join(root, name)
            if not os.path.isdir(directory):
                continue
            label = name if root == MODELS_DIR else directory
            if name in MODEL_DIRS:
                skipped[label] = f"duplicate model ID, {MODEL_DIRS[name]} already has it"
                print(f"skipped model {label}: {skipped[label]}", file=sys.stderr)
                continue
            try:
                model, reason = scan_model(directory)
            except Exception as e:  # noqa: BLE001 - one bad model must never stop the server
                model, reason = None, f"{type(e).__name__}: {e}"
            if model:
                models.append(model)
                MODEL_DIRS[name] = directory
            else:
                skipped[label] = reason
                print(f"skipped model {label}: {reason}", file=sys.stderr)
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


def read_json_dict(path):
    """Returns the object stored at `path`, or None when the file is absent, unreadable, or not an object."""
    try:
        with open(path) as f:
            data = json.load(f)
    except (OSError, ValueError, RecursionError):
        return None
    return data if isinstance(data, dict) else None


def clamp(value, low, high, fallback):
    if isinstance(value, str) and not value.strip():
        return fallback
    try:
        number = float(value)
    except (TypeError, ValueError, OverflowError):
        return fallback
    # NaN compares unequal to itself. json.dump would write it and the page could not parse it back.
    if number != number:
        return fallback
    return max(low, min(high, number))


def plain_int(value, fallback=0):
    # bool is an int subclass, so it needs its own check.
    return value if isinstance(value, int) and not isinstance(value, bool) else fallback




def new_uid(taken):
    while True:
        uid = secrets.token_hex(3)
        if uid not in taken:
            return uid


def clean_characters(raw, allowed_uids=None, known_only=True):
    """Return the valid characters of `raw`, in z order (last draws on top).

    An entry with a uid outside `allowed_uids` is dropped, so a stale page cannot bring back a removed character.
    An entry without a uid is new and gets one.
    A saved preset keeps a model that is missing now, so `known_only=False` skips the scan check for it.
    """
    if not isinstance(raw, list):
        return []
    cleaned, seen = [], set()
    for entry in raw:
        if not isinstance(entry, dict):
            continue
        model = entry.get("model")
        if not isinstance(model, str) or not model or (known_only and model not in IDS):
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
                # `is True` keeps 1 and "yes" out, because a bool is the only value the page writes.
                "auto": entry.get("auto") is True,
            }
        )
    return cleaned


def clean_screen_key(value):
    """Return a screen key, or None. The empty key is the default stage, which a lone monitor uses."""
    if not isinstance(value, str) or len(value) > SCREEN_KEY_MAX_CHARS or not value.isprintable():
        return None
    return value


def clean_stage(saved):
    """Return a full stage from stored JSON. A key that is missing or wrong falls back to its default."""
    stage = {
        "model": first_character_id(MODELS), "animation": None, "rev": 0, **VIEW_DEFAULT, "vrev": 0,
        "auto": False, "preset": None, "characters": [], "crev": 0,
    }
    if not isinstance(saved, dict):
        return stage
    # An empty list is single-character mode. Only a non-empty list switches the page to the shared stage.
    stage["characters"] = clean_characters(saved.get("characters"))
    stage["crev"] = max(0, plain_int(saved.get("crev")))
    stage["preset"] = clean_preset_name(saved.get("preset"))
    if saved.get("model") not in IDS:
        return stage
    stage["model"] = saved["model"]
    animation = saved.get("animation")
    stage["animation"] = animation if isinstance(animation, str) and 0 < len(animation) <= ANIMATION_MAX_CHARS else None
    stage["rev"] = max(0, plain_int(saved.get("rev")))
    stage["auto"] = saved.get("auto") is True
    stage["vrev"] = max(0, plain_int(saved.get("vrev")))
    stage["scale"] = clamp(saved.get("scale"), 0.3, 8.0, VIEW_DEFAULT["scale"])
    stage["x"] = clamp(saved.get("x"), -2.0, 2.0, VIEW_DEFAULT["x"])
    stage["y"] = clamp(saved.get("y"), -2.0, 2.0, VIEW_DEFAULT["y"])
    return stage


def load_default_stage():
    saved = read_json_dict(STATE_FILE)
    if saved is None and not os.path.exists(STATE_FILE):
        saved = read_json_dict(LEGACY_STATE_FILE)
    # The layout lives in its own file, apart from the state file.
    return clean_stage({**(saved or {}), **(read_json_dict(LAYOUT_FILE) or {})})


def load_screens():
    saved = read_json_dict(SCREENS_FILE) or {}
    keys = [k for k in saved if clean_screen_key(k)]
    return {k: clean_stage(saved[k]) for k in keys[:SCREEN_MAX]}




def clean_preset_name(value):
    if not isinstance(value, str):
        return None
    name = value.strip()
    if not name or len(name) > PRESET_NAME_MAX_CHARS or not name.isprintable():
        return None
    return name


# The default stage serves a lone monitor and seeds each new screen. The functions below read the stage in `STATE`.
DEFAULT_STAGE = load_default_stage()
STATE = DEFAULT_STAGE
SCREENS = load_screens()


def clean_preset(raw):
    """Return a stored preset, or None. A preset is a name and its characters in z order, with no uids.

    One character is the single-character layout. More than one is multi mode.
    """
    if not isinstance(raw, dict):
        return None
    name = clean_preset_name(raw.get("name"))
    # Uids are dropped because a load creates fresh characters, so two loads never share an id.
    characters = [
        {k: v for k, v in c.items() if k != "uid"}
        for c in clean_characters(raw.get("characters"), known_only=False)
    ]
    if name is None or not characters:
        return None
    return {"name": name, "characters": characters}


def clean_presets(raw):
    if not isinstance(raw, list):
        return []
    presets, names = [], set()
    for entry in raw:
        preset = clean_preset(entry)
        if preset and preset["name"] not in names and len(presets) < PRESET_MAX:
            names.add(preset["name"])
            presets.append(preset)
    return presets


def load_prefs():
    prefs = {"favorites": [], "recent": [], "layout": "grid", "sort": "name", "kind": "character", "variants": True, "presets": []}
    saved = read_json_dict(PREFS_FILE)
    if saved is None:
        return prefs
    for key in ("favorites", "recent"):
        if isinstance(saved.get(key), list):
            prefs[key] = [i for i in saved[key] if isinstance(i, str)]
    for key, choices in PREF_CHOICES.items():
        if saved.get(key) in choices:
            prefs[key] = saved[key]
    if isinstance(saved.get("variants"), bool):
        prefs["variants"] = saved["variants"]
    prefs["presets"] = clean_presets(saved.get("presets"))
    return prefs


# Kept apart from STATE so a favorite never bumps a revision and never reloads the page.
PREFS = load_prefs()


def write_atomic(path, data):
    directory = os.path.dirname(path)
    os.makedirs(directory, exist_ok=True)
    # A rename is atomic, so a crash mid-write cannot leave a half-written file.
    # A unique temp name keeps two writers from sharing one.
    fd, tmp = tempfile.mkstemp(dir=directory, prefix=os.path.basename(path) + ".", suffix=".tmp")
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(data)
        os.replace(tmp, path)
    except BaseException:
        if os.path.exists(tmp):
            os.unlink(tmp)
        raise


def write_json_atomic(path, obj):
    write_atomic(path, json.dumps(obj).encode())


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
    if STATE is DEFAULT_STAGE:
        write_json_atomic(STATE_FILE, {k: v for k, v in STATE.items() if k not in LAYOUT_KEYS})
    else:
        write_json_atomic(SCREENS_FILE, SCREENS)


def save_layout():
    if STATE is DEFAULT_STAGE:
        write_json_atomic(LAYOUT_FILE, {"characters": STATE["characters"], "crev": STATE["crev"]})
    else:
        write_json_atomic(SCREENS_FILE, SCREENS)


@contextlib.contextmanager
def on_stage(stage):
    """Run the state functions on one stage. Every caller holds LOCK, so the swap is never seen by another request."""
    global STATE
    previous, STATE = STATE, stage
    try:
        yield
    finally:
        STATE = previous


def all_stages():
    return [DEFAULT_STAGE, *SCREENS.values()]


def screen_stage(key, create=False):
    """The stage of a screen, or None. A new screen starts as a copy of the default stage, so it looks the same until edited."""
    if key == "":
        return DEFAULT_STAGE
    stage = SCREENS.get(key)
    if stage is None and create and len(SCREENS) < SCREEN_MAX:
        stage = SCREENS[key] = copy.deepcopy(DEFAULT_STAGE)
        with on_stage(stage):
            save()
    return stage


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
                "auto": STATE["auto"],
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
            "auto": False,
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
        if last["model"] != STATE["model"] or last["animation"] or last["auto"] != STATE["auto"]:
            STATE["model"] = last["model"]
            STATE["animation"] = last["animation"]
            STATE["auto"] = last["auto"]
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


def set_auto_cycle(arg):
    """`on` or `off` for the single character, or all characters in multi mode. `UID/on` for one character."""
    uid, slash, value = arg.rpartition("/")
    if value not in ("on", "off"):
        return
    on = value == "on"
    if slash:
        update_character(uid, auto=on)
    elif STATE["characters"]:
        chars = [{**c, "auto": on} for c in STATE["characters"]]
        if chars != STATE["characters"]:
            set_characters(chars)
    elif STATE["auto"] != on:
        STATE["auto"] = on
        STATE["rev"] += 1
        save()


def set_layout(body):
    """Merge a page push of positions and order, or replace the whole layout when `replace` is true.

    A merge keeps characters the push does not mention. A page holding an older list therefore never
    removes a character that a command added a moment earlier. Removal goes through `remove-character`.
    """
    existing = {c["uid"]: c for c in STATE["characters"]}
    replace = body.get("replace") is True
    pushed = [e for e in body["characters"] if isinstance(e, dict)]
    # A bad flag keeps the stored one. Only a real bool changes it.
    pushed = [e if isinstance(e.get("auto"), bool) else {k: v for k, v in e.items() if k != "auto"} for e in pushed]
    if not replace:
        pushed = [e for e in pushed if e.get("uid") in existing]
    # Pushed entries carry position, scale and order. The stored model and animation fill the rest, so a
    # stale push cannot overwrite what a command changed.
    if not replace:
        pushed = [{k: v for k, v in e.items() if k not in ("model", "animation")} for e in pushed]
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


def current_characters():
    """The characters on screen. The single-character layout counts as a list of one."""
    if STATE["characters"]:
        return [{k: v for k, v in c.items() if k != "uid"} for c in STATE["characters"]]
    return [{k: STATE[k] for k in ("model", "animation", "x", "y", "scale", "auto")}]


def save_preset(raw_name):
    """Store the current layout under a name. The same name replaces its preset in place."""
    name = clean_preset_name(raw_name)
    preset = clean_preset({"name": name, "characters": current_characters()})
    if not preset:
        return False
    presets = PREFS["presets"]
    index = next((i for i, p in enumerate(presets) if p["name"] == name), None)
    if index is None and len(presets) >= PRESET_MAX:
        return False
    if index is None:
        presets.append(preset)
    else:
        presets[index] = preset
    save_prefs()
    # The stage now matches this preset, so a cycle continues from it.
    STATE["preset"] = name
    save()
    return True


def delete_preset(name):
    presets = [p for p in PREFS["presets"] if p["name"] != name]
    if len(presets) == len(PREFS["presets"]):
        return False
    PREFS["presets"] = presets
    save_prefs()
    for stage in all_stages():
        if stage["preset"] == name:
            with on_stage(stage):
                STATE["preset"] = None
                save()
    return True


def load_preset(name):
    """Replace the layout with a preset. A model that is gone is skipped, and no model left changes nothing."""
    preset = next((p for p in PREFS["presets"] if p["name"] == name), None)
    chars = clean_characters(preset["characters"]) if preset else []
    if not chars:
        return False
    STATE["preset"] = name
    if len(chars) == 1:
        # One character is the single-character layout, so the old keys carry it, as in remove_character.
        only = chars[0]
        STATE.update(model=only["model"], animation=only["animation"], auto=only["auto"])
        STATE["rev"] += 1
        STATE.update(scale=only["scale"], x=only["x"], y=only["y"])
        STATE["vrev"] += 1
        save()
        record_recent(only["model"])
        chars = []
    set_characters(chars)
    # The multi path writes only the layout file, so the preset name needs its own save.
    save()
    return True


def step_preset(step):
    """Load the next (1) or previous (-1) preset after the one this stage loaded last, and wrap around.

    A preset whose models are all gone is skipped. A stage that never loaded one starts at the first or the last.
    """
    names = [p["name"] for p in PREFS["presets"]]
    if not names:
        return
    start = names.index(STATE["preset"]) if STATE["preset"] in names else (-1 if step > 0 else 0)
    for offset in range(1, len(names) + 1):
        if load_preset(names[(start + step * offset) % len(names)]):
            return


def set_view(scale=None, x=None, y=None):
    # An unusable value keeps the current one, so a bad push never writes NaN into the state.
    if scale is not None:
        STATE["scale"] = clamp(scale, 0.3, 8.0, STATE["scale"])
    if x is not None:
        STATE["x"] = clamp(x, -2.0, 2.0, STATE["x"])
    if y is not None:
        STATE["y"] = clamp(y, -2.0, 2.0, STATE["y"])
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
    for stage in all_stages():
        with on_stage(stage):
            drop_missing_models()


def drop_missing_models():
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


# These act on the shared preset list, so one run is enough. A broadcast would save the last stage's layout.
ONCE_COMMANDS = ("save-preset", "delete-preset")


def command(cmd, arg=None, stages=None):
    """Run a command on `stages`, or on every stage when none is named, so a lone CLI call still reaches each monitor."""
    with LOCK:
        targets = stages if stages is not None else all_stages()
        if cmd in ONCE_COMMANDS:
            targets = targets[:1]
        for stage in targets:
            with on_stage(stage):
                run_command(cmd, arg)


def run_command(cmd, arg=None):
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
        change_animation(arg[:ANIMATION_MAX_CHARS])
    elif cmd == "add-character":
        add_character(arg)
    elif cmd == "save-preset" and arg:
        save_preset(arg)
    elif cmd == "load-preset" and arg:
        load_preset(arg)
    elif cmd in ("next-preset", "prev-preset"):
        step_preset(1 if cmd == "next-preset" else -1)
    elif cmd == "delete-preset" and arg:
        delete_preset(arg)
    elif cmd == "clear-characters" and STATE["characters"]:
        set_characters([])
    elif cmd == "remove-character" and arg:
        remove_character(arg)
    elif cmd in ("next-char-anim", "prev-char-anim") and arg:
        cycle_character_animation(arg, 1 if cmd == "next-char-anim" else -1)
    elif cmd == "auto-cycle" and arg:
        set_auto_cycle(arg)
    elif cmd in ("set-char-anim", "set-char-model") and arg:
        uid, _, value = arg.partition("/")
        if cmd == "set-char-anim" and value:
            update_character(uid, animation=value[:ANIMATION_MAX_CHARS])
        elif cmd == "set-char-model" and value in IDS:
            update_character(uid, model=value, animation=None)


class Handler(SimpleHTTPRequestHandler):
    cors_origin = None

    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=MODELS_DIR, **kwargs)

    def log_message(self, *args):
        pass

    def translate_path(self, path):
        # A path that names no scanned model maps to a file that does not exist, so it answers 404.
        parts = [urllib.parse.unquote(p) for p in path.split("?", 1)[0].split("/") if p]
        directory = MODEL_DIRS.get(parts[0]) if len(parts) == 2 else None
        return os.path.join(directory, parts[1]) if directory else os.path.join(MODELS_DIR, ".not-a-model")

    def end_headers(self):
        if self.cors_origin:
            self.send_header("Access-Control-Allow-Origin", self.cors_origin)
            self.send_header("Vary", "Origin")
        self.send_header("Cache-Control", "no-cache")
        super().end_headers()

    def send_json(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def allowed_origins(self):
        # The theme page is a local file, so its origin is "null" or file://. A web page cannot send these.
        return ("null", "file://", f"http://127.0.0.1:{PORT}", f"http://localhost:{PORT}")

    def guard(self):
        """Refuse DNS rebinding and cross-site requests. Returns True when the request may go on."""
        # A rebound host name reaches this server with its own name in Host.
        if self.headers.get("Host") not in (f"127.0.0.1:{PORT}", f"localhost:{PORT}"):
            self.send_json({"error": "bad host"}, 403)
            return False
        origin = self.headers.get("Origin")
        if origin is not None:
            if origin not in self.allowed_origins():
                self.send_json({"error": "origin not allowed"}, 403)
                return False
            self.cors_origin = origin
        elif self.headers.get("Sec-Fetch-Site") == "cross-site":
            # A no-cors request, such as an <img> or <script> tag on another site, sends no Origin.
            self.send_json({"error": "cross-site request"}, 403)
            return False
        return True

    def read_json_body(self):
        """Returns the JSON object of the body, or None after sending the error reply."""
        try:
            length = int(self.headers.get("Content-Length", 0))
        except ValueError:
            length = -1
        if length < 0:
            self.send_json({"error": "bad Content-Length"}, 400)
            return None
        if length > BODY_MAX_BYTES:
            self.send_json({"error": "body too large"}, 413)
            return None
        try:
            body = json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, RecursionError):
            body = None
        if not isinstance(body, dict):
            self.send_json({"error": "body must be a JSON object"}, 400)
            return None
        return body

    def do_OPTIONS(self):
        if not self.guard():
            return
        self.send_response(204)
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()

    def do_HEAD(self):
        self.send_error(405)

    def list_directory(self, path):
        self.send_error(404)
        return None

    def is_served_file(self):
        parts = [urllib.parse.unquote(p) for p in self.path.split("?", 1)[0].split("/") if p]
        # A model file sits one directory deep. Hidden files and other extensions are never served.
        return (
            len(parts) == 2
            and not parts[1].startswith(".")
            and parts[1] not in HIDDEN_FILES
            and parts[1].lower().endswith(SERVED_EXTENSIONS)
        )

    def do_GET(self):
        if not self.guard() or not self.parse_route():
            return
        with LOCK:
            if self.handle_get():
                return
        if not self.is_served_file():
            return self.send_json({"error": "not found"}, 404)
        try:
            super().do_GET()
        except ValueError:
            # A NUL byte in the path makes open() raise. A 404 keeps the connection usable.
            self.send_json({"error": "not found"}, 404)

    def parse_route(self):
        """Split the path from its query and read the screen. Returns False after sending the error reply."""
        parts = urllib.parse.urlsplit(self.path)
        self.route = parts.path
        self.screen = None
        values = urllib.parse.parse_qs(parts.query, keep_blank_values=True).get("screen")
        if values:
            self.screen = clean_screen_key(values[0])
            if self.screen is None:
                self.send_json({"error": "bad screen"}, 400)
                return False
        return True

    def stage_param(self, create=False):
        """The stage the request names, or None after sending the error reply. No screen means the default stage.

        Only a page creates a stage. A CLI call with a mistyped name gets a 404 instead of a new screen.
        """
        stage = screen_stage(self.screen or "", create=create)
        if stage is None:
            if create:
                self.send_json({"error": f"at most {SCREEN_MAX} screens"}, 400)
            else:
                self.send_json({"error": "unknown screen"}, 404)
        return stage

    def handle_get(self):
        """Answers the API paths. Returns False for a path that is a model file."""
        path = self.route
        if path == "/state":
            stage = self.stage_param(create=True)
            if stage is None:
                return True
            self.send_json({**stage, "edit": EDIT["on"]})
        elif path == "/prefs":
            self.send_json(PREFS)
        elif path == "/presets":
            self.send_json([{"name": p["name"], "characters": len(p["characters"])} for p in PREFS["presets"]])
        elif path == "/screens":
            self.send_json(
                [{"screen": k, "preset": v["preset"], "characters": len(v["characters"])} for k, v in SCREENS.items()]
            )
        elif path == "/models":
            self.send_json(MODELS)
        elif path.startswith("/thumb/") and path.endswith(".png"):
            path = thumb_path(urllib.parse.unquote(path[7:-4]))
            if not path or not os.path.isfile(path):
                return self.send_json({"error": "no thumbnail"}, 404) or True
            with open(path, "rb") as f:
                body = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "image/png")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif path == "/rescan":
            rescan()
            self.send_json(MODELS)
        elif path == "/info":
            self.send_json(
                {
                    "state": DEFAULT_STAGE,
                    "screens": SCREENS,
                    "animations": ANIMS.get(STATE["model"], []),
                    "models": len(MODELS),
                    "skipped": SKIPPED,
                    "modelsDir": MODELS_DIR,
                    "additionalModelDirs": load_extra_dirs(),
                    "input": INPUT,
                }
            )
        elif path.startswith("/edit/"):
            EDIT["on"] = 1 if path.endswith("/1") else 0
            self.send_json(EDIT)
        elif path.startswith("/cmd/"):
            name, _, arg = path[5:].partition("/")
            # A call that names no screen reaches every stage. A named one reaches that stage alone.
            stage = self.stage_param() if self.screen is not None else DEFAULT_STAGE
            if stage is None:
                return True
            command(name, urllib.parse.unquote(arg) or None, None if self.screen is None else [stage])
            self.send_json(stage)
        else:
            return False
        return True

    def do_POST(self):
        if not self.guard() or not self.parse_route():
            return
        # A cross-site form can send text/plain without a preflight. JSON forces one.
        if self.headers.get("Content-Type", "").split(";")[0].strip().lower() != "application/json":
            return self.send_json({"error": "Content-Type must be application/json"}, 415)
        body = self.read_json_body()
        if body is None:
            return
        with LOCK:
            self.handle_post(body)

    def handle_post(self, body):
        path = self.route
        stage = self.stage_param(create=True)
        if stage is None:
            return
        with on_stage(stage):
            self.route_post(path, body)

    def route_post(self, path, body):
        if path == "/report":
            model, animations = body.get("model"), body.get("animations")
            valid = (
                isinstance(model, str)
                and model in IDS
                and isinstance(animations, list)
                and len(animations) <= REPORT_ANIMATIONS_MAX
                and all(isinstance(a, str) and 0 < len(a) <= ANIMATION_MAX_CHARS for a in animations)
            )
            if not valid:
                return self.send_json({"error": "bad report"}, 400)
            ANIMS[model] = animations
            playing = body.get("playing")
            if model == STATE["model"] and not STATE["animation"] and isinstance(playing, str) and playing:
                STATE["animation"] = playing[:ANIMATION_MAX_CHARS]
            return self.send_json({"ok": True})
        if path.startswith("/thumb/"):
            path = thumb_path(urllib.parse.unquote(path[7:]))
            encoded = body.get("png")
            try:
                png = base64.b64decode(encoded, validate=True) if isinstance(encoded, str) else b""
            except (binascii.Error, ValueError):
                png = b""
            if not path or not png.startswith(PNG_MAGIC) or len(png) > THUMB_MAX_BYTES:
                return self.send_json({"error": "bad thumbnail"}, 400)
            write_atomic(path, png)
            return self.send_json({"ok": True})
        if path.startswith("/fav/"):
            if not toggle_favorite(urllib.parse.unquote(path[5:])):
                return self.send_json({"error": "unknown model"}, 404)
            return self.send_json(PREFS)
        if path == "/prefs":
            if not set_prefs(body):
                return self.send_json({"error": "bad prefs"}, 400)
            return self.send_json(PREFS)
        if path == "/layout":
            if not isinstance(body.get("characters"), list):
                return self.send_json({"error": "bad layout"}, 400)
            set_layout(body)
            return self.send_json({"characters": STATE["characters"], "crev": STATE["crev"]})
        if path == "/view":
            set_view(body.get("scale"), body.get("x"), body.get("y"))
            return self.send_json({"ok": True})
        if path == "/input":
            for key in INPUT_KEYS:
                step = max(0, min(INPUT_STEP_MAX, plain_int(body.get(key))))
                INPUT[key] = min(INPUT_TOTAL_MAX, INPUT[key] + step)
            return self.send_json({"ok": True})
        self.send_json({"error": "not found"}, 404)


if __name__ == "__main__":
    print(f"spinewall: {len(MODELS)} model(s) from {MODELS_DIR}, port {PORT}", file=sys.stderr)
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
