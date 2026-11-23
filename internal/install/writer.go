package install

import (
	"fmt"
	"os"
	"path/filepath"
)

// writer accumulates what an install produced so the manifest records exactly
// the files and directories this run created.
type writer struct {
	paths Paths
	prev  Manifest // the manifest from a previous run, so a reinstall keeps entries
	files map[string]Entry
	dirs  []string
}

func newWriter(paths Paths) *writer {
	prev := loadManifest(paths.ManifestPath())
	w := &writer{paths: paths, prev: prev, files: map[string]Entry{}}
	// Carry forward every earlier entry. A path this run rewrites replaces its
	// entry; an unrelated earlier file keeps its Created flag so uninstall still
	// removes it.
	for _, e := range prev.Files {
		w.files[e.Path] = e
	}
	return w
}

// write records one file. It creates parent directories first and tracks each
// one this run created. A file that already existed is marked Created=false, so
// uninstall never removes a file the installer did not make.
func (w *writer) write(path string, data []byte, mode os.FileMode) error {
	_, existed := os.Stat(path)
	if err := w.ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := writeFileAtomic(path, data, mode); err != nil {
		return err
	}
	created := existed != nil
	if existing, ok := w.prev.entry(path); ok && existing.Created {
		// An earlier install made this file, so a reinstall still owns it.
		created = true
	}
	w.files[path] = Entry{
		Path:    path,
		SHA256:  sha256Hex(data),
		Mode:    fmt.Sprintf("%04o", mode.Perm()),
		Created: created,
	}
	return nil
}

// ensureDir creates dir and every missing parent, recording only the
// directories this run created, deepest last.
func (w *writer) ensureDir(dir string) error {
	if dir == "" || dir == "." || dir == "/" {
		return nil
	}
	if info, err := os.Stat(dir); err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	if err := w.ensureDir(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for _, d := range w.dirs {
		if d == dir {
			return nil
		}
	}
	w.dirs = append(w.dirs, dir)
	return nil
}

// manifest folds the recorded files and directories into a Manifest.
func (w *writer) manifest() Manifest {
	m := Manifest{Version: 1}
	for _, e := range w.files {
		m.Files = append(m.Files, e)
	}
	m.Dirs = append(m.Dirs, w.dirs...)
	// Keep earlier directories that still track a created file, so a partial
	// reinstall does not forget a parent an earlier run made.
	for _, d := range w.prev.Dirs {
		found := false
		for _, have := range m.Dirs {
			if have == d {
				found = true
				break
			}
		}
		if !found {
			m.Dirs = append(m.Dirs, d)
		}
	}
	return m
}
