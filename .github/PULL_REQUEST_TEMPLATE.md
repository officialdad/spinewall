## Needs you

<!-- One line. What the human must do. Example: Merge, or request changes. -->

## Summary

<!-- 2 to 4 short sentences. What changes and why. -->

## Risk

<!-- One line, or: None known. -->

<details>
<summary>Agent detail</summary>

### Component(s)

<!-- Name the directories or services touched. -->

### Type

<!-- Keep the lines that apply. -->

- [ ] Added
- [ ] Changed
- [ ] Fixed
- [ ] Removed
- [ ] Plumbing (CI, shared config, docs)

### Changes

<!-- One bullet per notable change. -->

-

### Validation

<!-- List each check you ran and its result. -->

- [ ] The title follows Conventional Commits, for example `fix: reject a bad Host header`.
- [ ] No models, `.skel`, `.atlas`, atlas images, or Spine runtime files are added outside `docs/demo/models/`.
- [ ] No game names or game-specific code are added.
- [ ] Tests pass: `python3 -I -m unittest discover -s tests -t . -v` and `cd tests/page && npm test`.
- [ ] Docs are updated: `README.md`, `docs/`, and `CLAUDE.md` where the change affects them.
- [ ] A visual change was tried on a real Plasma desktop, or the PR says it was not.

</details>
