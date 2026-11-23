package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/officialdad/spinewall/internal/doctor"
	"github.com/officialdad/spinewall/internal/install"
	spanruntime "github.com/officialdad/spinewall/internal/runtime"
)

// homePaths builds install.Paths under a temp HOME so a test never touches the
// real home.
func homePaths(t *testing.T) install.Paths {
	t.Helper()
	home := t.TempDir()
	p := install.DefaultPaths()
	p.Home = home
	p.BinDir = filepath.Join(home, ".local", "bin")
	p.DataDir = filepath.Join(home, ".local", "share")
	p.StateDir = filepath.Join(home, ".local", "state", "spinewall")
	p.Config = filepath.Join(home, ".config", "spinewall", "config.json")
	p.CacheDir = filepath.Join(home, ".cache", "spinewall")
	p.Runtime = filepath.Join(p.CacheDir, "runtime")
	return p
}

// freePort finds a port nothing is listening on, so the verify step starts a
// server that does not collide with the real wallpaper.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// offlineDetect is a doctor with no network, so the fetch step is skipped and
// the run never reaches out.
func offlineDetect() doctor.Options {
	return doctor.Options{
		Getenv:  func(k string) string { return os.Getenv(k) },
		Network: func() bool { return false },
	}
}

// TestSetupYesInstallsAndUninstallRestoresHome runs the non-interactive path,
// checks the manifest, then proves uninstall returns the home to its start.
func TestSetupYesInstallsAndUninstallRestoresHome(t *testing.T) {
	paths := homePaths(t)
	port := freePort(t)
	t.Setenv("SPINEWALL_PORT", port)

	before := snapshotTree(t, paths.Home)

	exe := buildTestBinary(t)
	res, err := Run(Options{
		Paths:      paths,
		Yes:        true,
		Offline:    true,
		Executable: exe,
		Doctor:     offlineDetect(),
		Runtime:    spanruntime.New(spanruntime.Options{Dir: paths.Runtime, Offline: true}),
	})
	if err != nil {
		t.Fatalf("setup --yes: %v", err)
	}
	if !res.Verified {
		t.Errorf("server was not verified: %+v", res)
	}
	if res.Models != 0 {
		t.Errorf("expected zero models in an empty home, got %d", res.Models)
	}
	if !res.AddModelFolder {
		t.Error("zero models did not offer adding a folder")
	}

	manifest := readManifest(t, paths.ManifestPath())
	if len(manifest.Files) == 0 {
		t.Fatal("manifest lists no files")
	}
	for _, e := range manifest.Files {
		if _, err := os.Stat(e.Path); err != nil {
			t.Errorf("manifest entry %s not on disk: %v", e.Path, err)
		}
	}

	// Stop the server the verify step started before uninstalling.
	if err := install.StopServer(paths, func(string) {}); err != nil {
		t.Fatalf("stop server: %v", err)
	}

	if _, err := install.Uninstall(install.UninstallOptions{
		Paths:       paths,
		StopProcess: true,
		RemoveState: true,
		RemoveCache: true,
	}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	after := snapshotTree(t, paths.Home)
	if !sameTree(before, after) {
		t.Fatalf("home changed after uninstall:\nbefore %v\nafter  %v", before, after)
	}
}

// TestSetupModelDirIsRecorded checks the models folder choice lands in the config.
func TestSetupModelDirIsRecorded(t *testing.T) {
	paths := homePaths(t)
	port := freePort(t)
	t.Setenv("SPINEWALL_PORT", port)
	extra := filepath.Join(paths.Home, "models-here")

	_, err := Run(Options{
		Paths:      paths,
		Yes:        true,
		Offline:    true,
		Executable: buildTestBinary(t),
		ModelDir:   extra,
		Doctor:     offlineDetect(),
		Runtime:    spanruntime.New(spanruntime.Options{Dir: paths.Runtime, Offline: true}),
	})
	if err != nil {
		t.Fatalf("setup --yes: %v", err)
	}
	defer install.StopServer(paths, func(string) {})

	cfg := install.ReadConfig(paths.Config)
	found := false
	for _, d := range cfg.AdditionalModelDirs {
		if d == extra {
			found = true
		}
	}
	if !found {
		t.Errorf("chosen model dir %q not in config %+v", extra, cfg)
	}
	if _, err := os.Stat(extra); err != nil {
		t.Errorf("chosen model dir not created: %v", err)
	}
}

// TestNoNetworkStillInstalls proves a failed fetch is not fatal.
func TestNoNetworkStillInstalls(t *testing.T) {
	paths := homePaths(t)
	port := freePort(t)
	t.Setenv("SPINEWALL_PORT", port)

	// Offline with an empty cache: the fetch step reports failure and setup goes on.
	_, err := Run(Options{
		Paths:      paths,
		Yes:        true,
		Offline:    true,
		Executable: buildTestBinary(t),
		Doctor:     offlineDetect(),
		Runtime:    spanruntime.New(spanruntime.Options{Dir: paths.Runtime, Offline: true}),
	})
	if err != nil {
		t.Fatalf("setup without network should still install: %v", err)
	}
	defer install.StopServer(paths, func(string) {})
	if _, err := os.Stat(filepath.Join(paths.BinDir, "spinewall")); err != nil {
		t.Errorf("binary not installed without network: %v", err)
	}
}

// TestSetupLinksFetchedPlayersIntoTheTheme is the end-to-end regression for the
// wallpaper opening the theme from file:// and failing to find the players setup
// fetched into the cache. With the cache already holding the players, setup must
// leave every one loading from the theme folder by its relative URL.
func TestSetupLinksFetchedPlayersIntoTheTheme(t *testing.T) {
	paths := homePaths(t)
	t.Setenv("SPINEWALL_PORT", freePort(t))

	// Seed the cache the way a completed fetch leaves it, so the offline run has
	// players to link. The offline fetch reports failure without removing them.
	if err := os.MkdirAll(paths.Runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	names := spanruntime.ServedNames()
	files := map[string]string{}
	for _, name := range names {
		files[name] = hashBytes([]byte("/* " + name + " */\n"))
		if err := os.WriteFile(filepath.Join(paths.Runtime, name), []byte("/* "+name+" */\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Run(Options{
		Paths:      paths,
		Yes:        true,
		Offline:    true,
		Executable: buildTestBinary(t),
		Doctor:     offlineDetect(),
		Runtime:    spanruntime.New(spanruntime.Options{Dir: paths.Runtime, Offline: true}),
	}); err != nil {
		t.Fatalf("setup --yes: %v", err)
	}
	defer install.StopServer(paths, func(string) {})

	for _, name := range names {
		got, err := os.ReadFile(filepath.Join(paths.ThemeDir(), name))
		if err != nil {
			t.Errorf("theme cannot load %s after setup: %v", name, err)
			continue
		}
		if hashBytes(got) != files[name] {
			t.Errorf("theme %s does not match the cached player", name)
		}
	}
}

// TestVerifyThemeRuntimeNamesAMissingLink proves the setup check fails when a
// cached player has no link in the theme folder, which is the state the bug left.
func TestVerifyThemeRuntimeNamesAMissingLink(t *testing.T) {
	paths := homePaths(t)
	if err := os.MkdirAll(paths.Runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.ThemeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range spanruntime.ServedNames() {
		if err := os.WriteFile(filepath.Join(paths.Runtime, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := verifyThemeRuntime(paths); err == nil {
		t.Fatal("a cache with no links passed the theme check")
	}
	if err := install.RefreshThemeRuntime(paths, nil); err != nil {
		t.Fatal(err)
	}
	if err := verifyThemeRuntime(paths); err != nil {
		t.Fatalf("linked cache failed the theme check: %v", err)
	}
}

// ---- helpers ----

// manifestEntry mirrors install.Manifest's JSON so the test can read it without
// exporting loaders the production package does not need.
type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   string `json:"mode"`
}

type testManifest struct {
	Version int             `json:"version"`
	Files   []manifestEntry `json:"files"`
	Dirs    []string        `json:"dirs"`
}

func readManifest(t *testing.T, path string) testManifest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m testManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

// TestServerEnvPinsEveryPath proves the child server cannot escape the chosen
// paths into the ambient HOME. This is the regression for a setup run under a
// throwaway HOME starting a server that reads the real state.
func TestServerEnvPinsEveryPath(t *testing.T) {
	paths := homePaths(t)
	// A misleading ambient HOME must not leak into the child.
	t.Setenv("HOME", "/nonexistent-real-home")
	t.Setenv("SPINEWALL_STATE", "/nonexistent-real-home/state.json")
	env := serverEnv(Options{Paths: paths})

	got := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			got[k] = v
		}
	}
	want := map[string]string{
		"HOME":                  paths.Home,
		"SPINEWALL_MODELS":      paths.ModelsDir(),
		"SPINEWALL_CONFIG":      paths.Config,
		"SPINEWALL_STATE":       filepath.Join(paths.StateDir, "state.json"),
		"SPINEWALL_PREFS":       filepath.Join(paths.StateDir, "prefs.json"),
		"SPINEWALL_LAYOUT":      filepath.Join(paths.StateDir, "layout.json"),
		"SPINEWALL_SCREENS":     filepath.Join(paths.StateDir, "screens.json"),
		"SPINEWALL_THUMBS":      filepath.Join(paths.CacheDir, "thumbs"),
		"SPINEWALL_RUNTIME_DIR": paths.Runtime,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// repoRoot walks up from the test working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the test directory")
		}
		dir = parent
	}
}

// buildTestBinary builds the spinewall binary once per test and returns its path.
// It uses the source tree so the test runs the real command.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "spinewall")
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "-o", out, "./cmd/spinewall")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, msg)
	}
	return out
}

