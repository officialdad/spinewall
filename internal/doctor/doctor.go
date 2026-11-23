// Package doctor detects what a machine offers for running spinewall and prints
// one found/missing table with the exact package command per distro. It never
// runs a package manager itself.
package doctor

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Status is one row's result.
type Status string

const (
	Found   Status = "found"
	Missing Status = "missing"
	Unknown Status = "unknown"
)

// Check is one detected fact with the package that supplies it.
type Check struct {
	Name    string
	Status  Status
	Detail  string
	Package string // the package command for this distro, when a package helps
	// Needed marks a check the KDE legacy backend requires to show the wallpaper.
	Needed bool
}

// Report is the full detection result.
type Report struct {
	OS            string
	SessionType   string
	Desktop       string
	Distro        string
	Checks        []Check
	Backend       string // the recommended backend
	PackageCmd    string // the full package command for this distro, empty when unknown
	SystemdUser   bool
	Network       bool
	LayerShell    Status
	WebEngineName string
	// Buildable is true when the C++ toolchain, moc and pkg-config can build the
	// native view launcher. The layer-shell backend needs it.
	Buildable bool
}

// Options let tests replace the detection sources.
type Options struct {
	Getenv        func(string) string
	LookPath      func(string) (string, error)
	Exists        func(string) bool
	ReadOSRelease func(string) (map[string]string, error)
	Network       func() bool
	// WaylandGlobals is a file listing the compositor's Wayland globals, as
	// wayland-info prints them. Empty falls back to the known desktops.
	WaylandGlobals string
}

// Detect runs every check and returns a report.
func Detect(opts Options) Report {
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
		exists = fileExists
	}

	r := Report{
		OS:          runtime.GOOS,
		SessionType: firstNonEmpty(getenv("XDG_SESSION_TYPE"), "unknown"),
		Desktop:     firstNonEmpty(getenv("XDG_CURRENT_DESKTOP"), getenv("DESKTOP_SESSION"), "unknown"),
	}

	release := readOSRelease(opts.ReadOSRelease)
	r.Distro = firstNonEmpty(release["ID"], "unknown")
	// ID_LIKE names the family, so an Arch derivative resolves to the pacman
	// command without naming every derivative.
	r.PackageCmd = packageCmd(r.Distro, release["ID_LIKE"])

	// Session and desktop.
	r.Checks = append(r.Checks,
		Check{Name: "session type", Status: foundOrMissing(r.SessionType != "unknown" && r.SessionType != ""), Detail: r.SessionType, Needed: true},
		Check{Name: "desktop", Status: foundOrMissing(r.Desktop != "unknown" && r.Desktop != ""), Detail: r.Desktop, Needed: true},
	)

	// Layer-shell is offered by the compositor. On Wayland read the globals a
	// probe would see; off Wayland it is simply not available.
	r.LayerShell = detectLayerShell(getenv, opts.WaylandGlobals, r.Desktop)
	r.Checks = append(r.Checks, Check{
		Name:   "layer-shell",
		Status: r.LayerShell,
		Detail: layerShellDetail(r.LayerShell, r.SessionType),
	})

	// GPU from /sys/class/drm.
	gpu, gpuDetail := detectGPU(exists)
	r.Checks = append(r.Checks, Check{Name: "GPU", Status: gpu, Detail: gpuDetail})

	// Web engine candidates the KDE backend and the future layer-shell view need.
	webEngine, webName, webPkg := detectWebEngine(lookPath, exists, r.PackageCmd)
	r.WebEngineName = webName
	r.Checks = append(r.Checks, Check{
		Name:    "web engine",
		Status:  webEngine,
		Detail:  webName,
		Package: webPkg,
		Needed:  true,
	})

	// The Plasma wallpaper plugin. Only needed by the KDE legacy backend.
	if _, err := lookPath("plasmashell"); err == nil {
		r.Checks = append(r.Checks, Check{Name: "plasmashell", Status: Found, Detail: "KDE Plasma session"})
	}

	// The C++ toolchain and Qt development files the native view launcher needs.
	// It builds on the user's machine because QtWebEngine cannot link into a
	// static Go binary.
	buildable, buildDetail := detectBuildToolchain(lookPath)
	r.Buildable = buildable
	r.Checks = append(r.Checks, Check{
		Name:    "view build toolchain",
		Status:  foundOrMissing(buildable),
		Detail:  buildDetail,
		Package: toolchainPackage(r.Distro, release["ID_LIKE"]),
	})

	// systemd user session.
	r.SystemdUser = detectSystemdUser(lookPath, exists)
	r.Checks = append(r.Checks, Check{
		Name:    "systemd user session",
		Status:  foundOrMissing(r.SystemdUser),
		Detail:  systemdDetail(r.SystemdUser),
		Package: "",
	})

	// Network, so the first run can fetch the Spine runtimes.
	network := opts.Network
	if network == nil {
		network = defaultNetwork
	}
	r.Network = network()
	r.Checks = append(r.Checks, Check{
		Name:    "network",
		Status:  foundOrMissing(r.Network),
		Detail:  networkDetail(r.Network),
		Package: "",
	})

	// Both backends are reported. The native layer-shell view needs a layer-shell
	// compositor, Qt WebEngine and the build toolchain; kde-legacy needs Plasma
	// and the webwallpaper plugin. setup preselects the recommended one.
	r.Backend = nativeRecommendation(r)
	return r
}

