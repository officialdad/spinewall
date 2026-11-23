package doctor

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// fakeEnv builds Options from a map so a test controls every detection source.
func fakeEnv(env map[string]string, present map[string]bool, network bool) Options {
	return Options{
		Getenv: func(k string) string { return env[k] },
		LookPath: func(name string) (string, error) {
			if present[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errNotFound
		},
		Exists: func(path string) bool { return present[path] },
		ReadOSRelease: func(string) (map[string]string, error) {
			m := map[string]string{"ID": env["DISTRO_ID"]}
			if idLike, ok := env["DISTRO_ID_LIKE"]; ok {
				m["ID_LIKE"] = idLike
			}
			return m, nil
		},
		Network: func() bool { return network },
		HasPkg:  func([]string) bool { return present["pkg:qt-dev"] },
	}
}

type notFoundError struct{}

func (notFoundError) Error() string { return "not found" }

var errNotFound = notFoundError{}

func TestDetectNamesAgnosticSessionAndDesktop(t *testing.T) {
	opts := fakeEnv(map[string]string{
		"XDG_SESSION_TYPE":    "wayland",
		"XDG_CURRENT_DESKTOP": "KDE",
		"DISTRO_ID":           "arch",
	}, map[string]bool{"/usr/lib/qt6/qml/QtWebEngine": true, "plasmashell": true, "systemctl": true, fmt.Sprintf("/run/user/%d/systemd", os.Getuid()): true}, true)
	r := Detect(opts)
	if r.SessionType != "wayland" {
		t.Errorf("session = %q", r.SessionType)
	}
	if r.Desktop != "KDE" {
		t.Errorf("desktop = %q", r.Desktop)
	}
	if r.Distro != "arch" {
		t.Errorf("distro = %q", r.Distro)
	}
	if !r.SystemdUser {
		t.Error("systemd user session not detected")
	}
	if !r.Network {
		t.Error("network not detected")
	}
	if r.PackageCmd != "sudo pacman -S qt6-webengine layer-shell-qt" {
		t.Errorf("package cmd = %q", r.PackageCmd)
	}
	if web := checkNamed(t, r, "web engine"); web.Status != Found || !strings.Contains(web.Detail, "QtWebEngine") {
		t.Errorf("web engine = %+v", web)
	}
}

func TestMissingWebEngineNamesThePackage(t *testing.T) {
	opts := fakeEnv(map[string]string{
		"XDG_SESSION_TYPE":    "x11",
		"XDG_CURRENT_DESKTOP": "XFCE",
		"DISTRO_ID":           "debian",
	}, map[string]bool{}, false)
	r := Detect(opts)
	var webCheck *Check
	for i := range r.Checks {
		if r.Checks[i].Name == "web engine" {
			webCheck = &r.Checks[i]
		}
	}
	if webCheck == nil {
		t.Fatal("no web engine check")
	}
	if webCheck.Status != Missing {
		t.Errorf("web engine status = %q, want missing", webCheck.Status)
	}
	if webCheck.Package != "sudo apt install qml6-module-qtwebengine layer-shell-qt" {
		t.Errorf("package hint %q is not the Debian command", webCheck.Package)
	}
}

func TestPrintNeverRunsAPackageManager(t *testing.T) {
	// Print only formats. It takes a writer and returns; the test asserts it
	// wrote a package hint rather than executing one.
	opts := fakeEnv(map[string]string{"DISTRO_ID": "fedora"}, map[string]bool{}, false)
	r := Detect(opts)
	var sb strings.Builder
	Print(&sb, r)
	out := sb.String()
	if !strings.Contains(out, "check") || !strings.Contains(out, "status") {
		t.Errorf("no table header:\n%s", out)
	}
	if !strings.Contains(out, "spinewall never runs a package manager") {
		t.Errorf("missing the never-run note:\n%s", out)
	}
}

func TestOfflineIsReported(t *testing.T) {
	opts := fakeEnv(map[string]string{"DISTRO_ID": "arch"}, map[string]bool{}, false)
	r := Detect(opts)
	if r.Network {
		t.Error("network true when dial fails")
	}
	var sb strings.Builder
	Print(&sb, r)
	if !strings.Contains(sb.String(), "offline") {
		t.Errorf("offline not reported:\n%s", sb.String())
	}
}

func TestArchFamilyResolvesToPacman(t *testing.T) {
	// A derivative whose own id is unknown resolves through ID_LIKE, which is how
	// an Arch-based distro gets the pacman command without naming it.
	opts := fakeEnv(map[string]string{
		"DISTRO_ID":      "somearchclone",
		"DISTRO_ID_LIKE": "arch",
	}, map[string]bool{}, true)
	r := Detect(opts)
	if !strings.HasPrefix(r.PackageCmd, "sudo pacman -S ") {
		t.Errorf("package cmd = %q, want the pacman command", r.PackageCmd)
	}
}

func TestRecommendBackendIsKdeLegacy(t *testing.T) {
	opts := fakeEnv(map[string]string{"XDG_CURRENT_DESKTOP": "GNOME"}, map[string]bool{}, true)
	r := Detect(opts)
	if r.Backend != "kde-legacy" {
		t.Errorf("backend = %q", r.Backend)
	}
}

// readyNative is a Plasma Wayland box with everything the native view needs.
func readyNative() (map[string]string, map[string]bool) {
	env := map[string]string{"XDG_SESSION_TYPE": "wayland", "XDG_CURRENT_DESKTOP": "KDE", "DISTRO_ID": "debian"}
	present := map[string]bool{
		"c++": true, "moc": true, "pkg-config": true, "pkg:qt-dev": true,
		"/usr/lib/qt6/qml/QtWebEngine":        true,
		"/usr/lib/qt6/qml/org/kde/layershell": true,
	}
	return env, present
}

func TestRecommendLayerShellWhenTheViewCanBuildAndLoad(t *testing.T) {
	env, present := readyNative()
	if r := Detect(fakeEnv(env, present, true)); r.Backend != "layer-shell" {
		t.Errorf("backend = %q, checks %+v", r.Backend, r.Checks)
	}
}

func TestDesktopSessionAloneAgreesWithTheRecommendation(t *testing.T) {
	env, present := readyNative()
	delete(env, "XDG_CURRENT_DESKTOP")
	env["DESKTOP_SESSION"] = "sway"
	r := Detect(fakeEnv(env, present, true))
	if got := checkNamed(t, r, "layer-shell").Status; got != Found {
		t.Errorf("layer-shell row = %q, want found", got)
	}
	if r.Backend != "layer-shell" {
		t.Errorf("backend = %q, want layer-shell to match the row", r.Backend)
	}
}

func TestNoLayerShellWithoutTheQtDevelopmentModules(t *testing.T) {
	// The tools are on PATH but pkg-config cannot resolve Qt6WebEngineQuick, so
	// setup's build would fail.
	env, present := readyNative()
	delete(present, "pkg:qt-dev")
	r := Detect(fakeEnv(env, present, true))
	if r.Backend != "kde-legacy" {
		t.Errorf("backend = %q, want kde-legacy", r.Backend)
	}
	c := checkNamed(t, r, "view build toolchain")
	if c.Status != Missing || !strings.Contains(c.Detail, "Qt development modules") {
		t.Errorf("toolchain check = %+v", c)
	}
}

func TestNoLayerShellWithoutTheLayerShellQMLModule(t *testing.T) {
	env, present := readyNative()
	delete(present, "/usr/lib/qt6/qml/org/kde/layershell")
	r := Detect(fakeEnv(env, present, true))
	if r.Backend != "kde-legacy" {
		t.Errorf("backend = %q, want kde-legacy", r.Backend)
	}
	if c := checkNamed(t, r, "layer-shell QML module"); c.Status != Missing || !strings.Contains(c.Package, "layer-shell-qt") {
		t.Errorf("QML module check = %+v", c)
	}
}

func checkNamed(t *testing.T, r Report, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check", name)
	return Check{}
}

func TestPackageCommandUsesEachFamilysNames(t *testing.T) {
	cases := map[string]string{
		"arch":                "sudo pacman -S qt6-webengine layer-shell-qt",
		"ubuntu":              "sudo apt install qml6-module-qtwebengine layer-shell-qt",
		"fedora":              "sudo dnf install qt6-qtwebengine layer-shell-qt",
		"opensuse-tumbleweed": "sudo zypper install qt6-webengine layer-shell-qt6",
	}
	for id, want := range cases {
		r := Detect(fakeEnv(map[string]string{"DISTRO_ID": id}, map[string]bool{}, true))
		if got := checkNamed(t, r, "web engine").Package; got != want {
			t.Errorf("%s: package = %q, want %q", id, got, want)
		}
	}
}

func TestUnknownDistroPrintsNoPackageCommand(t *testing.T) {
	r := Detect(fakeEnv(map[string]string{"DISTRO_ID": "somethingelse"}, map[string]bool{}, true))
	if r.PackageCmd != "" {
		t.Errorf("package cmd = %q, want none", r.PackageCmd)
	}
	var sb strings.Builder
	Print(&sb, r)
	if strings.Contains(sb.String(), "To install what is missing") {
		t.Errorf("printed a package command for an unknown distro:\n%s", sb.String())
	}
}

func TestAQmlBinaryIsNotQtWebEngine(t *testing.T) {
	r := Detect(fakeEnv(map[string]string{"DISTRO_ID": "arch"}, map[string]bool{"qml": true}, true))
	if web := checkNamed(t, r, "web engine"); web.Status != Missing {
		t.Errorf("qml alone reported web engine %+v", web)
	}
}

func TestLayerShellWithoutAProbe(t *testing.T) {
	cases := []struct {
		session, desktop string
		want             Status
	}{
		{"wayland", "GNOME", Missing},
		{"wayland", "ubuntu:GNOME", Missing},
		{"wayland", "KDE", Found},
		{"wayland", "sway", Found},
		{"wayland", "Hyprland", Found},
		{"wayland", "river", Found},
		{"wayland", "SomeNewCompositor", Unknown},
		{"x11", "KDE", Missing},
	}
	for _, c := range cases {
		r := Detect(fakeEnv(map[string]string{"XDG_SESSION_TYPE": c.session, "XDG_CURRENT_DESKTOP": c.desktop}, map[string]bool{}, true))
		if got := checkNamed(t, r, "layer-shell").Status; got != c.want {
			t.Errorf("%s on %s: layer-shell = %q, want %q", c.desktop, c.session, got, c.want)
		}
	}
}

func TestLayerShellReadsTheGlobalsProbe(t *testing.T) {
	globals := t.TempDir() + "/globals"
	if err := os.WriteFile(globals, []byte("interface: 'wl_compositor'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := fakeEnv(map[string]string{"XDG_SESSION_TYPE": "wayland", "XDG_CURRENT_DESKTOP": "KDE"}, map[string]bool{}, true)
	opts.WaylandGlobals = globals
	if got := checkNamed(t, Detect(opts), "layer-shell").Status; got != Missing {
		t.Errorf("layer-shell = %q, want missing without zwlr_layer_shell_v1", got)
	}
}
