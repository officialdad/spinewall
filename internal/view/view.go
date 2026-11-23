// Package view selects and launches the native wallpaper backend: a
// layer-shell window per monitor that runs the page in QtWebEngine.
//
// It never embeds a web engine. The launcher is a small C++ program built on
// the user's machine from source kept here, because QtWebEngine cannot be
// linked into a CGO_ENABLED=0 Go binary. The surface, WebGL, click delivery and
// the QWindow input mask were proven on KDE Plasma 6 Wayland.
package view

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Backend names a wallpaper host. The native layer-shell backend is this
// package; the KDE legacy backend is the webwallpaper plugin plus the patched
// containment, which stays installable until the cutover.
type Backend string

const (
	BackendLayerShell Backend = "layer-shell"
	BackendKDELegacy  Backend = "kde-legacy"
	BackendNone       Backend = "none"
)

// Scope is the layer-shell scope name the surface registers under.
const Scope = "spinewall"

// Options inject every detection source so a test never touches the real box.
type Options struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Exists   func(string) bool
	// WaylandGlobals is a file holding `wayland-info` output. Empty falls back
	// to the known desktops, as doctor does.
	WaylandGlobals string
	// HasPkg reports whether the named pkg-config modules resolve. Nil runs
	// pkg-config itself.
	HasPkg func([]string) bool
}

// Detect reports which native backend this machine can run and why.
type DetectResult struct {
	// Recommended is the backend to preselect in setup and doctor.
	Recommended Backend
	// LayerShell is true when the compositor offers zwlr_layer_shell_v1.
	LayerShell bool
	// WebEngine is true when the Qt WebEngine QML module is present.
	WebEngine bool
	// Buildable is true when moc, a C++ compiler, pkg-config and the Qt
	// development modules are all present, so the launcher can be built.
	Buildable bool
	// Reasons names each missing piece, empty when Buildable and WebEngine.
	Reasons []string
}

// layerShellDesktops offer zwlr_layer_shell_v1. GNOME is the common Wayland
// desktop that does not.
var layerShellDesktops = []string{"kde", "sway", "hyprland", "river", "wayfire", "labwc", "niri"}

// qtWebEngineModules are where distros install the QtWebEngine QML module.
var qtWebEngineModules = []string{
	"/usr/lib/qt6/qml/QtWebEngine",
	"/usr/lib64/qt6/qml/QtWebEngine",
	"/usr/lib/x86_64-linux-gnu/qt6/qml/QtWebEngine",
	"/usr/lib/aarch64-linux-gnu/qt6/qml/QtWebEngine",
}

// launcherPkgs are the pkg-config modules the launcher links against.
var launcherPkgs = []string{"Qt6Quick", "Qt6Qml", "Qt6WebEngineQuick", "Qt6WebChannel", "Qt6Gui", "Qt6Network", "Qt6DBus"}

// Detect inspects the machine.
func Detect(opts Options) DetectResult {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	exists := opts.Exists
	if exists == nil {
		exists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	}
	hasPkg := opts.HasPkg
	if hasPkg == nil {
		hasPkg = func(pkgs []string) bool { return pkgConfigHas(pkgs) }
	}

	var r DetectResult
	if runtime.GOOS != "linux" {
		r.Recommended = BackendNone
		r.Reasons = append(r.Reasons, "the native view runs on Linux only")
		return r
	}

	r.LayerShell = detectLayerShell(getenv, opts.WaylandGlobals)
	r.WebEngine = detectWebEngine(exists)
	compiler, moc, pkgOK := detectToolchain(lookPath)
	pkgModules := hasPkg(launcherPkgs)
	r.Buildable = compiler && moc && pkgOK && pkgModules

	if !r.LayerShell {
		r.Reasons = append(r.Reasons, "the compositor does not offer zwlr_layer_shell_v1")
	}
	if !r.WebEngine {
		r.Reasons = append(r.Reasons, "the Qt WebEngine QML module is missing")
	}
	if !compiler {
		r.Reasons = append(r.Reasons, "no C++ compiler (install a C++ toolchain)")
	}
	if !moc {
		r.Reasons = append(r.Reasons, "no moc (install the Qt base development package)")
	}
	if !pkgOK {
		r.Reasons = append(r.Reasons, "no pkg-config (install pkgconf)")
	}
	if !pkgModules {
		r.Reasons = append(r.Reasons, "missing Qt development modules: "+strings.Join(launcherPkgs, " "))
	}

	switch {
	case r.LayerShell && r.WebEngine && r.Buildable:
		r.Recommended = BackendLayerShell
	case isKDE(getenv):
		r.Recommended = BackendKDELegacy
		r.Reasons = append(r.Reasons, "falling back to the KDE legacy backend")
	default:
		r.Recommended = BackendNone
	}
	return r
}

// Available lists the backends this machine could run, recommended first.
func (r DetectResult) Available() []Backend {
	var out []Backend
	if r.Recommended == BackendLayerShell {
		out = append(out, BackendLayerShell)
	}
	if isKDEFromReport(r) || r.Recommended == BackendKDELegacy {
		out = append(out, BackendKDELegacy)
	} else if r.Recommended != BackendLayerShell {
		// The legacy backend is KDE only, but doctor still names it as the
		// fallback the user can pick explicitly.
		out = append(out, BackendKDELegacy)
	}
	return out
}

// isKDEFromReport is a best-effort hint; detection already put availabay into
// Recommended, so callers prefer that.
func isKDEFromReport(r DetectResult) bool { return false }

func isKDE(getenv func(string) string) bool {
	desktop := strings.ToLower(getenv("XDG_CURRENT_DESKTOP") + ":" + getenv("DESKTOP_SESSION"))
	for _, name := range strings.Split(desktop, ":") {
		if name == "kde" || name == "plasma" {
			return true
		}
	}
	return false
}

func detectLayerShell(getenv func(string) string, globalsFile string) bool {
	if getenv("XDG_SESSION_TYPE") != "wayland" {
		return false
	}
	if globalsFile != "" {
		if data, err := os.ReadFile(globalsFile); err == nil {
			return strings.Contains(string(data), "zwlr_layer_shell_v1")
		}
	}
	desktop := strings.ToLower(getenv("XDG_CURRENT_DESKTOP"))
	for _, name := range strings.Split(desktop, ":") {
		for _, known := range layerShellDesktops {
			if name == known {
				return true
			}
		}
	}
	return false
}

func detectWebEngine(exists func(string) bool) bool {
	for _, dir := range qtWebEngineModules {
		if exists(dir) {
			return true
		}
	}
	return false
}

func detectToolchain(lookPath func(string) (string, error)) (compiler, moc, pkg bool) {
	if _, err := lookPath("c++"); err == nil {
		compiler = true
	} else if _, err := lookPath("g++"); err == nil {
		compiler = true
	}
	_, mocErr := lookPath("moc")
	moc = mocErr == nil
	_, pkgErr := lookPath("pkg-config")
	pkg = pkgErr == nil
	return
}

// pkgConfigHas reports whether every module resolves through pkg-config.
func pkgConfigHas(pkgs []string) bool {
	for _, name := range pkgs {
		if exec.Command("pkg-config", "--exists", name).Run() != nil {
			return false
		}
	}
	return true
}

// ViewDir is the cache directory the launcher source and binary live in.
func ViewDir(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	if dir := getenv("SPINEWALL_VIEW_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cache", "spinewall", "view")
}
