# Model assets

Spinewall ships no models and no Spine runtime. You supply the models. You must have the right to use them.

## Layout

Put one directory per model in `~/.local/share/spinewall/models/`. The directory name is the model ID.

```
~/.local/share/spinewall/models/
  my-character/
    my-character.skel      skeleton, binary or JSON
    my-character.atlas     texture atlas
    my-character.png       atlas page, one file per page listed in the atlas
    model.json             optional
```

## Required files

| File | Rule |
|---|---|
| Skeleton | One `.skel` (binary) or `.json` file. A `.skel` wins over `.json`. |
| Atlas | One `.atlas` file. |
| Atlas pages | Every image the `.atlas` lists, in the same directory. A missing page makes the model fail to load. |

## Supported Spine versions

| Editor version | Runtime |
|---|---|
| 4.0.x | `@esotericsoftware/spine-player` 4.0.31 |
| 4.1.x | `@esotericsoftware/spine-player` 4.1.56 |

The server reads the version from the skeleton file. It skips a model with another version and prints the reason. `spinewall info` lists skipped models under `skipped`.

Spine 3.x and 4.2 are not supported yet.

## Optional `model.json`

All keys are optional.

```json
{
  "name": "My Character",
  "skeleton": "my-character.skel",
  "atlas": "my-character.atlas",
  "skin": "default",
  "defaultAnimation": "idle"
}
```

| Key | Default | Use |
|---|---|---|
| `name` | directory name | Display name for the model picker. |
| `skeleton` | first `.skel`, else first `.json` | Set this when a directory has several skeletons. |
| `atlas` | first `.atlas` | Set this when a directory has several atlases. |
| `skin` | `default` | Set this when the skeleton has no `default` skin. |
| `defaultAnimation` | `idle` | Played at load. If the model lacks it, the first animation plays. |

## Check a model

1. Copy the model directory into `~/.local/share/spinewall/models/`.
2. Restart the server: `systemctl --user restart spinewall.service`. The server scans the directory at start only.
3. Run `spinewall info`. The `models` count rises by one, or the model appears under `skipped` with a reason.
4. Run `spinewall set-model <id>`.

## Where to get models

- Your own Spine projects, exported with "Pack atlas" enabled.
- Spine example projects from Esoteric Software, for testing.
- Game assets you own. Check the game's terms first.

## Atlas page check

A model with no picture usually lacks an atlas page. List the pages the atlas names:

```bash
grep -E '\.(png|jpg|webp)$' ~/.local/share/spinewall/models/<id>/*.atlas
```

Each name must exist in the same directory.
