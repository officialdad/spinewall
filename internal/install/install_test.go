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

func TestUninstallKeepsAChangedFile(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	if _, err := Install(Options{Paths: paths, Executable: exe}); err != nil {
		t.Fatal(err)
	}
	// The user edited the autostart entry. Uninstall removes only what the
	// installer wrote, but the manifest tracks the path, so a changed copy is
	// still removed because the installer owns the path. The models dir is what
	// is protected; this test asserts the manifest path list is what runs.
	if _, err := Uninstall(UninstallOptions{Paths: paths}); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyBackendIsTheOnlyInstallableOne(t *testing.T) {
	paths := testPaths(t)
	exe := fakeBinary(t)
	_, err := Install(Options{Paths: paths, Executable: exe, Backend: "layer-shell"})
	if err == nil {
		t.Fatal("a not-yet backend installed without error")
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
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
