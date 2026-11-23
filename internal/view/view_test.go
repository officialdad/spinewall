package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeOpts(session, desktop string, present map[string]bool) Options {
	return Options{
		Getenv: func(k string) string {
			switch k {
			case "XDG_SESSION_TYPE":
				return session
			case "XDG_CURRENT_DESKTOP":
				return desktop
			}
			return ""
		},
		LookPath: func(name string) (string, error) {
			if present[name] {
				return "/usr/bin/" + name, nil
			}
			return "", os.ErrNotExist
		},
		Exists: func(path string) bool { return present[path] },
		// Keep the test off the real box: pkg-config presence is controlled here.
		HasPkg: func(pkgs []string) bool { return present["pkg:"+pkgs[0]] },
	}
}

func TestDetectIsReadyWhenProven(t *testing.T) {
	opts := fakeOpts("wayland", "KDE", map[string]bool{
		"c++":                                 true,
		"moc":                                 true,
		"pkg-config":                          true,
		"/usr/lib/qt6/qml/QtWebEngine":        true,
		"/usr/lib/qt6/qml/org/kde/layershell": true,
		"pkg:Qt6Quick":                        true,
	})
	r := Detect(opts)
	if !r.Ready() {
		t.Errorf("not ready: %+v", r)
	}
	if !r.LayerShell || !r.WebEngine || !r.LayerShellQML || !r.Buildable {
		t.Errorf("detect = %+v", r)
	}
	if len(r.BuildMissing) != 0 {
		t.Errorf("build pieces missing on a ready box: %v", r.BuildMissing)
	}
}

func TestDetectWithoutACompilerIsNotReady(t *testing.T) {
	// No compiler, so the launcher cannot build, and the reason names the
	// missing piece.
	opts := fakeOpts("wayland", "KDE", map[string]bool{
		"/usr/lib/qt6/qml/QtWebEngine":        true,
		"/usr/lib/qt6/qml/org/kde/layershell": true,
	})
	r := Detect(opts)
	if r.Ready() {
		t.Error("ready with no compiler")
	}
	if r.Buildable {
		t.Error("buildable with no compiler")
	}
	assertReason(t, r.BuildMissing, "C++ compiler")
}

func TestDetectGNOMENeedsNoLayerShell(t *testing.T) {
	opts := fakeOpts("wayland", "GNOME", map[string]bool{
		"c++": true, "moc": true, "pkg-config": true,
		"/usr/lib/qt6/qml/QtWebEngine": true, "pkg:Qt6Quick": true,
	})
	r := Detect(opts)
	if r.LayerShell {
		t.Error("GNOME reported as layer-shell")
	}
	if r.Ready() {
		t.Error("the native view is ready on GNOME, which has no layer-shell")
	}
}

func TestDetectX11HasNoLayerShell(t *testing.T) {
	opts := fakeOpts("x11", "KDE", map[string]bool{
		"c++": true, "moc": true, "pkg-config": true,
		"/usr/lib/qt6/qml/QtWebEngine": true, "pkg:Qt6Quick": true,
	})
	if Detect(opts).LayerShell {
		t.Error("X11 session reported layer-shell")
	}
}

func TestSessionBackendPicksX11OnARealX11Session(t *testing.T) {
	cases := map[string]string{
		"x11":     BackendX11,
		"wayland": BackendLayerShell,
		"tty":     BackendLayerShell,
	}
	for session, want := range cases {
		got := SessionBackend(func(key string) string {
			if key == "XDG_SESSION_TYPE" {
				return session
			}
			return ""
		})
		if got != want {
			t.Errorf("%s session: backend = %q, want %q", session, got, want)
		}
	}
}

func TestReadyX11NeedsNoLayerShellQML(t *testing.T) {
	// A real X11 session cannot offer layer-shell, so the X11 backend must be
	// ready with the same launcher and web engine but no layer-shell module.
	opts := fakeOpts("x11", "XFCE", map[string]bool{
		"c++": true, "moc": true, "pkg-config": true,
		"/usr/lib/qt6/qml/QtWebEngine": true, "pkg:Qt6Quick": true,
	})
	r := Detect(opts)
	if !r.X11 {
		t.Error("X11 session not detected")
	}
	if r.Ready() {
		t.Error("layer-shell reported ready on X11")
	}
	if !r.ReadyX11() {
		t.Errorf("X11 backend not ready: %+v", r)
	}
}

