package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testPaths builds a Paths rooted at a temp HOME so a test never touches the
// real home.
func testPaths(t *testing.T) Paths {
	t.Helper()
	home := t.TempDir()
	p := DefaultPaths()
	p.Home = home
	p.BinDir = filepath.Join(home, ".local", "bin")
	p.DataDir = filepath.Join(home, ".local", "share")
	p.StateDir = filepath.Join(home, ".local", "state", "spinewall")
	p.Config = filepath.Join(home, ".config", "spinewall", "config.json")
	p.CacheDir = filepath.Join(home, ".cache", "spinewall")
	p.Runtime = filepath.Join(p.CacheDir, "runtime")
	return p
}

// fakeBinary writes a stand-in executable so Install has bytes to copy.
func fakeBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spinewall")
	if err := os.WriteFile(path, []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstallRecordsEveryFile(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	res, err := Install(Options{Paths: paths, Executable: exe})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(res.Manifest.Files) == 0 {
		t.Fatal("manifest lists no files")
	}
	for _, e := range res.Manifest.Files {
		if _, err := os.Stat(e.Path); err != nil {
			t.Errorf("manifest entry %s not on disk: %v", e.Path, err)
		}
		hash, err := sha256File(e.Path)
		if err != nil {
			t.Fatal(err)
		}
		if hash != e.SHA256 {
			t.Errorf("%s: manifest hash %s, disk %s", e.Path, e.SHA256, hash)
		}
	}
	// The binary, theme, and autostart are all there.
	for _, want := range []string{
		filepath.Join(paths.BinDir, "spinewall"),
		filepath.Join(paths.ThemeDir(), "index.html"),
		filepath.Join(paths.ThemeDir(), "local-host.js"),
		filepath.Join(paths.ThemeDir(), "spinewall.theme"),
		paths.AutostartFile(),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected installed file %s: %v", want, err)
		}
	}
	if _, err := os.Stat(paths.ManifestPath()); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	first, err := Install(Options{Paths: paths, Executable: exe})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(Options{Paths: paths, Executable: exe})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Manifest.Files) != len(second.Manifest.Files) {
		t.Fatalf("file count changed: %d then %d", len(first.Manifest.Files), len(second.Manifest.Files))
	}
}

func TestAutostartRunsTheServer(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	if _, err := Install(Options{Paths: paths, Executable: exe}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.AutostartFile())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"[Desktop Entry]", "Type=Application", "Exec=" + filepath.Join(paths.BinDir, "spinewall") + " serve"} {
		if !strings.Contains(text, want) {
			t.Errorf("autostart missing %q:\n%s", want, text)
		}
	}
}

func TestSystemdUnitIsOptIn(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	if _, err := Install(Options{Paths: paths, Executable: exe}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.UnitFile()); err == nil {
		t.Fatal("systemd unit written without opt-in")
	}
	if _, err := Install(Options{Paths: paths, Executable: exe, SystemdUnit: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.UnitFile())
	if err != nil {
		t.Fatalf("unit not written when opted in: %v", err)
	}
	for _, want := range []string{"NoNewPrivileges=yes", "ProtectSystem=strict", "ExecStart=" + filepath.Join(paths.BinDir, "spinewall") + " serve"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("unit missing %q", want)
		}
	}
}

