package view

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// launcherPkgModules are the pkg-config modules the launcher links against. It
// matches launcherPkgs above; kept as a slice here so the build command and the
// detection cannot drift.
var launcherPkgModules = launcherPkgs

// Build compiles the launcher from the embedded source into dir and returns the
// binary path. The build needs moc and pkg-config for the Qt modules. It is
// idempotent: a binary newer than the source is kept.
func Build(dir string, logf func(string)) (string, error) {
	if logf == nil {
		logf = func(string) {}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src := filepath.Join(dir, "launcher.cpp")
	qml := filepath.Join(dir, "view.qml")
	qmlX11 := filepath.Join(dir, "view-x11.qml")
	bin := filepath.Join(dir, "spinewall-view")

	if err := writeIfChanged(src, LauncherSource); err != nil {
		return "", err
	}
	if err := writeIfChanged(qml, SurfaceQML); err != nil {
		return "", err
	}
	if err := writeIfChanged(qmlX11, SurfaceX11QML); err != nil {
		return "", err
	}
	if upToDate(bin, src) {
		logf("view launcher is up to date: " + bin)
		return bin, nil
	}

	cflags, err := pkgConfig("--cflags", launcherPkgModules)
	if err != nil {
		return "", err
	}
	ldflags, err := pkgConfig("--libs", launcherPkgModules)
	if err != nil {
		return "", err
	}
	compiler, err := findCompiler()
	if err != nil {
		return "", err
	}

	args := []string{"-O2", "-fPIC", "-o", bin, src}
	args = append(args, cflags...)
	args = append(args, ldflags...)
	cmd := exec.Command(compiler, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build the view launcher: %v\n%s", err, out.String())
	}
	logf("built the view launcher: " + bin)
	return bin, nil
}

// pkgConfig runs pkg-config and splits its output into arguments.
func pkgConfig(flag string, modules []string) ([]string, error) {
	args := append([]string{flag}, modules...)
	out, err := exec.Command("pkg-config", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("pkg-config %s %s: %w", flag, strings.Join(modules, " "), err)
	}
	return strings.Fields(string(out)), nil
}

// findCompiler returns the C++ compiler to use.
func findCompiler() (string, error) {
	for _, name := range []string{"c++", "g++", "clang++"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no C++ compiler found: install a C++ toolchain to build the view launcher")
}

// writeIfChanged writes data to path unless the bytes already match, so a
// rebuild is not triggered just by running setup again.
func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

// upToDate reports whether bin exists and is newer than src.
func upToDate(bin, src string) bool {
	bi, err := os.Stat(bin)
	if err != nil {
		return false
	}
	si, err := os.Stat(src)
	if err != nil {
		return false
	}
	return !si.ModTime().After(bi.ModTime())
}

// Monitor is one output the compositor reports.
type Monitor struct {
	Name     string
	Geometry string // WxH+X+Y as the launcher prints it
}

// ListMonitors asks the launcher for the compositor's outputs. It runs the
// built binary in list mode, so the answer matches how it will place windows.
func ListMonitors(bin string, env []string) ([]Monitor, error) {
	cmd := exec.Command(bin, "--list-screens")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	var monitors []Monitor
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		name, geom, ok := strings.Cut(line, "\t")
		if !ok {
			name, geom = "", line
		}
		monitors = append(monitors, Monitor{Name: name, Geometry: geom})
	}
	return monitors, nil
}

// ScreenKey turns a monitor geometry into the page's stage key, matching how
// the page derives it: WxH+X+Y. The launcher passes it to the page's URL.
func ScreenKey(m Monitor) string {
	return m.Geometry
}

// LaunchArgs builds the argv for one monitor's window. Backend is layer-shell
// or x11 and selects the QML surface the launcher loads.
func LaunchArgs(bin, pageURL, apiBase string, m Monitor, layer, backend string) []string {
	if backend == "" {
		backend = BackendLayerShell
	}
	args := []string{pageURL, "--api", apiBase, "--screen", ScreenKey(m), "--layer", layer, "--backend", backend}
	if m.Name != "" {
		args = append(args, "--output", m.Name)
	}
	if m.Geometry != "" {
		args = append(args, "--geometry", m.Geometry)
	}
	return args
}

// PageURL builds the URL the view loads: the server's page for this monitor's
// stage, plus a native-view flag so the page reports its mask and offers an
// edit toggle without the containment patch.
func PageURL(apiBase, screenKey string) string {
	url := strings.TrimSuffix(apiBase, "/") + "/index.html?view=native"
	if screenKey != "" {
		url += "&screen=" + queryEscape(screenKey)
	}
	return url
}

// queryEscape escapes a query value the way the page reads it back.
func queryEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
