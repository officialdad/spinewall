## What and why

<!-- Explain why the change is needed, not only what changed. -->

## Checklist

- [ ] The title follows Conventional Commits, for example `fix: reject a bad Host header`.
- [ ] No models, `.skel`, `.atlas`, atlas images, or Spine runtime files are added.
- [ ] No game names or game-specific code are added.
- [ ] Tests pass: `python3 -I -m unittest discover -s tests -t . -v` and `cd tests/page && npm test`.
- [ ] Docs are updated: `README.md`, `docs/`, and `CLAUDE.md` where the change affects them.
- [ ] A visual change was tried on a real Plasma desktop, or the PR says it was not.
