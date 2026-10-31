#!/usr/bin/env python3
import json
import os
import random
import sys
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("SPINEWALL_PORT", "8777"))
MODELS_DIR = os.environ.get(
    "SPINEWALL_MODELS", os.path.expanduser("~/.local/share/spinewall/models")
)
STATE_FILE = os.environ.get(
    "SPINEWALL_STATE", os.path.expanduser("~/.local/state/spinewall.json")
)

# Maps the Spine editor version of a model to the player runtime that can load it.
RUNTIME_BY_VERSION = {"4.0": "spine40", "4.1": "spine41"}

VIEW_DEFAULT = {"scale": 1.0, "x": 0.0, "y": 0.0}
INPUT_KEYS = ("wheel", "pointerdown", "pointermove", "pointerup", "fired")


def read_binary_version(path):
    # Spine 4.x binary header: 8-byte hash, then a string (varint length + 1, utf8).
    with open(path, "rb") as f:
        head = f.read(64)
    length = head[8] - 1
    if not 0 < length < 32:
        return None
    return head[9 : 9 + length].decode("ascii", "replace")


def read_json_version(path):
    with open(path) as f:
        return json.load(f).get("skeleton", {}).get("version")


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
        return None, f"unsupported Spine version {version!r}, supported: 4.0.x, 4.1.x"
    model_id = os.path.basename(directory)
    return {
        "id": model_id,
        "name": overrides.get("name", model_id),
        "skeleton": skeleton,
        "atlas": atlas,
        "version": version,
        "runtime": runtime,
        "skin": overrides.get("skin", "default"),
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
    return models, skipped


MODELS, SKIPPED = scan_models()
IDS = [m["id"] for m in MODELS]
ANIMS = {}
EDIT = {"on": 0}
INPUT = dict.fromkeys(INPUT_KEYS, 0)


def load_state():
    state = {"model": IDS[0] if IDS else None, "animation": None, "rev": 0}
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


def save():
    os.makedirs(os.path.dirname(STATE_FILE), exist_ok=True)
    with open(STATE_FILE, "w") as f:
        json.dump(STATE, f)


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
        if self.path == "/models":
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
            command(name, arg or None)
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
