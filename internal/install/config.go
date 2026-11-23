package install

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the user config file at ~/.config/spinewall/config.json. The server
// reads additional_model_dirs from it. The installer writes only that key, so a
// hand edit to any other key survives a reinstall.
type Config struct {
	AdditionalModelDirs []string `json:"additional_model_dirs"`
}

// ReadConfig reads the config file. A missing or malformed file is empty, which
// matches how the server treats it.
func ReadConfig(path string) Config {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

// WriteModelDir records the models folder as the primary model directory. The
// server's primary directory is SPINEWALL_MODELS (no config key), so a chosen
// folder that is not the default becomes an additional_model_dirs entry, and a
// chosen folder equal to the default clears any override.
//
// The config file is installed state, so the manifest records it and uninstall
// removes it.
func WriteModelDir(w *writer, configPath, chosen, defaultModels string) error {
	cfg := ReadConfig(configPath)
	clean := make([]string, 0, len(cfg.AdditionalModelDirs)+1)
	seen := map[string]bool{}
	// Keep any dirs the user already listed, minus the default and duplicates.
	for _, dir := range cfg.AdditionalModelDirs {
		if dir == "" || dir == chosen || dir == defaultModels || seen[dir] {
			continue
		}
		seen[dir] = true
		clean = append(clean, dir)
	}
	if chosen != "" && chosen != defaultModels {
		clean = append([]string{chosen}, clean...)
	}
	cfg.AdditionalModelDirs = clean

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return w.write(configPath, data, 0o644)
}

// ConfigPath is a convenience for callers that have only a Paths value.
func ConfigPath(paths Paths) string {
	if paths.Config != "" {
		return paths.Config
	}
	return filepath.Join(paths.Home, ".config", "spinewall", "config.json")
}
