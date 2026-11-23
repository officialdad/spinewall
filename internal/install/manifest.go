package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Manifest records every file and directory the installer created, with each
// file's sha256 and mode. Uninstall removes exactly these. A file that already
// existed before install is marked Created=false and is never removed, so the
// installer cannot delete something it did not write.
type Manifest struct {
	Version int      `json:"version"`
	Files   []Entry  `json:"files"`
	Dirs    []string `json:"dirs"`
}

// Entry is one installed file.
type Entry struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Mode    string `json:"mode"`
	Created bool   `json:"created"`
}

// loadManifest reads a manifest. A missing or malformed file is an empty one.
func loadManifest(path string) Manifest {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{Version: 1}
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil || m.Version == 0 {
		return Manifest{Version: 1}
	}
	return m
}

func (m Manifest) save(path string) error {
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	sort.Strings(m.Dirs)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o644)
}

// manifestOf returns the entry for a path, or false.
func (m Manifest) entry(path string) (Entry, bool) {
	for _, e := range m.Files {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}

// sha256Hex hashes bytes the way the manifest records them.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sha256File hashes a file on disk.
func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

// writeFileAtomic writes data to a temp file in the same directory and renames
// it into place, so a crash never leaves a half-written installed file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
