# Changelog

## [0.7.0](https://github.com/officialdad/spinewall/compare/v0.6.0...v0.7.0) (2026-11-24)


### Features

* **cli:** move the command line tool into the Go binary ([#29](https://github.com/officialdad/spinewall/issues/29)) ([0f7c05c](https://github.com/officialdad/spinewall/commit/0f7c05c5a3d5b2f66a1123a399796420cec682ae))
* **install:** add the setup wizard, doctor, manifest-backed install and uninstall, and signed release binaries ([#30](https://github.com/officialdad/spinewall/issues/30)) ([7374782](https://github.com/officialdad/spinewall/commit/737478293d7245236c7ddad1646a8e8f15cd2e1b))
* **release:** verify releases with the real minisign key ([#31](https://github.com/officialdad/spinewall/issues/31)) ([82f541c](https://github.com/officialdad/spinewall/commit/82f541c2296a76dc2a50d0a796ccb1b28409d1ca))
* **runtime:** fetch, verify and serve Spine runtimes from the Go binary ([#28](https://github.com/officialdad/spinewall/issues/28)) ([b14029c](https://github.com/officialdad/spinewall/commit/b14029c09065105a7ceae2d5315c3509fc64eed2))
* **server:** port the Python server to Go ([#27](https://github.com/officialdad/spinewall/issues/27)) ([b77d0a8](https://github.com/officialdad/spinewall/commit/b77d0a8a888333f821ee0f53f3fee082c2e48503))
* **theme:** add an on-canvas handle and chip bar to edit mode ([#26](https://github.com/officialdad/spinewall/issues/26)) ([076ba6e](https://github.com/officialdad/spinewall/commit/076ba6ed6c7c5ef7285d9f51177ae72cc8bd6bfe))
* **theme:** fold the edit-mode panel into a dock that leaves the wallpaper usable ([#24](https://github.com/officialdad/spinewall/issues/24)) ([7f5e35b](https://github.com/officialdad/spinewall/commit/7f5e35b8cf771e00b802f44ff6b9711b0330ddaf))
* **view:** add native layer-shell wallpaper backend ([#33](https://github.com/officialdad/spinewall/issues/33)) ([74ec7f3](https://github.com/officialdad/spinewall/commit/74ec7f38618b30300c1ba1024aabc76beb052860))
* **view:** add X11 and kiosk backends and Hyprland and sway screen providers ([#34](https://github.com/officialdad/spinewall/issues/34)) ([0468e7c](https://github.com/officialdad/spinewall/commit/0468e7c34714385242990d0df4eefa3abe327f31))


### Bug Fixes

* **install:** link the Spine players into the theme folder for file:// ([#32](https://github.com/officialdad/spinewall/issues/32)) ([7f8a4e7](https://github.com/officialdad/spinewall/commit/7f8a4e743789e3a914b984b20ccab3e9acd04e32))
* **theme:** accept mouse events when the host sends no pointer events ([#35](https://github.com/officialdad/spinewall/issues/35)) ([8cf8c8b](https://github.com/officialdad/spinewall/commit/8cf8c8bd7aab2fe0d07d95af9acc99a5ac2a36e2))
* **theme:** stop mouse events at page controls so a control press never clicks the stage ([#36](https://github.com/officialdad/spinewall/issues/36)) ([0f018ac](https://github.com/officialdad/spinewall/commit/0f018ac2d29ca556e23e9377d6c43d81e6ef4980))
* **view:** keep the wallpaper through Peek at Desktop ([#37](https://github.com/officialdad/spinewall/issues/37)) ([91254a3](https://github.com/officialdad/spinewall/commit/91254a30ae58b01443610a280ec19f6c1ceea0c6))

## [0.6.0](https://github.com/officialdad/spinewall/compare/v0.5.0...v0.6.0) (2026-11-08)


### Features

* manage presets in a searchable card screen ([#16](https://github.com/officialdad/spinewall/issues/16)) ([8fde725](https://github.com/officialdad/spinewall/commit/8fde7253e457a69c360a7d7f622420a6fa4ed552))

## [0.5.0](https://github.com/officialdad/spinewall/compare/v0.4.0...v0.5.0) (2026-11-08)


### Features

* log CLI errors to a file when no terminal is attached ([#13](https://github.com/officialdad/spinewall/issues/13)) ([d49ef81](https://github.com/officialdad/spinewall/commit/d49ef8178ab04820c6e459536fca604540a04f39))


### Bug Fixes

* seed new screens from the stage chosen last ([#15](https://github.com/officialdad/spinewall/issues/15)) ([f1341be](https://github.com/officialdad/spinewall/commit/f1341beb578ea2ba7a13f842e9e2f4abf8a752c2))

## [0.4.0](https://github.com/officialdad/spinewall/compare/v0.3.1...v0.4.0) (2026-11-04)


### Features

* run a different preset on each monitor and cycle presets ([079b733](https://github.com/officialdad/spinewall/commit/079b733e38af10674e651a4d7a5f7b0317306c14))
* run a different preset on each monitor and cycle presets ([1567ee8](https://github.com/officialdad/spinewall/commit/1567ee81f946dfb9c4efcb8aa6425dc63cc355a1))
* save and load presets of the characters on screen ([56a27bf](https://github.com/officialdad/spinewall/commit/56a27bfd8427d47ff2980fa8869d20a5050ebfb4))
* save and load presets of the characters on screen ([aa5454c](https://github.com/officialdad/spinewall/commit/aa5454ca0b97527f53721935229c6fa130915076))


### Bug Fixes

* pick the exact screen key when two keys share a monitor position ([bfe1b82](https://github.com/officialdad/spinewall/commit/bfe1b82cab4e142a5ba8720104847a208793726c))

## [0.3.1](https://github.com/officialdad/spinewall/compare/v0.3.0...v0.3.1) (2026-11-02)


### Bug Fixes

* draw straight-alpha 4.x atlases without white lines ([b4b02e1](https://github.com/officialdad/spinewall/commit/b4b02e1215ddbcc834649b4924769b43577dfffd))
* draw straight-alpha 4.x atlases without white lines ([ed9d476](https://github.com/officialdad/spinewall/commit/ed9d476c41f82ffee7af65cebf509c4d696073de))

## [0.3.0](https://github.com/officialdad/spinewall/compare/v0.2.0...v0.3.0) (2026-11-02)


### Features

* support Spine 3.7 models in JSON export ([91917b4](https://github.com/officialdad/spinewall/commit/91917b4a471a53eb3626f8c6bed8412d0012143c))

## [0.2.0](https://github.com/officialdad/spinewall/compare/v0.1.0...v0.2.0) (2026-11-01)


### Features

* build the Pages demo from the real page in local mode ([f746173](https://github.com/officialdad/spinewall/commit/f74617381294b2b7366389faa1336c6832263521))
* scan additional model directories from a config file ([ae8dc99](https://github.com/officialdad/spinewall/commit/ae8dc996b0627e3c2d7acd69beeb4cc039eff5fc))

## 0.1.0 (2026-11-01)


### Features

* run the page without a server, package it for Windows hosts, and skip blank animations ([238258e](https://github.com/officialdad/spinewall/commit/238258ec40062b9130036924a089292cf1e3deee))