func TestUninstallRemovesExactlyTheManifest(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	if _, err := Install(Options{Paths: paths, Executable: exe}); err != nil {
		t.Fatal(err)
	}
	// A user file in the models dir must survive.
	model := filepath.Join(paths.ModelsDir(), "keep")
	if err := os.MkdirAll(model, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(model, "model.skel")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Uninstall(UninstallOptions{Paths: paths})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(res.RemovedFiles) == 0 {
		t.Fatal("nothing removed")
	}
	for _, f := range res.RemovedFiles {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("file still present: %s", f)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("model file removed by uninstall: %v", err)
	}
	// Directories the installer created and left empty are gone.
	if _, err := os.Stat(filepath.Join(paths.BinDir)); err == nil {
		t.Errorf("empty %s kept", paths.BinDir)
	}
}

func TestUninstallLeavesHomeIdenticalToPreInstall(t *testing.T) {
	home := t.TempDir()
	paths := DefaultPaths()
	paths.Home = home
	paths.BinDir = filepath.Join(home, ".local", "bin")
	paths.DataDir = filepath.Join(home, ".local", "share")
	paths.StateDir = filepath.Join(home, ".local", "state", "spinewall")
	paths.Config = filepath.Join(home, ".config", "spinewall", "config.json")
	paths.CacheDir = filepath.Join(home, ".cache", "spinewall")
	paths.Runtime = filepath.Join(paths.CacheDir, "runtime")

	before := snapshot(t, home)
	exe := fakeBinary(t)
	if _, err := Install(Options{Paths: paths, Executable: exe}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(UninstallOptions{Paths: paths}); err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, home)
	if !equalMap(before, after) {
		t.Fatalf("home changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestUnknownBackendIsRejected(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	_, err := Install(Options{Paths: paths, Executable: exe, Backend: "gnome-shell"})
	if err == nil {
		t.Fatal("an unknown backend installed without error")
	}
}

func TestLayerShellBackendWritesTheViewAndStartsWithIt(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	// BuildView is off, so the test needs no Qt toolchain: the source is written
	// and the first start builds it.
	res, err := Install(Options{Paths: paths, Executable: exe, Backend: BackendLayerShell})
	if err != nil {
		t.Fatalf("install layer-shell: %v", err)
	}
	src := filepath.Join(paths.ViewDir(), "launcher.cpp")
	qml := filepath.Join(paths.ViewDir(), "view.qml")
	for _, want := range []string{src, qml} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("view file %s not written: %v", want, err)
		}
	}
	if res.ThemeDir != paths.ViewDir() {
		t.Errorf("theme dir = %q, want the view dir", res.ThemeDir)
	}
	data, err := os.ReadFile(paths.AutostartFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "serve --with-view --view-backend layer-shell") {
		t.Errorf("autostart does not pin the layer-shell surface:\n%s", data)
	}
	if got := strings.Join(res.AutostartCommand, " "); !strings.Contains(string(data), "Exec="+res.BinPath+" "+got+"\n") {
		t.Errorf("result command %q is not the autostart Exec line:\n%s", got, data)
	}
	// The view files are in the manifest, so uninstall removes them.
	recorded := false
	for _, e := range res.Manifest.Files {
		if e.Path == src {
			recorded = true
		}
	}
	if !recorded {
		t.Error("the launcher source is not in the manifest")
	}
}

func TestX11BackendWritesBothSurfacesAndPinsIt(t *testing.T) {
	paths := testPaths(t)
	res, err := Install(Options{Paths: paths, Executable: fakeBinary(t), Backend: BackendX11})
	if err != nil {
		t.Fatalf("install x11: %v", err)
	}
	for _, name := range []string{"launcher.cpp", "view.qml", "view-x11.qml"} {
		if _, err := os.Stat(filepath.Join(paths.ViewDir(), name)); err != nil {
			t.Errorf("view file %s not written: %v", name, err)
		}
	}
	if res.ThemeDir != paths.ViewDir() {
		t.Errorf("theme dir = %q, want the view dir", res.ThemeDir)
	}
	data, err := os.ReadFile(paths.AutostartFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "serve --with-view --view-backend x11") {
		t.Errorf("autostart does not pin the X11 surface:\n%s", data)
	}
}

func TestLayerShellBackendNeverTouchesTheKDETheme(t *testing.T) {
	paths := testPaths(t)
	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t), Backend: BackendLayerShell}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.ThemeDir(), "index.html")); err == nil {
		t.Error("the layer-shell backend wrote the webwallpaper theme")
	}
}

// snapshot maps every path under root to a hash, so two snapshots can prove a
// home came back byte identical.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			out[rel+"/"] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = sha256Hex(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equalMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestModelDirIsResolvedToAnAbsolutePath(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	work := t.TempDir()
	t.Chdir(work)
	cases := map[string]string{
		"~/Spine":    filepath.Join(paths.Home, "Spine"),
		"rel/models": filepath.Join(work, "rel", "models"),
	}
	for typed, want := range cases {
		res, err := Install(Options{Paths: paths, Executable: exe, ModelDir: typed})
		if err != nil {
			t.Fatalf("%s: %v", typed, err)
		}
		if res.ModelDir != want {
			t.Errorf("%s: model dir = %q, want %q", typed, res.ModelDir, want)
		}
		if got := ReadConfig(paths.Config).AdditionalModelDirs; len(got) == 0 || got[0] != want {
			t.Errorf("%s: config dirs = %v, want %q first", typed, got, want)
		}
		if info, err := os.Stat(want); err != nil || !info.IsDir() {
			t.Errorf("%s: %s not created: %v", typed, want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "~")); err == nil {
		t.Error("a literal ~ directory was created")
	}
}

