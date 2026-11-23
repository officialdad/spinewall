// Package install writes and removes the files spinewall needs on a user's
// account. Every file it writes is recorded in a manifest so uninstall removes
// exactly those files and nothing else.
package install

import (
	"os"
	"path/filepath"
)

// Paths are every location the installer reads or writes. They default to the
// same XDG-style homes the server uses, so a throwaway HOME moves all of them.
type Paths struct {
	Home     string
	BinDir   string
	Config   string // config.json
	DataDir  string // ~/.local/share
	StateDir string // ~/.local/state/spinewall
	CacheDir string // ~/.cache/spinewall
	Runtime  string // runtime cache directory
}

// DefaultPaths resolves the paths from the environment. A path variable that
// is unset falls back to the home-based default, matching the server.
func DefaultPaths() Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	p := Paths{Home: home}
	p.BinDir = filepath.Join(home, ".local", "bin")
	p.DataDir = filepath.Join(home, ".local", "share")
	p.StateDir = filepath.Join(home, ".local", "state", "spinewall")
	p.Config = filepath.Join(home, ".config", "spinewall", "config.json")
	p.CacheDir = filepath.Join(home, ".cache", "spinewall")
	p.Runtime = filepath.Join(p.CacheDir, "runtime")
	return p
}

// ThemeDir is where the legacy KDE webwallpaper backend reads its theme.
func (p Paths) ThemeDir() string {
	return filepath.Join(p.DataDir, "webwallpaper", "themes", "spinewall")
}

// ManifestPath is the file that records everything installed.
func (p Paths) ManifestPath() string {
	return filepath.Join(p.StateDir, "install-manifest.json")
}

// PidFile is where a running server writes its process id.
func (p Paths) PidFile() string {
	return filepath.Join(p.StateDir, "server.pid")
}

// ModelsDir is the primary model directory the server scans.
func (p Paths) ModelsDir() string {
	return filepath.Join(p.DataDir, "spinewall", "models")
}

// AutostartFile is the XDG autostart entry.
func (p Paths) AutostartFile() string {
	return filepath.Join(p.Home, ".config", "autostart", "spinewall.desktop")
}

// UnitFile is the optional systemd user unit.
func (p Paths) UnitFile() string {
	return filepath.Join(p.Home, ".config", "systemd", "user", "spinewall.service")
}