func TestBrowserCommandRunsOnItsOwnProfile(t *testing.T) {
	cases := map[string][]string{
		"firefox":  {"--new-instance", "--profile", "/p/kiosk", "--kiosk", "http://x/index.html"},
		"chromium": {"--user-data-dir=/p/kiosk", "--kiosk", "http://x/index.html"},
	}
	for browser, want := range cases {
		got, err := BrowserCommand("http://x/index.html", "/p/kiosk", func(name string) (string, error) {
			if name == browser {
				return "/usr/bin/" + name, nil
			}
			return "", os.ErrNotExist
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != browser {
			t.Errorf("browser = %q, want %s", got.Name, browser)
		}
		if strings.Join(got.Args, " ") != strings.Join(want, " ") {
			t.Errorf("%s args = %v, want %v", browser, got.Args, want)
		}
	}
}

func TestBrowserCommandPicksTheFirstInstalled(t *testing.T) {
	have := map[string]bool{"firefox": true, "google-chrome": true}
	lookPath := func(name string) (string, error) {
		if have[name] {
			return "/usr/bin/" + name, nil
		}
		return "", os.ErrNotExist
	}
	got, err := BrowserCommand("http://x", "/p", lookPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "google-chrome" || FindBrowser(lookPath) != got.Name {
		t.Errorf("browser = %q, FindBrowser = %q, want google-chrome", got.Name, FindBrowser(lookPath))
	}
}

func TestBrowserCommandWithoutABrowserIsAnError(t *testing.T) {
	_, err := BrowserCommand("http://x", "/p", func(string) (string, error) { return "", os.ErrNotExist })
	if err == nil {
		t.Fatal("no browser reported as success")
	}
}

func TestDetectReadsTheGlobalsProbe(t *testing.T) {
	file := filepath.Join(t.TempDir(), "globals")
	if err := os.WriteFile(file, []byte("interface: 'zwlr_layer_shell_v1'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := fakeOpts("wayland", "SomeNewCompositor", map[string]bool{
		"c++": true, "moc": true, "pkg-config": true,
		"/usr/lib/qt6/qml/QtWebEngine": true, "pkg:Qt6Quick": true,
	})
	opts.WaylandGlobals = file
	r := Detect(opts)
	if !r.LayerShell {
		t.Error("layer-shell not read from the probe file")
	}
}

func TestDetectWithoutTheLayerShellQMLModuleIsNotReady(t *testing.T) {
	// view.qml imports org.kde.layershell, so without layer-shell-qt the window
	// cannot load even when the launcher builds.
	opts := fakeOpts("wayland", "KDE", map[string]bool{
		"c++": true, "moc": true, "pkg-config": true,
		"/usr/lib/qt6/qml/QtWebEngine": true, "pkg:Qt6Quick": true,
	})
	r := Detect(opts)
	if !r.Buildable || r.LayerShellQML || r.Ready() {
		t.Errorf("detect = %+v", r)
	}
}

func TestPageURLNamesTheMonitorAndTheNativeView(t *testing.T) {
	got := PageURL("http://127.0.0.1:8777", "3440x1440+0+0")
	want := "http://127.0.0.1:8777/index.html?view=native&screen=3440x1440%2B0%2B0"
	if got != want {
		t.Errorf("PageURL = %q, want %q", got, want)
	}
	// No screen key is the default stage, still with the native flag.
	got = PageURL("http://127.0.0.1:8777/", "")
	want = "http://127.0.0.1:8777/index.html?view=native"
	if got != want {
		t.Errorf("PageURL = %q, want %q", got, want)
	}
}

func TestLaunchArgsCarryTheMonitor(t *testing.T) {
	args := LaunchArgs("/bin/spinewall-view", "http://x/index.html?view=native&screen=k", "http://x", Monitor{Name: "DP-2", Geometry: "3440x1440+0+0"}, "bottom", BackendLayerShell)
	joined := joinArgs(args)
	for _, want := range []string{
		"http://x/index.html?view=native&screen=k",
		"--api http://x",
		"--screen 3440x1440+0+0",
		"--layer bottom",
		"--backend layer-shell",
		"--output DP-2",
		"--geometry 3440x1440+0+0",
	} {
		if !contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
}

func TestScreenKeyMatchesGeometry(t *testing.T) {
	if got := ScreenKey(Monitor{Geometry: "1920x1080+3440+236"}); got != "1920x1080+3440+236" {
		t.Errorf("ScreenKey = %q", got)
	}
}

func TestUpToDateOnlyWhenBinaryIsNewer(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if upToDate(bin, src) {
		t.Error("missing binary reported up to date")
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Same second: the source is not after the binary, so it is up to date.
	if !upToDate(bin, src) {
		t.Error("equal-time binary not up to date")
	}
}

func TestWriteIfChangedSkipsSameBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := writeIfChanged(path, []byte("a")); err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(path)
	if err := writeIfChanged(path, []byte("a")); err != nil {
		t.Fatal(err)
	}
	info2, _ := os.Stat(path)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("identical bytes rewrote the file")
	}
	if err := writeIfChanged(path, []byte("b")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "b" {
		t.Errorf("changed bytes not written: %q", data)
	}
}

func assertReason(t *testing.T, reasons []string, want string) {
	t.Helper()
	for _, r := range reasons {
		if contains(r, want) {
			return
		}
	}
	t.Errorf("reasons %v do not name %q", reasons, want)
}

func joinArgs(args []string) string {
	out := ""
	for _, a := range args {
		out += a + " "
	}
	return out
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
