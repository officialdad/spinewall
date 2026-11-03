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

## Models in other directories

The server can scan more directories. List them in `~/.config/spinewall/config.json` (env `SPINEWALL_CONFIG`):

```json
{
  "additional_model_dirs": ["/mnt/games/spine-models", "~/Downloads/models"]
}
```

| Rule | Detail |
|---|---|
| Entry | An absolute path, or one that starts with `~`. Anything else is skipped and logged. |
| Layout | The same as the main directory: one subdirectory per model. |
| Duplicate ID | The main directory wins. The later model is skipped and listed under `skipped`. |
| Reload | The server reads the config at start and on `spinewall rescan`. |
| Bad file | Invalid JSON or a non-list value is ignored and logged. The main directory still loads. |

`spinewall info` lists the directories under `additionalModelDirs`. Local mode takes its models from an index and has no directory scan.

## Required files

| File | Rule |
|---|---|
| Skeleton | One `.skel` (binary) or `.json` file. A `.skel` wins over `.json`. |
| Atlas | One `.atlas` file. |
| Atlas pages | Every image the `.atlas` lists, in the same directory. A missing page makes the model fail to load. |

## Supported Spine versions

| Editor version | Runtime |
|---|---|
| 3.7.x | `spine-player.js` from the `spine-runtimes` repo, commit `9639bcc`, fetched by the install script. JSON export only, because this player has no binary reader |
| 3.8.x | `spine-player.js` from the `spine-runtimes` repo, commit `8b4844b`, fetched by the install script |
| 4.0.x | `@esotericsoftware/spine-player` 4.0.31 |
| 4.1.x | `@esotericsoftware/spine-player` 4.1.56 |
| 4.2.x | `@esotericsoftware/spine-player` 4.2.120 |

The server reads the version from the skeleton file. It skips a model with another version and prints the reason. `spinewall info` lists skipped models under `skipped`.

Spine 3.6 and older, 4.3, and Live2D Cubism (`.moc3`) models are not supported. Many mobile game exports use 3.8.

## Optional `model.json`

All keys are optional.

```json
{
  "name": "My Character",
  "skeleton": "my-character.skel",
  "atlas": "my-character.atlas",
  "skin": "default",
  "defaultAnimation": "idle",
  "group": "My Game",
  "character": "my-character",
  "kind": "character",
  "hideSlots": ["bg"]
}
```

| Key | Default | Use |
|---|---|---|
| `name` | directory name | Display name for the model picker. |
| `skeleton` | first `.skel`, else first `.json` | Set this when a directory has several skeletons. |
| `atlas` | first `.atlas` | Set this when a directory has several atlases. |
| `skin` | `default` | Set this when the skeleton has no `default` skin. |
| `defaultAnimation` | `idle` | Played at load. If the model lacks it, the first animation plays. |
| `character` | model ID without a trailing `_NN` or `-NN` | Models with the same value collapse into one card in the picker. The server derives the value when a stem is shared by two models or equals another model's ID. |
| `kind` | detected | `character` or `scene`. A scene is a backdrop. The picker filter hides scenes by default, and the stage draws a scene behind every character and never sends it a click. |
| `hideSlots` | none | Slot name prefixes, case-insensitive. Matching slots never draw. Use it to remove a backdrop from a character. It also leaves the framing, the thumbnail, and the click area. |
| `group` | none | Source or game name. The model picker shows one filter button per group. A model with no group is listed under "Other". |

## Characters and scenes

Edit mode has a Characters / Scenes / All filter with counts. It starts on Characters, and the server keeps the choice in `prefs.json` under `kind`.

Without `kind`, the server reads the atlas region names (the JSON slot names for a JSON skeleton). The model is a scene when more than half of the names start with `bg`, `background`, `scene`, or `diorama`, or contain `bg_`. A bare `back` does not count, so `F_Coat_Back` stays a character. Set `kind` to override a wrong guess.

A model with a few `bg` regions next to a full character rig is a character with a backdrop. Add `hideSlots` to remove the backdrop. The default model on a fresh install is the first character, never a scene.

## Several characters at once

Edit mode shows a Characters strip above the model list.

- **Add character** adds the highlighted model next to the current one. Ctrl+Enter in the search box does the same.
- A press on a character in edit mode selects it. Drag and wheel then move and scale that character.
- A short click changes the animation of the topmost character under the pointer. A click on empty space does nothing.
- With a character selected, picking a model or animation changes that character.
- The ✕ button removes a character. One character left returns to the single layout.
- A warning shows above 25 characters. There is no hard limit.

