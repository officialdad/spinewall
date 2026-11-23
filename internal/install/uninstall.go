package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// UninstallOptions configure one uninstall run.
type UninstallOptions struct {
	Paths Paths
	// RemoveState also removes the state directory (state, prefs, layout,
	// screens, manifest) after the files are gone. The caller asks first.
	RemoveState bool
	// RemoveCache also removes the cache directory (runtime, thumbnails).
	RemoveCache bool
	// StopProcess stops a running server before removing files.
	StopProcess bool
	// Logf receives progress lines. Nil discards them.
	Logf func(string)
}

// UninstallResult reports what was removed and what was kept.
type UninstallResult struct {
	RemovedFiles []string
	RemovedDirs  []string
	KeptFiles    []string
	StateRemoved bool
	CacheRemoved bool
}

// Uninstall removes exactly the files the manifest records, then the
// directories this installer created, deepest first. It never touches the
// models directory or any file the installer did not write.
func Uninstall(opts UninstallOptions) (UninstallResult, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string) {}
	}
	paths := opts.Paths
	if paths.Home == "" {
		paths = DefaultPaths()
	}
	var res UninstallResult

	if opts.StopProcess {
		if err := StopServer(paths, logf); err != nil {
			logf("spinewall: could not stop the server: " + err.Error())
		}
	}

	// Remove the systemd and autostart entries through the manifest first, so a
	// running unit is disabled before its file disappears.
	manifest := loadManifest(paths.ManifestPath())

	// Turn off any enabled systemd unit before removing its file.
	if _, ok := manifest.entry(paths.UnitFile()); ok {
		if unitCommand(paths, "disable", "--now") {
			logf("disabled the systemd user unit")
		}
	}

	for _, e := range manifest.Files {
		if !e.Created {
			res.KeptFiles = append(res.KeptFiles, e.Path)
			continue
		}
		if err := os.Remove(e.Path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return res, fmt.Errorf("remove %s: %w", e.Path, err)
		}
		res.RemovedFiles = append(res.RemovedFiles, e.Path)
		logf("removed: " + e.Path)
	}

	// The manifest goes once its file list is used, so its own state directory
	// can be pruned below.
	if err := os.Remove(paths.ManifestPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("remove manifest: %w", err)
	}

	if opts.RemoveState {
		if err := os.RemoveAll(paths.StateDir); err != nil {
			return res, fmt.Errorf("remove %s: %w", paths.StateDir, err)
		}
		res.StateRemoved = true
		logf("removed state: " + paths.StateDir)
	}
	if opts.RemoveCache {
		if err := os.RemoveAll(paths.CacheDir); err != nil {
			return res, fmt.Errorf("remove %s: %w", paths.CacheDir, err)
		}
		res.CacheRemoved = true
		logf("removed cache: " + paths.CacheDir)
	}

	// Directories, deepest first, and only when empty, so a directory that holds
	// files the user added is kept. The models directory is only removed when it
	// is empty, so a folder with models is never touched.
	dirs := append([]string(nil), manifest.Dirs...)
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		removed, err := removeIfEmpty(d)
		if err != nil {
			return res, err
		}
		if removed {
			res.RemovedDirs = append(res.RemovedDirs, d)
		}
	}
	return res, nil
}

// removeIfEmpty removes a directory only when it has no entries. It reports
// whether it removed it, so the caller can note which directories went.
func removeIfEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return false, nil
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove %s: %w", dir, err)
	}
	return true, nil
}

// legacyPythonPaths are the files the old Python installer wrote. Uninstall
// --legacy removes them so an existing user migrates cleanly. The patched
// containment copy lives beside the system plasmoid; removing it lets the
// desktop fall back to the system one, or to its restored .bak. The unit and
// the theme share their paths with the new install, so each is only listed
// while it still belongs to the old one.
func legacyPythonPaths(paths Paths) []string {
	found := []string{paths.LegacyServer(), paths.LegacyContainment()}
	if isLegacyUnit(paths.UnitFile()) {
		found = append(found, paths.UnitFile())
	}
	if !manifestHasUnder(loadManifest(paths.ManifestPath()), paths.ThemeDir()) {
		found = append(found, paths.ThemeDir())
	}
	return found
}

// manifestHasUnder reports whether this install created a file inside dir. A
// file it found already there predates it, so it does not count.
func manifestHasUnder(m Manifest, dir string) bool {
	for _, e := range m.Files {
		if e.Created && strings.HasPrefix(e.Path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// LegacyUninstall removes the old Python install and restores the patched
// containment .bak. It is safe to run more than once.
func LegacyUninstall(paths Paths, logf func(string)) error {
	if logf == nil {
		logf = func(string) {}
	}
	if paths.Home == "" {
		paths = DefaultPaths()
	}
	if isLegacyUnit(paths.UnitFile()) {
		unitCommand(paths, "disable", "--now")
	}

	// Stop a python server a user may have started by hand.
	killMatching(func(pid int) bool { return isLegacyServer(pid, paths.LegacyServer()) }, logf)

	for _, f := range legacyPythonPaths(paths) {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if err := os.RemoveAll(f); err != nil {
				return fmt.Errorf("remove %s: %w", f, err)
			}
			logf("removed legacy: " + f)
			continue
		}
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", f, err)
		}
		logf("removed legacy: " + f)
	}

	// Restore the containment backup the patch script left behind.
	containment := paths.LegacyContainment()
	bak := containment + ".bak"
	if info, err := os.Stat(bak); err == nil && info.IsDir() {
		if err := os.RemoveAll(containment); err != nil {
			return fmt.Errorf("remove patched containment: %w", err)
		}
		if err := os.Rename(bak, containment); err != nil {
			return fmt.Errorf("restore containment .bak: %w", err)
		}
		logf("restored the patched containment backup")
	}
	return nil
}
