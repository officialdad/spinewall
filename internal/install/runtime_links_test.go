package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/officialdad/spinewall/internal/runtime"
)

// seedCache writes stand-in players and the stylesheet into the runtime cache
// the way a fetch would, so a theme link has a real target.
func seedCache(t *testing.T, paths Paths) []string {
	t.Helper()
	if err := os.MkdirAll(paths.Runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	names := runtime.ServedNames()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(paths.Runtime, name), []byte("/* "+name+" */\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return names
}

// themeHasEveryPlayer reports the runtime files the theme folder cannot load.
func themeHasEveryPlayer(t *testing.T, paths Paths, names []string) []string {
	t.Helper()
	var missing []string
	for _, name := range names {
		got, err := os.ReadFile(filepath.Join(paths.ThemeDir(), name))
		if err != nil {
			missing = append(missing, name)
			continue
		}
		want, err := os.ReadFile(filepath.Join(paths.Runtime, name))
		if err != nil || string(got) != string(want) {
			missing = append(missing, name)
		}
	}
	return missing
}

// TestInstallLinksFetchedRuntimesIntoTheTheme is the regression for the wallpaper
// opening the theme from file:// and failing to find the players setup fetched
// into the cache. After install, every cached player must load from the theme
// folder by its relative URL.
func TestInstallLinksFetchedRuntimesIntoTheTheme(t *testing.T) {
	paths := testPaths(t)
	names := seedCache(t, paths)

	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t)}); err != nil {
		t.Fatalf("install: %v", err)
	}

	if missing := themeHasEveryPlayer(t, paths, names); len(missing) > 0 {
		t.Fatalf("theme cannot load %v after install", missing)
	}

	// Each player is recorded, so uninstall removes the links it wrote.
	manifest := loadManifest(paths.ManifestPath())
	for _, name := range names {
		if _, ok := manifest.entry(filepath.Join(paths.ThemeDir(), name)); !ok {
			t.Errorf("manifest does not record the theme's %s", name)
		}
	}
}

// TestRuntimeLinksSurviveARefetch proves a refetched cache stays reachable: the
// theme link follows the cache file, so rewriting the cache refreshes the page.
func TestRuntimeLinksSurviveARefetch(t *testing.T) {
	paths := testPaths(t)
	names := seedCache(t, paths)
	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	// Rewrite the cache the way a refetch would.
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(paths.Runtime, name), []byte("/* refetched "+name+" */\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if missing := themeHasEveryPlayer(t, paths, names); len(missing) > 0 {
		t.Fatalf("theme cannot load %v after a refetch", missing)
	}
}

// TestRefreshLinksACacheThatArrivedAfterInstall covers a first run that fetched
// nothing (offline) and a later fetch: the theme folder gets the links then, and
// the manifest records them so uninstall removes them.
func TestRefreshLinksACacheThatArrivedAfterInstall(t *testing.T) {
	paths := testPaths(t)
	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	names := seedCache(t, paths)

	if err := RefreshThemeRuntime(paths, nil); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if missing := themeHasEveryPlayer(t, paths, names); len(missing) > 0 {
		t.Fatalf("theme cannot load %v after refresh", missing)
	}
	manifest := loadManifest(paths.ManifestPath())
	for _, name := range names {
		if _, ok := manifest.entry(filepath.Join(paths.ThemeDir(), name)); !ok {
			t.Errorf("refresh did not record the theme's %s", name)
		}
	}
}

// TestUninstallRemovesLinksNotTheirTarget proves uninstall deletes the theme
// links but leaves the runtime cache for the caller who asked to keep it.
func TestUninstallRemovesLinksNotTheirTarget(t *testing.T) {
	paths := testPaths(t)
	names := seedCache(t, paths)
	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(UninstallOptions{Paths: paths}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(paths.ThemeDir(), name)); err == nil {
			t.Errorf("theme link %s survived uninstall", name)
		}
		if _, err := os.Stat(filepath.Join(paths.Runtime, name)); err != nil {
			t.Errorf("uninstall removed the cache target %s: %v", name, err)
		}
	}
}

// TestRefreshNeedsAnInstall stops RefreshThemeRuntime from writing a manifest
// before install wrote the theme, which would mislead uninstall.
func TestRefreshNeedsAnInstall(t *testing.T) {
	paths := testPaths(t)
	seedCache(t, paths)
	if err := RefreshThemeRuntime(paths, nil); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := os.Stat(paths.ManifestPath()); err == nil {
		t.Fatal("refresh wrote a manifest without an install")
	}
}

// TestInstallKeepsAHandMadeLink proves the installer does not claim a link a user
// made by hand, so uninstall leaves it. This is the shape of the reported manual
// workaround: symlinks from the theme folder to the cached players.
func TestInstallKeepsAHandMadeLink(t *testing.T) {
	paths := testPaths(t)
	names := seedCache(t, paths)
	if err := os.MkdirAll(paths.ThemeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	hand := filepath.Join(paths.ThemeDir(), names[0])
	if err := os.Symlink(filepath.Join(paths.Runtime, names[0]), hand); err != nil {
		t.Skipf("symlinks are refused here: %v", err)
	}

	if _, err := Install(Options{Paths: paths, Executable: fakeBinary(t)}); err != nil {
		t.Fatal(err)
	}
	entry, ok := loadManifest(paths.ManifestPath()).entry(hand)
	if !ok {
		t.Fatalf("%s not recorded", hand)
	}
	if entry.Created {
		t.Errorf("a hand-made link was recorded as installer-created")
	}

	if _, err := Uninstall(UninstallOptions{Paths: paths}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(hand); err != nil {
		t.Errorf("uninstall removed the hand-made link: %v", err)
	}
}