Characters of one Spine version draw in the order of the list. Characters of different versions share order by layer only, so two overlapping characters of different versions can appear in the wrong order.

## Presets

A preset saves the characters on screen with each model, animation, position, size, and auto-cycle flag. One character is a preset of one. Loading a preset replaces the layout, so it is a quick way to switch scenes.

- Edit mode: press **Presets** in the Characters strip. Type a name and press **Save**. Press a preset name in the list to load it, or its ✕ to delete it.
- Saving under an existing name replaces that preset. A name takes up to 40 letters. The list takes up to 50 presets.
- A load skips a model that is gone. One model left loads as the single layout. No model left changes nothing, and the page says so.
- Characters get new UIDs on each load.
- The server keeps presets in `prefs.json` under `presets`. Local mode keeps them in `localStorage`.
- Command line: `spinewall presets`, `save-preset NAME`, `load-preset NAME`, `delete-preset NAME`. Quote a name that has spaces.

### Presets per monitor

Each monitor shows its own layout, so each can run its own preset. The page names its monitor and the server keeps one layout per name.

- A new monitor starts as a copy of the layout it first finds. Edit mode then changes that monitor only.
- A lone monitor keeps the old behavior. Its page uses the default layout, and the CLI needs no `--screen`.
- **Previous** and **Next** in the Presets pane step this monitor through the saved presets and wrap around. The preset loaded last is highlighted.
- `spinewall next-preset` and `prev-preset` do the same from a shortcut. Without `--screen KEY` they step every monitor, each from its own preset. Add `--screen KEY` to step one.
- `spinewall screens` lists the monitors the server knows, with the last preset of each. Use `--screen KEY` on `load-preset`, `save-preset`, and any other command to act on one monitor.
- A command without `--screen` reaches every monitor. `save-preset` is the exception: it saves the default layout, so name the monitor on a multi-monitor desktop.
- A preset whose models are all gone is skipped when cycling.
- The server keeps the monitor layouts in `screens.json`, up to 16 monitors.

#### Monitor names

The page builds the key from the screen size and position, for example `3440x1440+0+0`, when the desktop has several monitors. Open the page with `?screen=NAME` to choose the key yourself. The key can change when a panel moves or a monitor moves in the display settings. The old key then keeps its layout, and the monitor starts again from the default layout.

#### Shortcuts

Bind `spinewall next-preset --screen current` and `spinewall prev-preset --screen current` as global shortcuts. `current` is the monitor KWin reports as active, which follows the pointer, so the shortcut cycles the monitor you are using. It needs `qdbus6` and `kscreen-doctor`, and a wallpaper page must be connected on that monitor. Without `--screen`, the shortcut cycles every monitor.

Command line: `spinewall add-character [ID]`, `remove-character UID`, `clear-characters`, `next-char-anim UID`, `set-char-anim UID NAME`, `set-char-model UID ID`. `spinewall info` lists each UID under `state.characters`.

## Auto-cycle animations

Each character has an Auto-cycle animations checkbox in the Characters strip and in its row. It is off by default and the server keeps it in `layout.json`.

- An animation plays at least 6 s, rounded up to whole loops, so a motion never cuts off. The next animation in list order plays, wrapping at the end.
- A click on the character restarts the timer. Previewing an animation from the list pauses cycling for the selected character.
- Cycling also runs in normal mode.

Command line: `spinewall auto-cycle on|off` for all characters, `spinewall auto-cycle UID on|off` for one.

## Check a model

1. Copy the model directory into `~/.local/share/spinewall/models/`.
2. Run `spinewall rescan`. Entering edit mode also rescans.
3. Run `spinewall info`. The `models` count rises by one, or the model appears under `skipped` with a reason.
4. Run `spinewall set-model <id>`.

## Where to get models

- Your own Spine projects, exported with "Pack atlas" enabled.
- Spine example projects from Esoteric Software, for testing.
- Assets you own, or have a license to use. Check the owner's terms first.

## Atlas page check

A model with no picture usually lacks an atlas page. List the pages the atlas names:

```bash
grep -E '\.(png|jpg|webp)$' ~/.local/share/spinewall/models/<id>/*.atlas
```

Each name must exist in the same directory.
