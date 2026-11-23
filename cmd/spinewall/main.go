package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/officialdad/spinewall/internal/api"
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
		return serve()
	default:
		return fmt.Errorf("unknown command %q. This build only ships 'serve'.", command)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func homePath(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return rel
	}
	return filepath.Join(home, rel)
}

func serve() error {
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
	srv := api.New(store, port, api.Theme{Page: theme.IndexHTML, LocalHost: theme.LocalHostJS})

	fmt.Fprintf(os.Stderr, "spinewall: %d model(s) from %s, port %d\n", len(store.ModelsCopy()), modelsDir, port)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: srv}
	return server.Serve(listener)
}
