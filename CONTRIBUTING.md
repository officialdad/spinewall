# Contributing

- Never commit models, `.skel`, `.atlas`, atlas images, or Spine runtime files. CI rejects them.
- Never add game-specific code or names.
- Run `python3 -m py_compile server/spinewall-server.py` and `shellcheck scripts/*.sh bin/spinewall` before a pull request.
- Test on a real Plasma desktop. See `CLAUDE.md`.
