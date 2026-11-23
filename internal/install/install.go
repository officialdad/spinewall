package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/officialdad/spinewall/theme"
)

// Backend names the wallpaper host the installer can prepare. Until the
// layer-shell view ships, only the KDE legacy backend can be installed.
type Backend string

const (
	BackendKDEBackend Backend = "kde-legacy"
)

// Options configure one install run.
type Options struct {
	Paths Paths
	// Backend selects what the install prepares. Only BackendKDEBackend is
	// installable now; the wizard lists the others as not available yet.
	Backend Backend
	// SystemdUnit writes the opt-in systemd user unit.
	SystemdUnit bool
	// ModelDir is the models folder a user picked. When it differs from the
	// default, it is written to config.json as an additional model dir.
	ModelDir string
	// Executable is the binary to install as ~/.local/bin/spinewall. Empty uses
	// the running executable.
	Executable string
	// Logf receives progress lines. Nil discards them.
	Logf func(string)
}

// Result reports what an install wrote.
type Result struct {
	Manifest  Manifest
	BinPath   string
	ThemeDir  string
	Autostart string
	UnitFile  string
}

// Install writes the backend files, the binary, the autostart entry, and the
// optional systemd unit, then records them in the manifest. It is idempotent:
// a second run rewrites the same bytes and keeps the same manifest entries.
func Install(opts Options) (Result, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string) {}
	}
	paths := opts.Paths
	if paths.Home == "" {
		paths = DefaultPaths()
	}
	if opts.Backend == "" {
		opts.Backend = BackendKDEBackend
	}
	if opts.Backend != BackendKDEBackend {
		return Result{}, fmt.Errorf("backend %q is not installable yet", opts.Backend)
	}

	w := newWriter(paths)

	// The binary. Copy it when it is running from somewhere else, so a `go install`
	// or a repo build lands in ~/.local/bin. When it already runs from there,
	// record the existing bytes without rewriting a file the process has open.
	binPath := filepath.Join(paths.BinDir, "spinewall")
	self := opts.Executable
	if self == "" {
		self, _ = os.Executable()
	}
	if self != "" && sameFile(self, binPath) {
		if err := w.ensureDir(paths.BinDir); err != nil {
			return Result{}, err
		}
		hash, err := sha256File(binPath)
		if err != nil {
			return Result{}, err
		}
		w.files[binPath] = Entry{Path: binPath, SHA256: hash, Mode: "0755", Created: true}
	} else if self != "" {
		data, err := os.ReadFile(self)
		if err != nil {
			return Result{}, fmt.Errorf("read the running binary: %w", err)
		}
		if err := w.ensureDir(paths.BinDir); err != nil {
			return Result{}, err
		}
		if err := w.write(binPath, data, 0o755); err != nil {
			return Result{}, err
		}
	} else {
		return Result{}, fmt.Errorf("cannot find the spinewall binary to install")
	}
	logf("installed binary: " + binPath)

	// The KDE legacy backend is the webwallpaper theme directory the plugin reads.
	themeDir := paths.ThemeDir()
	files := []struct {
		name string
		data []byte
	}{
		{"index.html", theme.IndexHTML},
		{"local-host.js", theme.LocalHostJS},
		{"spinewall.theme", theme.ThemeManifest},
	}
	for _, f := range files {
		if err := w.write(filepath.Join(themeDir, f.name), f.data, 0o644); err != nil {
			return Result{}, err
		}
	}
	logf("installed theme: " + themeDir)

	// XDG autostart is the portable default, so the server runs at login without
	// assuming systemd.
	if err := w.write(paths.AutostartFile(), []byte(autostartEntry(binPath)), 0o644); err != nil {
		return Result{}, err
	}
	logf("installed autostart: " + paths.AutostartFile())

	if opts.SystemdUnit {
		if err := w.write(paths.UnitFile(), []byte(unitFile(binPath)), 0o644); err != nil {
			return Result{}, err
		}
		logf("installed systemd unit: " + paths.UnitFile())
	}

	// A chosen models folder becomes an extra scan root, which the server reads
	// from config.json. Config is installed state, so it is recorded too.
	if opts.ModelDir != "" && opts.ModelDir != paths.ModelsDir() {
		if err := WriteModelDir(w, paths.Config, opts.ModelDir, paths.ModelsDir()); err != nil {
			return Result{}, err
		}
		logf("models folder: " + opts.ModelDir)
	}
	// The models directory always exists so a first run has somewhere to put a
	// model and the server scan has a folder to read.
	if err := w.ensureDir(paths.ModelsDir()); err != nil {
		return Result{}, err
	}
	if opts.ModelDir != "" && opts.ModelDir != paths.ModelsDir() {
		if err := w.ensureDir(opts.ModelDir); err != nil {
			return Result{}, err
		}
	}

	m := w.manifest()
	// The manifest lives in the state dir, so record the dirs this run created on
	// the way to writing it. Uninstall removes the manifest file itself and then
	// any of those dirs left empty.
	if err := w.ensureDir(paths.StateDir); err != nil {
		return Result{}, err
	}
	m = w.manifest()
	if err := m.save(paths.ManifestPath()); err != nil {
		return Result{}, err
	}
	res := Result{
		Manifest:  m,
		BinPath:   binPath,
		ThemeDir:  themeDir,
		Autostart: paths.AutostartFile(),
	}
	if opts.SystemdUnit {
		res.UnitFile = paths.UnitFile()
	}
	return res, nil
}

// sameFile reports whether two paths name the same file on disk.
func sameFile(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// autostartEntry is the XDG desktop entry that runs the server at login.
func autostartEntry(binPath string) string {
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Spinewall",
		"Comment=Live Spine wallpaper control server",
		"Exec=" + shellQuote(binPath) + " serve",
		"Terminal=false",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
}

// unitFile is the opt-in systemd user unit. It keeps the hardening of the old
// Python unit with the Go binary as its ExecStart.
func unitFile(binPath string) string {
	return strings.Join([]string{
		"[Unit]",
		"Description=Spinewall control server",
		"StartLimitIntervalSec=60",
		"StartLimitBurst=5",
		"",
		"[Service]",
		"ExecStart=" + shellQuote(binPath) + " serve",
		"Restart=on-failure",
		"RestartSec=2",
		"NoNewPrivileges=yes",
		"PrivateTmp=yes",
		"ProtectSystem=strict",
		"ProtectHome=read-only",
		"ReadWritePaths=%h/.local/state/spinewall %h/.cache/spinewall",
		"RestrictAddressFamilies=AF_INET AF_UNIX",
		"ProtectKernelTunables=yes",
		"ProtectControlGroups=yes",
		"LockPersonality=yes",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
}

// shellQuote wraps a path for an Exec line. A path with no space or quote is
// left bare so the entry stays readable.
func shellQuote(path string) string {
	if !strings.ContainsAny(path, " \t\"'\\") {
		return path
	}
	return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
}
