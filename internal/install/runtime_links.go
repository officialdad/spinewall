package install

import (
	"os"
	"path/filepath"

	"github.com/officialdad/spinewall/internal/runtime"
)

// ThemeRuntimeEntries makes every cached runtime file reachable from the theme
// folder and returns the manifest entries for them, so uninstall removes the
// links it wrote. The page loads the Spine players by relative URL, and the KDE
// backend opens it from file://, so a link in the theme folder is the only way
// the page finds a player that the cache holds.
//
// A link is a symlink into the cache when the filesystem allows one, so a
// refetch that writes the same cache path keeps the theme current with no
// second copy. A copy is the fallback where symlinks are refused. An entry is
// Created only when this call made the path; a file or link that was already
// there keeps Created=false so uninstall never removes what the installer did
// not write. The theme folder must already exist; a folder that is not there is
// not an install.
func ThemeRuntimeEntries(paths Paths, logf func(string)) ([]Entry, error) {
	if logf == nil {
		logf = func(string) {}
	}
	themeDir := paths.ThemeDir()
	if !exists(themeDir) {
		return nil, nil
	}
	// A relative target would resolve against the theme folder, not the
	// caller's working directory, and swap a good link for a dangling one.
	runtimeDir, err := filepath.Abs(paths.Runtime)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, name := range runtime.ServedNames() {
		src := filepath.Join(runtimeDir, name)
		dst := filepath.Join(themeDir, name)

		// A player the cache does not hold yet has no link. A dangling link from
		// an earlier run is left for uninstall, which removes it by path.
		if info, err := os.Stat(src); err != nil || info.IsDir() {
			continue
		}
		existedBefore := exists(dst)
		if err := linkOrCopy(src, dst); err != nil {
			return entries, err
		}
		hash, err := sha256File(dst)
		if err != nil {
			return entries, err
		}
		entries = append(entries, Entry{
			Path:    dst,
			SHA256:  hash,
			Mode:    "0644",
			Created: !existedBefore,
		})
	}
	if len(entries) > 0 {
		logf("linked runtime into the theme: " + themeDir)
	}
	return entries, nil
}

// linkOrCopy points dst at src. An already-correct symlink is left alone, so a
// repeated run rewrites nothing. A file or a stale link is replaced by a
// symlink, created at a temp name and renamed into place so the swap is atomic.
// When a symlink is refused, the file is copied, and only when its bytes differ
// from src, so a repeated refresh does not churn the theme folder.
func linkOrCopy(src, dst string) error {
	if info, err := os.Lstat(dst); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(dst); err == nil && target == src {
			return nil
		}
	}
	if tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.tmp"); err == nil {
		name := tmp.Name()
		tmp.Close()
		os.Remove(name)
		defer os.Remove(name)
		if err := os.Symlink(src, name); err == nil {
			return os.Rename(name, dst)
		}
	}
	want, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if got, err := os.ReadFile(dst); err == nil && string(got) == string(want) {
		return nil
	}
	return writeFileAtomic(dst, want, 0o644)
}

// ownEntries folds entries into the manifest and returns whether it changed.
// A path an earlier install created stays owned, so a reinstall still removes
// it. A path that predates any install stays Created=false, so uninstall never
// removes what the installer did not write.
func ownEntries(m *Manifest, entries []Entry) bool {
	changed := false
	for _, e := range entries {
		if before, ok := m.entry(e.Path); ok {
			e.Created = e.Created || before.Created
			if before.SHA256 == e.SHA256 && before.Created == e.Created {
				continue
			}
		}
		m.set(e)
		changed = true
	}
	return changed
}

// RefreshThemeRuntime re-links every cached runtime into the theme folder and
// records the result in the install manifest. setup, `runtime fetch`, `runtime
// verify`, and the server start call it so a theme installed before a fetch, or
// a refetched or repaired cache, ends up reachable from the file:// page.
//
// Only the theme's own runtime dir relinks it: the default one, or the one its
// links already point into. A scratch --runtime-dir for a throwaway server or
// fetch must leave the live wallpaper and its manifest alone.
func RefreshThemeRuntime(paths Paths, logf func(string)) error {
	if logf == nil {
		logf = func(string) {}
	}
	if !ownsThemeRuntime(paths) {
		return nil
	}
	entries, err := ThemeRuntimeEntries(paths, logf)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	// Refresh only maintains a manifest install already wrote. Before install
	// there is nothing to record against, and a partial manifest would mislead
	// uninstall.
	if !exists(paths.ManifestPath()) {
		return nil
	}
	manifest := loadManifest(paths.ManifestPath())
	if !ownEntries(&manifest, entries) {
		return nil
	}
	return manifest.save(paths.ManifestPath())
}

// ownsThemeRuntime reports whether paths.Runtime is the cache the installed
// theme reads: the default runtime dir, or the dir a theme link points into.
func ownsThemeRuntime(paths Paths) bool {
	runtimeDir, err := filepath.Abs(paths.Runtime)
	if err != nil {
		return false
	}
	if runtimeDir == filepath.Join(paths.CacheDir, "runtime") {
		return true
	}
	if defaultDir, err := filepath.Abs(runtime.DefaultDir()); err == nil && runtimeDir == defaultDir {
		return true
	}
	for _, name := range runtime.ServedNames() {
		if target, err := os.Readlink(filepath.Join(paths.ThemeDir(), name)); err == nil && filepath.Dir(target) == runtimeDir {
			return true
		}
	}
	return false
}
