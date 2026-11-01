# Security policy

## Supported versions

| Version | Supported |
|---|---|
| Latest release | Yes |
| Older releases | No |

## Report a problem

Use a [private security advisory](https://github.com/officialdad/spinewall/security/advisories/new). Never open a public issue for a security problem.

Include the version, the steps to reproduce, and the effect. Expect a first reply within 7 days.

## Scope

- The server listens on `127.0.0.1` only. It rejects other `Host` headers and cross-site origins.
- The server has no login. Any local user can send it commands.
- The Plasma patch changes a local copy of the desktop containment. A stale copy after a `plasma-desktop` update is an error, not a vulnerability.
- Spine runtime and model licenses are out of scope.