func snapshotTree(t *testing.T, root string) map[string]string {
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
			// A socket or device is not a regular file we can hash.
			out[rel] = "special:" + info.Mode().String()
			return nil
		}
		out[rel] = hashBytes(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(a, b map[string]string) bool {
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

func TestCompositorStartLineForEachCompositor(t *testing.T) {
	bin := "/opt/u/bin/spinewall"
	cases := []struct {
		desktop, args, config, line string
	}{
		{"sway", "serve", "~/.config/sway/config", "exec /opt/u/bin/spinewall serve"},
		{"Hyprland", "serve", "~/.config/hypr/hyprland.conf", "exec-once = /opt/u/bin/spinewall serve"},
		{"river", "serve", "~/.config/river/init", `riverctl spawn "/opt/u/bin/spinewall serve"`},
		{"sway", "serve --with-view", "~/.config/sway/config", "exec /opt/u/bin/spinewall serve --with-view"},
	}
	for _, c := range cases {
		var out strings.Builder
		printCompositorLine(&out, c.desktop, bin, c.args)
		text := out.String()
		if !strings.Contains(text, "Add this line to "+c.config) || !strings.Contains(text, "\n  "+c.line+"\n") {
			t.Errorf("%s %s: printed\n%s\nwant %q in %s", c.desktop, c.args, text, c.line, c.config)
		}
	}
	for _, desktop := range []string{"KDE", "GNOME", "unknown"} {
		var out strings.Builder
		printCompositorLine(&out, desktop, bin, "serve")
		if out.Len() != 0 {
			t.Errorf("%s: printed a compositor line:\n%s", desktop, out.String())
		}
	}
}

func TestSetupPrintsTheSwayLine(t *testing.T) {
	paths := homePaths(t)
	t.Setenv("SPINEWALL_PORT", freePort(t))
	_, out := runCapture(t, Options{
		Paths:      paths,
		Yes:        true,
		Offline:    true,
		Executable: buildTestBinary(t),
		Doctor: doctor.Options{
			Getenv:  func(k string) string { return map[string]string{"XDG_CURRENT_DESKTOP": "sway"}[k] },
			Network: func() bool { return false },
		},
		Runtime: spanruntime.New(spanruntime.Options{Dir: paths.Runtime, Offline: true}),
	})
	defer install.StopServer(paths, func(string) {})
	want := "exec " + filepath.Join(paths.BinDir, "spinewall") + " serve"
	if !strings.Contains(out, "\n  "+want+"\n") {
		t.Errorf("setup did not print %q:\n%s", want, out)
	}
}

// runCapture runs setup with its output in a file, since the form needs an
// *os.File, and returns what it printed.
func runCapture(t *testing.T, opts Options) (Result, string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	opts.Stdout = f
	res, err := Run(opts)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return res, string(data)
}

func TestSetupNextToTheLegacyInstallStartsNothing(t *testing.T) {
	paths := homePaths(t)
	t.Setenv("SPINEWALL_PORT", freePort(t))
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyServer(), []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, out := runCapture(t, Options{
		Paths:      paths,
		Yes:        true,
		Offline:    true,
		Executable: buildTestBinary(t),
		Doctor:     offlineDetect(),
		Runtime:    spanruntime.New(spanruntime.Options{Dir: paths.Runtime, Offline: true}),
	})
	if !strings.Contains(out, "old Python install") || !strings.Contains(out, "spinewall uninstall --legacy") {
		t.Errorf("no legacy warning:\n%s", out)
	}
	if _, err := os.Stat(paths.AutostartFile()); err == nil {
		t.Error("autostart written next to the old Python install")
	}
	if res.Verified {
		t.Error("setup verified a server next to the old Python install")
	}
	if _, err := os.Stat(paths.PidFile()); err == nil {
		t.Error("setup started a server next to the old Python install")
	}
}

func TestVerifyRefusesTheOldPythonServer(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	paths := homePaths(t)
	port := freePort(t)
	t.Setenv("SPINEWALL_PORT", port)
	server := exec.Command(python, "-I", filepath.Join(repoRoot(t), "server", "spinewall-server.py"))
	server.Env = serverEnv(Options{Paths: paths})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	})
	for i := 0; i < 100; i++ {
		if _, ok := queryInfo(port, 200*time.Millisecond); ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	_, _, verified, err := startAndVerify(Options{Paths: paths, Executable: buildTestBinary(t)}, func(string) {})
	if verified || err == nil || !strings.Contains(err.Error(), "uninstall --legacy") {
		t.Errorf("verified = %v, err = %v, want a refusal naming uninstall --legacy", verified, err)
	}
}
