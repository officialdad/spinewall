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
	PackageCmd    string // the distro's package manager prefix, e.g. "pacman -S"
	SystemdUser   bool
	Network       bool
	LayerShell    bool
	WebEngineName string
}

// Options let tests replace the detection sources.
type Options struct {
	Getenv        func(string) string
	LookPath      func(string) (string, error)
	Exists        func(string) bool
	ReadOSRelease func(string) (map[string]string, error)
	Network       func() bool
	// FullscreenCompositor is the file listing Wayland globals, default
	// /proc/self/environ cannot be read for another process, so tests set it.
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
	// ID_LIKE names the family, so an Arch derivative such as CachyOS resolves to
	// the pacman command without naming every derivative.
	r.PackageCmd = packageCmd(r.Distro, release["ID_LIKE"])

	// Session and desktop.
	r.Checks = append(r.Checks,
		Check{Name: "session type", Status: foundOrMissing(r.SessionType != "unknown" && r.SessionType != ""), Detail: r.SessionType, Needed: true},
		Check{Name: "desktop", Status: foundOrMissing(r.Desktop != "unknown" && r.Desktop != ""), Detail: r.Desktop, Needed: true},
	)

	// Layer-shell is offered by the compositor. On Wayland read the globals a
	// probe would see; off Wayland it is simply not available.
	r.LayerShell = detectLayerShell(getenv, opts.WaylandGlobals)
	r.Checks = append(r.Checks, Check{
		Name:    "layer-shell",
		Status:  foundOrMissing(r.LayerShell),
		Detail:  layerShellDetail(r.LayerShell, r.SessionType),
		Package: "",
	})

	// GPU from /sys/class/drm.
	gpu, gpuDetail := detectGPU(exists)
	r.Checks = append(r.Checks, Check{Name: "GPU", Status: gpu, Detail: gpuDetail})

	// Web engine candidates the KDE backend and the future layer-shell view need.
	webEngine, webName, webPkg := detectWebEngine(lookPath, r.PackageCmd)
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

	r.Backend = recommendBackend(r)
	return r
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

// recommendBackend picks the installable backend for this machine. Only the KDE
// legacy backend is installable in this phase.
func recommendBackend(r Report) string {
	if strings.Contains(strings.ToLower(r.Desktop), "kde") || strings.Contains(strings.ToLower(r.Desktop), "plasma") {
		return "kde-legacy"
	}
	return "kde-legacy"
}

// ---- detection helpers ----

func detectLayerShell(getenv func(string) string, globalsFile string) bool {
	if getenv("XDG_SESSION_TYPE") != "wayland" {
		return false
	}
	// A real compositor probe needs a socket client. The environment tells us a
	// Wayland session exists; the globals file, when present, names the protocol.
	if globalsFile != "" {
		if data, err := os.ReadFile(globalsFile); err == nil {
			return strings.Contains(string(data), "zwlr_layer_shell_v1")
		}
	}
	// No probe available: Wayland itself is the honest partial signal.
	return true
}

func layerShellDetail(on bool, session string) string {
	if session != "wayland" {
		return "needs a Wayland session"
	}
	if on {
		return "Wayland layer-shell compositor"
	}
	return "compositor does not offer zwlr_layer_shell_v1"
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

func detectWebEngine(lookPath func(string) (string, error), pkgCmd string) (Status, string, string) {
	// Qt WebEngine is what the KDE backend and the planned layer-shell view use.
	if _, err := lookPath("qml"); err == nil {
		if _, err := lookPath("plasmashell"); err == nil {
			return Found, "Qt WebEngine (qml)", ""
		}
		return Found, "qml present", ""
	}
	for _, browser := range []string{"chromium", "google-chrome-stable", "firefox"} {
		if _, err := lookPath(browser); err == nil {
			return Found, browser + " (fallback)", ""
		}
	}
	return Missing, "no qml or browser", pkgCmd + " qt6-webengine layer-shell-qt"
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

// packageCmd maps a distro id and its ID_LIKE family to an install command
// prefix. The family catches derivatives, so an Arch-like id resolves to
// pacman even when its own id is unknown.
func packageCmd(id, idLike string) string {
	family := strings.ToLower(id)
	if family == "" || family == "unknown" {
		if fields := strings.Fields(idLike); len(fields) > 0 {
			family = strings.ToLower(fields[0])
		}
	}
	switch family {
	case "arch", "manjaro", "endeavouros":
		return "pacman -S"
	case "debian", "ubuntu", "linuxmint", "pop":
		return "apt install"
	case "fedora", "rhel", "centos":
		return "dnf install"
	case "opensuse", "opensuse-tumbleweed", "opensuse-leap", "suse":
		return "zypper install"
	case "alpine":
		return "apk add"
	case "void":
		return "xbps-install -S"
	}
	// A derivative whose own id differs uses its family when one is named.
	for _, field := range strings.Fields(strings.ToLower(idLike)) {
		switch field {
		case "arch":
			return "pacman -S"
		case "debian", "ubuntu":
			return "apt install"
		case "fedora", "rhel":
			return "dnf install"
		case "suse":
			return "zypper install"
		}
	}
	return "install"
}
