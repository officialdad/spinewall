package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/officialdad/spinewall/internal/view"
)

// viewEnv is the environment the launcher windows need. It carries the current
// session so a window lands on the running compositor, plus the layer-shell
// shell integration the surface requires.
func viewEnv() []string {
	env := os.Environ()
	have := map[string]bool{}
	for _, kv := range env {
		if key, _, ok := strings.Cut(kv, "="); ok {
			have[key] = true
		}
	}
	if !have["QT_WAYLAND_SHELL_INTEGRATION"] {
		env = append(env, "QT_WAYLAND_SHELL_INTEGRATION=layer-shell")
	}
	// QtWebEngine on NVIDIA may need the GPU blocklist ignored; the spike saw
	// WebGL work without it, so this is a documented escape hatch, not a default.
	return env
}

// viewDir is where the launcher source and binary live.
func viewDir() string {
	if dir := os.Getenv("SPINEWALL_VIEW_DIR"); dir != "" {
		return dir
	}
	cache := envOrDefault("XDG_CACHE_HOME", homePath(".cache"))
	return filepath.Join(cache, "spinewall", "view")
}

// apiBase is the server origin for this process.
func apiBase() string {
	return "http://127.0.0.1:" + envOrDefault("SPINEWALL_PORT", "8777")
}

// runViewCommand handles `spinewall view`, the launcher's own entry point.
// It builds the launcher if needed, then runs a window per monitor in the
// foreground until interrupted. `spinewall serve --with-view` calls launchView.
func runViewCommand(args []string) error {
	layer := "bottom"
	listOnly := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--layer":
			if i+1 >= len(args) {
				return fmt.Errorf("--layer needs bottom or background")
			}
			i++
			layer = args[i]
		case "--list-monitors":
			listOnly = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	logf := func(line string) { fmt.Fprintln(os.Stderr, line) }
	bin, err := view.Build(viewDir(), logf)
	if err != nil {
		return err
	}
	if listOnly {
		monitors, err := view.ListMonitors(bin, viewEnv())
		if err != nil {
			return err
		}
		for _, m := range monitors {
			fmt.Printf("%s\t%s\n", m.Name, m.Geometry)
		}
		return nil
	}
	return launchView(bin, apiBase(), layer, logf, nil)
}

// launchView starts the supervisor for one window per monitor and blocks until
// stop closes. It is shared by `view` and `serve --with-view`.
func launchView(bin, apiBase, layer string, logf func(string), stop <-chan struct{}) error {
	if stop == nil {
		stop = make(chan struct{}) // never closed: run until a signal
	}
	sup := view.NewSupervisor(view.SuperviseOptions{
		Binary:  bin,
		Env:     viewEnv(),
		APIBase: apiBase,
		Layer:   layer,
		Logf:    logf,
	})
	return sup.Run(nil, stop)
}