// nativeRecommendation picks the backend this machine should run. The native
// layer-shell view is preferred when the whole chain is present. The KDE legacy
// backend is the fallback for Plasma users until the cutover, and the default
// elsewhere, because it is the only backend this release ships for them.
func nativeRecommendation(r Report) string {
	if r.LayerShell == Found && r.WebEngineName != "" && !strings.HasPrefix(r.WebEngineName, "chromium") &&
		!strings.HasPrefix(r.WebEngineName, "firefox") && !strings.HasPrefix(r.WebEngineName, "google") &&
		r.Buildable {
		return "layer-shell"
	}
	return "kde-legacy"
}

// Backends lists the backends a user may pick, recommended first. The native
// view is offered only when this machine can run it; the legacy backend is
// always offered as the Plasma fallback.
func (r Report) Backends() []string {
	out := []string{}
	if r.Backend == "layer-shell" {
		out = append(out, "layer-shell")
	}
	out = append(out, "kde-legacy")
	return out
}

// Missing lists the checks that are missing and needed.
func (r Report) Missing() []Check {
	var out []Check
	for _, c := range r.Checks {
		if c.Status == Missing && c.Needed {
			out = append(out, c)
		}
	}
	return out
}

// PackageHints returns one package command per missing needed check that names
// a package, so the user can copy it. Duplicates are dropped.
func (r Report) PackageHints() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range r.Missing() {
		if c.Package == "" || seen[c.Package] {
			continue
		}
		seen[c.Package] = true
		out = append(out, c.Package)
	}
	return out
}

// ---- detection helpers ----

// layerShellDesktops offer zwlr_layer_shell_v1. GNOME is the common Wayland
// desktop that does not.
var layerShellDesktops = []string{"kde", "sway", "hyprland", "river", "wayfire", "labwc", "niri"}

func detectLayerShell(getenv func(string) string, globalsFile, desktop string) Status {
	if getenv("XDG_SESSION_TYPE") != "wayland" {
		return Missing
	}
	if globalsFile != "" {
		if data, err := os.ReadFile(globalsFile); err == nil {
			return foundOrMissing(strings.Contains(string(data), "zwlr_layer_shell_v1"))
		}
	}
	// No probe ran, so only a desktop known either way gives an answer.
	for _, name := range strings.Split(strings.ToLower(desktop), ":") {
		if name == "gnome" {
			return Missing
		}
		for _, known := range layerShellDesktops {
			if name == known {
				return Found
			}
		}
	}
	return Unknown
}

func layerShellDetail(status Status, session string) string {
	if session != "wayland" {
		return "needs a Wayland session"
	}
	switch status {
	case Found:
		return "Wayland layer-shell compositor"
	case Missing:
		return "compositor does not offer zwlr_layer_shell_v1"
	}
	return "not known for this compositor"
}

func detectGPU(exists func(string) bool) (Status, string) {
	if runtime.GOOS != "linux" {
		return Unknown, "not Linux"
	}
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return Unknown, "no /sys/class/drm"
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "card") || strings.Contains(e.Name(), "-") {
			continue
		}
		vendor := filepath.Join("/sys/class/drm", e.Name(), "device", "vendor")
		if data, err := os.ReadFile(vendor); err == nil {
			return Found, gpuVendor(strings.TrimSpace(string(data)))
		}
		return Found, "drm card present"
	}
	return Missing, "no drm card found"
}

func gpuVendor(id string) string {
	switch id {
	case "0x10de":
		return "NVIDIA"
	case "0x1002":
		return "AMD"
	case "0x8086":
		return "Intel"
	default:
		return "GPU " + id
	}
}

// qtWebEngineModules are where distros put the QtWebEngine QML module, which
// the KDE backend and the planned layer-shell view load.
var qtWebEngineModules = []string{
	"/usr/lib/qt6/qml/QtWebEngine",
	"/usr/lib64/qt6/qml/QtWebEngine",
	"/usr/lib/x86_64-linux-gnu/qt6/qml/QtWebEngine",
	"/usr/lib/aarch64-linux-gnu/qt6/qml/QtWebEngine",
}

func detectWebEngine(lookPath func(string) (string, error), exists func(string) bool, pkgCmd string) (Status, string, string) {
	for _, dir := range qtWebEngineModules {
		if exists(dir) {
			return Found, "Qt WebEngine (" + dir + ")", ""
		}
	}
	for _, browser := range []string{"chromium", "google-chrome-stable", "firefox"} {
		if _, err := lookPath(browser); err == nil {
			return Found, browser + " (fallback)", ""
		}
	}
	return Missing, "no Qt WebEngine or browser", pkgCmd
}

