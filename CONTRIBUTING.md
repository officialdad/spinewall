# Contributing

Read [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) first. Report security problems through [SECURITY.md](SECURITY.md), never in a public issue.

## Rules

- Never commit models, `.skel`, `.atlas`, atlas images, or Spine runtime files. The runtime license forbids redistribution. CI rejects them.
- Never add game-specific code or names, in code or in docs.
- Comments explain why, not what.
- Use short sentences and a plain dash, never an em dash.

## Dev setup

1. Clone the repo.
2. Install `python3`, `node` with `npm`, and `shellcheck`.
3. Run the server on a spare port with a temp model directory:
   `SPINEWALL_PORT=18777 SPINEWALL_MODELS=$(mktemp -d) python3 server/spinewall-server.py`
4. Put an Esoteric Software example project into the model directory. See [docs/assets.md](docs/assets.md).

## Run the tests

```bash
python3 -I -m unittest discover -s tests -t . -v
cd tests/page && npm install && npm test
python3 -m py_compile server/spinewall-server.py
shellcheck scripts/*.sh bin/spinewall
```

## Dev machine versus real desktop

Headless tests prove the server and the page logic. They do not prove the wallpaper works. A change to input, drawing, or the Plasma patch needs a test on a real Plasma desktop. Say in the PR if you did not run one.

## Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/) for every commit and every PR title: `feat:`, `fix:`, `perf:`, `refactor:`, `docs:`, `test:`, `build:`, `ci:`, `chore:`.
- Add `!` after the type for a breaking change, for example `feat!: rename a state key`.
- The release tool reads these titles to pick the version and write `CHANGELOG.md`. Never edit `CHANGELOG.md` by hand.
- Do not add an agent or model as co-author.
- Fill in the PR template.
