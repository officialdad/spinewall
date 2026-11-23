package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/officialdad/spinewall/theme"
)

// Backend names the wallpaper host the installer can prepare.
type Backend string

const (
	// BackendKDEBackend is the webwallpaper plugin plus the patched containment.
	BackendKDEBackend Backend = "kde-legacy"
	// BackendLayerShell is the native view: one layer-shell window per monitor.
	BackendLayerShell Backend = "layer-shell"
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
	// AllowLegacy writes the autostart entry even when the old Python install is
	// still present, which the user confirmed. Both start a server on one port.
	AllowLegacy bool
	// ViewDir is where the native launcher source and binary are written. Empty
	// uses the default cache directory. Ignored by the KDE legacy backend.
	ViewDir string
	// BuildView compiles the launcher when true. setup passes false in a test
	// with no Qt toolchain, so the files are still written for a later build.
	BuildView bool
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
	// ModelDir is the chosen models folder as an absolute path.
	ModelDir string
	// Legacy lists the old Python install files found, which held back autostart.
	Legacy []string
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
	if opts.Backend != BackendKDEBackend && opts.Backend != BackendLayerShell {
		return Result{}, fmt.Errorf("backend %q is not installable yet", opts.Backend)
	}
	modelDir, err := absModelDir(paths, opts.ModelDir)
	if err != nil {
		return Result{}, err
	}
	legacy := DetectLegacy(paths)
	startAtLogin := len(legacy) == 0 || opts.AllowLegacy
	if startAtLogin && opts.SystemdUnit && isLegacyUnit(paths.UnitFile()) {
		return Result{}, fmt.Errorf("%s is the old Python unit. Run 'spinewall uninstall --legacy' first", paths.UnitFile())
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

	// The backend decides the files and the autostart command. The KDE legacy
	// backend writes the webwallpaper theme the plugin reads; the native backend
	// writes the launcher source and binary and starts the server with a view.
	var themeDir string
	var autostartCommand []string
	switch opts.Backend {
	case BackendLayerShell:
		viewDir, err := installView(w, paths, opts, logf)
		if err != nil {
			return Result{}, err
		}
		themeDir = viewDir
		autostartCommand = []string{"serve", "--with-view"}
		logf("backend: layer-shell view (" + viewDir + ")")
	default:
		// The KDE legacy backend is the webwallpaper theme directory the plugin reads.
		themeDir = paths.ThemeDir()
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
		// The page loads the Spine players by relative URL over file://, so link the
		// cached players into the theme folder. A cache that is still empty (an
		// offline first run) gets its links on the next setup or fetch.
		runtimeEntries, err := ThemeRuntimeEntries(paths, logf)
		if err != nil {
			return Result{}, err
		}
		for _, e := range runtimeEntries {
			if prev, ok := w.prev.entry(e.Path); ok && prev.Created {
				e.Created = true
			}
			w.files[e.Path] = e
		}
		autostartCommand = []string{"serve"}
		logf("installed theme: " + themeDir)
	}

	// XDG autostart is the portable default, so the server runs at login without
	// assuming systemd. Next to the old Python install it would start a second
	// server on the same port, so it waits until the user confirms.
	if startAtLogin {
		if err := w.write(paths.AutostartFile(), []byte(autostartEntry(binPath, autostartCommand...)), 0o644); err != nil {
			return Result{}, err
		}
		logf("installed autostart: " + paths.AutostartFile())
	} else {
		logf("skipped autostart: the old Python install is still present. Run 'spinewall uninstall --legacy', then install again.")
	}

	if opts.SystemdUnit && startAtLogin {
		if err := w.write(paths.UnitFile(), []byte(unitFile(binPath)), 0o644); err != nil {
			return Result{}, err
		}
		logf("installed systemd unit: " + paths.UnitFile())
	}

	// A chosen models folder becomes an extra scan root, which the server reads
	// from config.json. Config is installed state, so it is recorded too.
	if modelDir != paths.ModelsDir() {
		if err := WriteModelDir(w, paths.Config, modelDir, paths.ModelsDir()); err != nil {
			return Result{}, err
		}
		logf("models folder: " + modelDir)
	}
	// The models directory always exists so a first run has somewhere to put a
	// model and the server scan has a folder to read.
	if err := w.ensureDir(paths.ModelsDir()); err != nil {
		return Result{}, err
	}
	if err := w.ensureDir(modelDir); err != nil {
		return Result{}, err
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
		Manifest: m,
		BinPath:  binPath,
		ThemeDir: themeDir,
		ModelDir: modelDir,
		Legacy:   legacy,
	}
	if startAtLogin {
		res.Autostart = paths.AutostartFile()
		if opts.SystemdUnit {
			res.UnitFile = paths.UnitFile()
		}
	}
	return res, nil
}

// absModelDir resolves a models folder the way a user types it: empty is the
// default, a leading ~ is the home, and a relative path is from the working
// directory. The server only scans absolute dirs, and uninstall runs from
// anywhere, so only an absolute path is recorded.
func absModelDir(paths Paths, dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	switch {
	case dir == "":
		return paths.ModelsDir(), nil
	case dir == "~":
		dir = paths.Home
	case strings.HasPrefix(dir, "~/"):
		dir = filepath.Join(paths.Home, dir[2:])
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("models folder %q: %w", dir, err)
	}
	return abs, nil
}

// DetectLegacy lists the old Python install files still on disk: its server
// script and its systemd unit. Either one means a login could start the old
// server on the port the new one uses.
func DetectLegacy(paths Paths) []string {
	var found []string
	if _, err := os.Stat(paths.LegacyServer()); err == nil {
		found = append(found, paths.LegacyServer())
	}
	if isLegacyUnit(paths.UnitFile()) {
		found = append(found, paths.UnitFile())
	}
	return found
}

// isLegacyUnit reports whether the unit file runs the old Python server rather
// than `spinewall serve`.
func isLegacyUnit(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(data), "spinewall-server")
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

// autostartEntry is the XDG desktop entry that runs the server at login, with
// the given subcommand and flags.
func autostartEntry(binPath string, command ...string) string {
	execLine := shellQuote(binPath)
	if len(command) > 0 {
		execLine += " " + strings.Join(command, " ")
	}
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Spinewall",
		"Comment=Live Spine wallpaper control server",
		"Exec=" + execLine,
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