// detectBuildToolchain reports whether the native view launcher can be built:
// a C++ compiler, moc and pkg-config. It returns a human detail and no error.
func detectBuildToolchain(lookPath func(string) (string, error)) (bool, string) {
	compiler := ""
	for _, name := range []string{"c++", "g++", "clang++"} {
		if _, err := lookPath(name); err == nil {
			compiler = name
			break
		}
	}
	_, mocErr := lookPath("moc")
	_, pkgErr := lookPath("pkg-config")
	var missing []string
	if compiler == "" {
		missing = append(missing, "C++ compiler")
	}
	if mocErr != nil {
		missing = append(missing, "moc")
	}
	if pkgErr != nil {
		missing = append(missing, "pkg-config")
	}
	if len(missing) > 0 {
		return false, "missing " + strings.Join(missing, ", ")
	}
	return true, "can build the layer-shell view"
}

// toolchainPackages is the build toolchain package command per distro family.
var toolchainPackages = map[string]string{
	"arch":     "sudo pacman -S base-devel qt6-base",
	"debian":   "sudo apt install build-essential qt6-base-dev-tools pkg-config",
	"fedora":   "sudo dnf install gcc-c++ qt6-qtbase-devel pkgconf",
	"opensuse": "sudo zypper install gcc-c++ qt6-base-devel pkg-config",
	"alpine":   "sudo apk add build-base qt6-qtbase-dev pkgconf",
	"void":     "sudo xbps-install -S base-devel qt6-base-devel pkgconf",
}

// toolchainPackage maps a distro id, then its ID_LIKE family, to the toolchain
// install command.
func toolchainPackage(id, idLike string) string {
	for _, name := range append([]string{strings.ToLower(id)}, strings.Fields(strings.ToLower(idLike))...) {
		if family, ok := distroFamilies[name]; ok {
			return toolchainPackages[family]
		}
	}
	return ""
}

func detectSystemdUser(lookPath func(string) (string, error), exists func(string) bool) bool {
	if _, err := lookPath("systemctl"); err != nil {
		return false
	}
	// /run/user/<uid>/systemd exists when a systemd user manager is running.
	if uid := os.Getuid(); uid >= 0 {
		if exists(filepath.Join("/run/user", itoa(uid), "systemd")) {
			return true
		}
	}
	return false
}

func systemdDetail(on bool) string {
	if on {
		return "systemctl --user available"
	}
	return "no systemd user session; the XDG autostart entry is used"
}

func defaultNetwork() bool {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", "registry.npmjs.org:443")
	if err != nil {
		// A restricted network may still reach GitHub, which serves the 3.x players.
		conn, err = dialer.DialContext(ctx, "tcp", "raw.githubusercontent.com:443")
	}
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func networkDetail(on bool) string {
	if on {
		return "runtime download reachable"
	}
	return "offline; run with a cached runtime or --offline"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func foundOrMissing(ok bool) Status {
	if ok {
		return Found
	}
	return Missing
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// readOSRelease reads /etc/os-release. A missing file is an empty map.
func readOSRelease(reader func(string) (map[string]string, error)) map[string]string {
	if reader != nil {
		m, err := reader("/etc/os-release")
		if err == nil {
			return m
		}
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return out
}

// distroPackages is the install command for Qt WebEngine and layer-shell-qt
// per distro family, with each family's own package names.
var distroPackages = map[string]string{
	"arch":     "sudo pacman -S qt6-webengine layer-shell-qt",
	"debian":   "sudo apt install qml6-module-qtwebengine layer-shell-qt",
	"fedora":   "sudo dnf install qt6-qtwebengine layer-shell-qt",
	"opensuse": "sudo zypper install qt6-webengine layer-shell-qt6",
	"alpine":   "sudo apk add qt6-qtwebengine layer-shell-qt",
	"void":     "sudo xbps-install -S qt6-webengine layer-shell-qt",
}

// distroFamilies maps a distro id or ID_LIKE entry to its family.
var distroFamilies = map[string]string{
	"arch": "arch", "manjaro": "arch", "endeavouros": "arch",
	"debian": "debian", "ubuntu": "debian", "linuxmint": "debian", "pop": "debian",
	"fedora": "fedora", "rhel": "fedora", "centos": "fedora",
	"opensuse": "opensuse", "opensuse-tumbleweed": "opensuse", "opensuse-leap": "opensuse", "suse": "opensuse",
	"alpine": "alpine",
	"void":   "void",
}

// packageCmd maps a distro id, then its ID_LIKE family, to the full install
// command. An unknown distro gets no command rather than a wrong one.
func packageCmd(id, idLike string) string {
	for _, name := range append([]string{strings.ToLower(id)}, strings.Fields(strings.ToLower(idLike))...) {
		if family, ok := distroFamilies[name]; ok {
			return distroPackages[family]
		}
	}
	return ""
}
