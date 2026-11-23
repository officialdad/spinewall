// Package theme embeds the wallpaper page and its local host so one binary can serve them.
// The Spine players are never embedded: the runtime license forbids redistribution.
package theme

import _ "embed"

// IndexHTML is theme/index.html, the wallpaper page.
//
//go:embed index.html
var IndexHTML []byte

// LocalHostJS is theme/local-host.js, the server's state logic for hosts with no server.
//
//go:embed local-host.js
var LocalHostJS []byte

// ThemeManifest is theme/spinewall.theme, the legacy webwallpaper theme manifest
// the KDE backend reads.
//
//go:embed spinewall.theme
var ThemeManifest []byte
