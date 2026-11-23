package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/officialdad/spinewall/internal/api"
	"github.com/officialdad/spinewall/internal/cli"
	"github.com/officialdad/spinewall/internal/install"
	"github.com/officialdad/spinewall/internal/runtime"
	"github.com/officialdad/spinewall/internal/state"
	"github.com/officialdad/spinewall/internal/view"
	"github.com/officialdad/spinewall/theme"
)

// version is set at build time with -X main.version.
var version = "dev"

func main() {
	// A first argument of serve, runtime, doctor, setup, install or uninstall is
	// a local command. Anything else is a client command against the running
	// server, including no argument (info).
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			if err := serve(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "runtime":
			if err := runtimeCommand(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "doctor":
			if err := runDoctor(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "setup":
			if err := runSetup(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "install":
			if err := runInstall(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "uninstall":
			if err := runUninstall(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "build-theme":
			if err := runBuildTheme(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "view":
			if err := runViewCommand(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "spinewall: %v\n", err)
				os.Exit(1)
			}
			return
		case "version", "--version":
			fmt.Println(version)
			return
		}
	}
	os.Exit(cli.Run(os.Args[1:]))
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
	withView := false
	viewLayer := "bottom"
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
		case "--with-view":
			withView = true
		case "--view-layer":
			if i+1 >= len(args) {
				return fmt.Errorf("--view-layer needs bottom or background")
			}
			i++
			viewLayer = args[i]
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
		ThumbDir:        envOrDefault("SPINEWALL_THUMBS", filepath.Join(filepath.Dir(runtime.DefaultDir()), "thumbs")),
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
	// The page loads the players over file://, so keep the theme folder's links
	// current with the cache. A missing theme folder is a no-op.
	installPaths := install.DefaultPaths()
	installPaths.Runtime = runtimeDir
	if err := install.RefreshThemeRuntime(installPaths, logf); err != nil {
		// The hardened systemd unit mounts the theme folder read-only, so the
		// server cannot relink it. Say once how to fix it by hand.
		logf("spinewall: cannot relink the theme folder " + installPaths.ThemeDir() + ": " + err.Error() + ". Run 'spinewall runtime fetch' or 'spinewall setup' to relink the players.")
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
	// Record the pid so `spinewall uninstall` can stop this process. A failure to
	// write it is not fatal: the port check in StopServer is the backstop.
	writePidFile(filepath.Dir(paths.StateFile))

	// With a native view, one process owns both halves: the server serves the
	// page and the launcher draws it. The view waits for the listener above, so
	// its first page load reaches a live server. A build failure is not fatal to
	// the server; the page is still there for a browser or the KDE legacy backend.
	server := &http.Server{Handler: srv}
	if !withView {
		return server.Serve(listener)
	}

	// The signal ends both halves: the windows stop without a restart, and the
	// server frees the port, so setup and uninstall can start a new one.
	ctx, cancel := stopSignals()
	defer cancel()
	viewDone := make(chan struct{})
	go func() {
		defer close(viewDone)
		bin, err := view.Build(viewDir(), logf)
		if err != nil {
			logf("spinewall: native view unavailable: " + err.Error())
			return
		}
		if err := launchView(bin, apiBase(), viewLayer, logf, ctx.Done()); err != nil {
			logf("spinewall: native view stopped: " + err.Error())
		}
	}()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	err = server.Serve(listener)
	cancel()
	<-viewDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// writePidFile records the process id beside the server state.
func writePidFile(stateDir string) {
	pidFile := filepath.Join(stateDir, "server.pid")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
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
	// A --runtime-dir override means the links must read that cache, not the
	// default one.
	paths := install.DefaultPaths()
	paths.Runtime = dir

	switch action {
	case "fetch":
		if err := manager.FetchAll(); err != nil {
			return err
		}
		if err := install.RefreshThemeRuntime(paths, logf); err != nil {
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
		if err := install.RefreshThemeRuntime(paths, logf); err != nil {
			return err
		}
		fmt.Printf("runtime cache ok: %s (%d files)\n", dir, len(manager.Manifest().Files))
		return nil
	default:
		return fmt.Errorf("unknown runtime action %q. Use fetch or verify.", action)
	}
}
