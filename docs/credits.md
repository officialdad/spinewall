# Credits

Spinewall ships no models and no Spine runtime. Each project below keeps its own license. Models used for testing were fetched for local, non-commercial use only and are not redistributed.

## Runtime and platform

| Project | Use |
|---|---|
| [Spine and spine-runtimes](https://github.com/EsotericSoftware/spine-runtimes) | WebGL players. 3.8 from commit `8b4844b`, 4.x from npm `@esotericsoftware/spine-player`. Spine Runtimes License applies. |
| [webwallpaper-kde](https://github.com/ekoputrapratama/webwallpaper-kde) | Plugin that shows a web page as the Plasma wallpaper. |
| [Spine-Viewer-Web](https://github.com/FrankoFPM/Spine-Viewer-Web) | Reference for a Spine web viewer. |

## Viewers studied

| Project | Use |
|---|---|
| [nikke-db.pages.dev](https://nikke-db.pages.dev/visualiser) and [Nikke-db](https://github.com/Nikke-db) | Source of the Nikke id to name table, and skin handling (`bg`, `acc`, `default`). Also [spine-web-player-template](https://github.com/Nikke-db/spine-web-player-template). |
| [nikkeviewer.com](https://nikkeviewer.com/viewer) | Reference for character ids such as `c850` and `c820`. |
| [Waifuland](https://github.com/shinkuan/Waifuland) | Reference for a multi-game viewer. |

## Asset sources surveyed

The repos below were read to find Spine assets. Which repo supplied which test model is unconfirmed in this file. Check each repo for its own terms.

| Project | Game |
|---|---|
| [respectZ/blue-archive-spine](https://github.com/respectZ/blue-archive-spine) | Blue Archive |
| [kiraio-moe/Schale-Archive](https://github.com/kiraio-moe/Schale-Archive) | Blue Archive |
| [isHarryh/Ark-Models](https://github.com/isHarryh/Ark-Models) | Arknights |
| [fexli/ArknightsResource](https://github.com/fexli/ArknightsResource) | Arknights |
| [Eukky/Azur-Lane-L2D](https://github.com/Eukky/Azur-Lane-L2D) | Azur Lane |
| [steve1316/gfl-archive](https://github.com/steve1316/gfl-archive) | Girls' Frontline |
| [0x795E69D8/exilium-tools](https://github.com/0x795E69D8/exilium-tools) | Girls' Frontline 2 |
| [Dimbreath/GFL2Data](https://github.com/Dimbreath/GFL2Data), [xudako/gf2e-db](https://github.com/xudako/gf2e-db), [nijinekoyo/GF2AssetBundleDecryption](https://github.com/nijinekoyo/GF2AssetBundleDecryption), [K0lb3/UnityPy](https://github.com/K0lb3/UnityPy) | Girls' Frontline 2 research. Not used. The bundles are encrypted and skipped. |

Games whose characters appear in tests: Nikke (Shift Up), Blue Archive (Nexon Games), Arknights (Hypergryph), Azur Lane (Manjuu, Yostar), Brown Dust 2 (Neowiz), Guardian Tales (Kakao Games), Genshin Impact and Honkai Star Rail (HoYoverse). All characters belong to their owners.

## Test models

| Set | Origin |
|---|---|
| `spineboy`, `raptor`, `goblins`, `hero`, `vine`, `stretchyman` | Esoteric Software example projects, in the `spine-runtimes` repo |
| Game characters | Fetched from the public repos and viewers above for local testing |