// writeLegacy lays out the old Python install in the throwaway home.
func writeLegacy(t *testing.T, paths Paths, withUnit bool) {
	t.Helper()
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyServer(), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if withUnit {
		if err := os.MkdirAll(filepath.Dir(paths.UnitFile()), 0o755); err != nil {
			t.Fatal(err)
		}
		unit := "[Service]\nExecStart=%h/.local/bin/spinewall-server\n"
		if err := os.WriteFile(paths.UnitFile(), []byte(unit), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyInstallHoldsBackAutostart(t *testing.T) {
	paths := testPaths(t)
	writeLegacy(t, paths, true)
	res, err := Install(Options{Paths: paths, Executable: fakeBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Legacy) != 2 {
		t.Errorf("legacy = %v, want the server and the unit", res.Legacy)
	}
	if res.Autostart != "" {
		t.Errorf("autostart reported as %q", res.Autostart)
	}
	if _, err := os.Stat(paths.AutostartFile()); err == nil {
		t.Error("autostart written next to the old Python install")
	}

	res, err = Install(Options{Paths: paths, Executable: fakeBinary(t), AllowLegacy: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.AutostartFile()); err != nil || res.Autostart == "" {
		t.Errorf("confirmed install wrote no autostart: %v", err)
	}
}

func TestLegacyUnitIsNeverOverwritten(t *testing.T) {
	paths := testPaths(t)
	writeLegacy(t, paths, true)
	before, _ := os.ReadFile(paths.UnitFile())
	for _, allow := range []bool{false, true} {
		_, err := Install(Options{Paths: paths, Executable: fakeBinary(t), SystemdUnit: true, AllowLegacy: allow})
		if allow && (err == nil || !strings.Contains(err.Error(), "uninstall --legacy")) {
			t.Errorf("err = %v, want a pointer to uninstall --legacy", err)
		}
		if !allow && err != nil {
			t.Errorf("held-back install failed: %v", err)
		}
		after, _ := os.ReadFile(paths.UnitFile())
		if string(after) != string(before) {
			t.Fatalf("allow=%v: legacy unit overwritten:\n%s", allow, after)
		}
	}
}

func TestNoLegacyInAFreshHome(t *testing.T) {
	paths := testPaths(t)
	if found := DetectLegacy(paths); len(found) != 0 {
		t.Errorf("legacy found in an empty home: %v", found)
	}
	// The new unit runs `spinewall serve`, so it is not the legacy one.
	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t), SystemdUnit: true}); err != nil {
		t.Fatal(err)
	}
	if found := DetectLegacy(paths); len(found) != 0 {
		t.Errorf("the new unit was taken for the legacy one: %v", found)
	}
}

func TestLegacyUninstallWithoutAMarkerKeepsTheNewInstall(t *testing.T) {
	paths := testPaths(t)
	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t), SystemdUnit: true}); err != nil {
		t.Fatal(err)
	}
	if err := LegacyUninstall(paths, nil); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{paths.UnitFile(), filepath.Join(paths.ThemeDir(), "index.html")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("legacy uninstall removed the new install's %s: %v", kept, err)
		}
	}
}

func TestLegacyUninstallNeverRemovesANewGoUnit(t *testing.T) {
	paths := testPaths(t)
	writeLegacy(t, paths, false)
	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t), SystemdUnit: true, AllowLegacy: true}); err != nil {
		t.Fatal(err)
	}
	if err := LegacyUninstall(paths, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.LegacyServer()); err == nil {
		t.Error("legacy server kept")
	}
	if _, err := os.Stat(paths.UnitFile()); err != nil {
		t.Errorf("legacy uninstall removed the new Go unit: %v", err)
	}
}

func TestUninstallLegacyRemovesTheWholeLegacyInstall(t *testing.T) {
	paths := testPaths(t)
	writeLegacy(t, paths, true)
	if err := os.MkdirAll(paths.ThemeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "local-host.js", "spinewall.theme", "spine40.js"} {
		if err := os.WriteFile(filepath.Join(paths.ThemeDir(), name), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inst, err := Install(Options{Paths: paths, Executable: fakeBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	preexisting := 0
	for _, e := range inst.Manifest.Files {
		if !e.Created {
			preexisting++
		}
	}
	if preexisting != 3 {
		t.Fatalf("install recorded %d pre-existing files, want the 3 legacy theme files", preexisting)
	}

	if err := LegacyUninstall(paths, nil); err != nil {
		t.Fatal(err)
	}
	res, err := Uninstall(UninstallOptions{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{paths.ThemeDir(), paths.LegacyServer(), paths.UnitFile()} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s left behind", gone)
		}
	}
	for _, kept := range res.KeptFiles {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("reported %s as kept, but it was deleted", kept)
		}
	}
	if len(res.KeptFiles) != 0 {
		t.Errorf("kept = %v, want none", res.KeptFiles)
	}
}
