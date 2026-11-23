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
	"runtime"
	"strings"
)

// Scope is the layer-shell scope name the surface registers under.
const Scope = "spinewall"

// Backend names the native view surface the launcher draws.
const (
	// BackendLayerShell is one layer-shell window per monitor, on a Wayland
	// compositor that offers zwlr_layer_shell_v1.
	BackendLayerShell = "layer-shell"
	// BackendX11 is one desktop-type window per monitor, on a real X11 session.
	// X11 has no layer-shell, so the window asks the WM to keep it at the
	// bottom. It is best effort and unverified; see docs/desktops.md.
	BackendX11 = "x11"
)

// SessionBackend picks the native view surface for a session. A real X11
// session gets the desktop-type window; everything else gets layer-shell, which
// is the proven backend where the compositor offers it.
func SessionBackend(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	if getenv("XDG_SESSION_TYPE") == "x11" {
		return BackendX11
	}
	return BackendLayerShell
}

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

// DetectResult reports whether this machine can run the native view.
type DetectResult struct {
	// LayerShell is true when the compositor offers zwlr_layer_shell_v1.
	LayerShell bool
	// X11 is true on a real X11 session, where the desktop-type window backend
	// can run instead of layer-shell.
	X11 bool
	// LayerShellKnown is false when no probe ran and the desktop is not known
	// either way, so LayerShell false only means "not proven".
	LayerShellKnown bool
	// WebEngine is true when the Qt WebEngine QML module is present.
	WebEngine bool
	// LayerShellQML is true when the org.kde.layershell QML module that view.qml
	// imports is present. Without it the window cannot load.
	LayerShellQML bool
	// Buildable is true when moc, a C++ compiler, pkg-config and the Qt
	// development modules are all present, so the launcher can be built.
	Buildable bool
	// BuildMissing names each build requirement that is missing.
	BuildMissing []string
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

// layerShellQMLModules are where distros install layer-shell-qt's QML module.
var layerShellQMLModules = []string{
	"/usr/lib/qt6/qml/org/kde/layershell",
	"/usr/lib64/qt6/qml/org/kde/layershell",
	"/usr/lib/x86_64-linux-gnu/qt6/qml/org/kde/layershell",
	"/usr/lib/aarch64-linux-gnu/qt6/qml/org/kde/layershell",
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
		return r
	}

	r.X11 = getenv("XDG_SESSION_TYPE") == "x11"
	r.LayerShell, r.LayerShellKnown = detectLayerShell(getenv, opts.WaylandGlobals)
	r.WebEngine = QtWebEngineDir(exists) != ""
	r.LayerShellQML = anyExists(exists, layerShellQMLModules)
	compiler, moc, pkgOK := detectToolchain(lookPath)
	pkgModules := hasPkg(launcherPkgs)
	r.Buildable = compiler && moc && pkgOK && pkgModules

	if !compiler {
		r.BuildMissing = append(r.BuildMissing, "C++ compiler")
	}
	if !moc {
		r.BuildMissing = append(r.BuildMissing, "moc")
	}
	if !pkgOK {
		r.BuildMissing = append(r.BuildMissing, "pkg-config")
	}
	if !pkgModules {
		r.BuildMissing = append(r.BuildMissing, "Qt development modules")
	}

	return r
}

// Ready reports whether the layer-shell view can build and load on this machine.
func (r DetectResult) Ready() bool {
	return r.LayerShell && r.WebEngine && r.LayerShellQML && r.Buildable
}

// ReadyX11 reports whether the X11 view can build and load on this machine. It
// needs the same launcher and web engine as layer-shell but the layer-shell QML
// module, which X11 does not have.
func (r DetectResult) ReadyX11() bool {
	return r.X11 && r.WebEngine && r.Buildable
}

// detectLayerShell reports whether the compositor offers layer-shell, and
// whether that answer is known. A globals probe decides first; without one, only
// a desktop known either way gives an answer.
func detectLayerShell(getenv func(string) string, globalsFile string) (offered, known bool) {
	if getenv("XDG_SESSION_TYPE") != "wayland" {
		return false, true
	}
	if globalsFile != "" {
		if data, err := os.ReadFile(globalsFile); err == nil {
			return strings.Contains(string(data), "zwlr_layer_shell_v1"), true
		}
	}
	desktop := getenv("XDG_CURRENT_DESKTOP")
	if strings.TrimSpace(desktop) == "" {
		desktop = getenv("DESKTOP_SESSION")
	}
	for _, name := range strings.Split(strings.ToLower(desktop), ":") {
		if name == "gnome" {
			return false, true
		}
		for _, known := range layerShellDesktops {
			if name == known {
				return true, true
			}
		}
	}
	return false, false
}

// QtWebEngineDir returns the Qt WebEngine QML module directory, or "" when
// none is installed.
func QtWebEngineDir(exists func(string) bool) string {
	for _, dir := range qtWebEngineModules {
		if exists(dir) {
			return dir
		}
	}
	return ""
}

func anyExists(exists func(string) bool, dirs []string) bool {
	for _, dir := range dirs {
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
