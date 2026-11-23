package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/officialdad/spinewall/internal/api"
	"github.com/officialdad/spinewall/internal/runtime"
	"github.com/officialdad/spinewall/internal/state"
	"github.com/officialdad/spinewall/theme"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "serve":
		return serve(args[1:])
	case "runtime":
		return runtimeCommand(args[1:])
	default:
		return fmt.Errorf("unknown command %q. This build ships 'serve' and 'runtime'.", command)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBool reads a boolean env var. "0", "false" and empty are false.
func envBool(key string) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

func homePath(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return rel
	}
	return filepath.Join(home, rel)
}

func serve(args []string) error {
	offline := envBool("SPINEWALL_OFFLINE")
	runtimeDirFlag := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--offline":
			offline = true
		case "--runtime-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("--runtime-dir needs a directory")
			}
			i++
			runtimeDirFlag = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	portStr := envOrDefault("SPINEWALL_PORT", "8777")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("SPINEWALL_PORT must be an integer")
	}

	modelsDir := envOrDefault("SPINEWALL_MODELS", homePath(".local/share/spinewall/models"))
	paths := state.Paths{
		ModelsDir:       modelsDir,
		ConfigFile:      envOrDefault("SPINEWALL_CONFIG", homePath(".config/spinewall/config.json")),
		StateFile:       envOrDefault("SPINEWALL_STATE", homePath(".local/state/spinewall/state.json")),
		PrefsFile:       envOrDefault("SPINEWALL_PREFS", homePath(".local/state/spinewall/prefs.json")),
		LayoutFile:      envOrDefault("SPINEWALL_LAYOUT", homePath(".local/state/spinewall/layout.json")),
		ScreensFile:     envOrDefault("SPINEWALL_SCREENS", homePath(".local/state/spinewall/screens.json")),
		ThumbDir:        envOrDefault("SPINEWALL_THUMBS", homePath(".cache/spinewall/thumbs")),
		LegacyStateFile: homePath(".local/state/spinewall.json"),
	}

	logf := func(line string) { fmt.Fprintln(os.Stderr, line) }
	store := state.Open(paths, logf)

	// The page links every player, and each model names the one it needs. Fetch
	// only those, plus the stylesheet, so a first run downloads the small set in
	// use. A failed fetch is logged and the server still starts: the page shows
	// its "runtime not installed" notice instead of a black wallpaper.
	runtimeDir := envOrDefault("SPINEWALL_RUNTIME_DIR", runtime.DefaultDir())
	if runtimeDirFlag != "" {
		runtimeDir = runtimeDirFlag
	}
	manager := runtime.New(runtime.Options{Dir: runtimeDir, Offline: offline, Logf: logf})
	if err := manager.Ensure(neededRuntimes(store)); err != nil {
		logf("spinewall: " + err.Error())
	}
	if offline {
		logf("spinewall: runtime offline, using the cache at " + runtimeDir)
	}

	srv := api.New(store, port, api.Theme{Page: theme.IndexHTML, LocalHost: theme.LocalHostJS, Runtime: manager})

	fmt.Fprintf(os.Stderr, "spinewall: %d model(s) from %s, port %d\n", len(store.ModelsCopy()), modelsDir, port)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: srv}
	return server.Serve(listener)
}

// neededRuntimes returns the distinct runtime globals the scanned models use.
func neededRuntimes(store *state.Store) []string {
	seen := map[string]bool{}
	order := []string{}
	for _, model := range store.ModelsCopy() {
		if model.Runtime != "" && !seen[model.Runtime] {
			seen[model.Runtime] = true
			order = append(order, model.Runtime)
		}
	}
	return order
}

func runtimeCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: spinewall runtime fetch|verify [--runtime-dir DIR] [--offline]")
	}
	action := args[0]
	dir := envOrDefault("SPINEWALL_RUNTIME_DIR", runtime.DefaultDir())
	offline := envBool("SPINEWALL_OFFLINE")
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--runtime-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("--runtime-dir needs a directory")
			}
			i++
			dir = args[i]
		case "--offline":
			offline = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	logf := func(line string) { fmt.Fprintln(os.Stderr, line) }
	manager := runtime.New(runtime.Options{Dir: dir, Offline: offline, Logf: logf})

	switch action {
	case "fetch":
		if err := manager.FetchAll(); err != nil {
			return err
		}
		man := manager.Manifest()
		names := make([]string, 0, len(man.Files))
		for name := range man.Files {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Printf("runtime cache: %s\n", dir)
		for _, name := range names {
			fmt.Printf("  %s  %s\n", man.Files[name], name)
		}
		return nil
	case "verify":
		if err := manager.Verify(); err != nil {
			return err
		}
		fmt.Printf("runtime cache ok: %s (%d files)\n", dir, len(manager.Manifest().Files))
		return nil
	default:
		return fmt.Errorf("unknown runtime action %q. Use fetch or verify.", action)
	}
}
