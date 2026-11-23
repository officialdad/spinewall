package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/officialdad/spinewall/internal/install"
	"github.com/officialdad/spinewall/internal/view"
)

// viewEnv is the environment the launcher windows need. It carries the current
// session so a window lands on the running compositor, plus the platform hook
// the chosen surface requires: layer-shell on Wayland, xcb on a real X11
// session.
func viewEnv(backend string) []string {
	env := os.Environ()
	have := map[string]bool{}
	for _, kv := range env {
		if key, _, ok := strings.Cut(kv, "="); ok {
			have[key] = true
		}
	}
	switch backend {
	case view.BackendX11:
		if !have["QT_QPA_PLATFORM"] {
			env = append(env, "QT_QPA_PLATFORM=xcb")
		}
	default:
		if !have["QT_WAYLAND_SHELL_INTEGRATION"] {
			env = append(env, "QT_WAYLAND_SHELL_INTEGRATION=layer-shell")
		}
	}
	// QtWebEngine on NVIDIA may need the GPU blocklist ignored; the spike saw
	// WebGL work without it, so this is a documented escape hatch, not a default.
	return env
}

// validBackend reports whether name is a native view backend this build knows.
func validBackend(name string) bool {
	return name == view.BackendLayerShell || name == view.BackendX11
}

// viewDir is where the launcher source and binary live. It is the directory
// setup installs into and records in the manifest, so serve runs that launcher.
func viewDir() string {
	return install.DefaultPaths().ViewDir()
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
	backend := view.SessionBackend(os.Getenv)
	listOnly := false
	browser := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--layer":
			if i+1 >= len(args) {
				return fmt.Errorf("--layer needs bottom or background")
			}
			i++
			layer = args[i]
		case "--backend":
			if i+1 >= len(args) {
				return fmt.Errorf("--backend needs layer-shell or x11")
			}
			i++
			backend = args[i]
		case "--browser":
			browser = true
		case "--list-monitors":
			listOnly = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if !validBackend(backend) {
		return fmt.Errorf("unknown backend %q. Use layer-shell or x11.", backend)
	}

	logf := func(line string) { fmt.Fprintln(os.Stderr, line) }
	// The kiosk fallback needs no launcher and no Qt toolchain, so it runs
	// before the build below.
	if browser {
		return runBrowser(apiBase(), logf)
	}
	bin, err := view.Build(viewDir(), logf)
	if err != nil {
		return err
	}
	if listOnly {
		monitors, err := view.ListMonitors(bin, viewEnv(backend))
		if err != nil {
			return err
		}
		for _, m := range monitors {
			fmt.Printf("%s\t%s\n", m.Name, m.Geometry)
		}
		return nil
	}
	ctx, cancel := stopSignals()
	defer cancel()
	return launchView(bin, apiBase(), layer, backend, logf, ctx.Done())
}

// runBrowser starts the kiosk-browser fallback and waits for it, stopping it on
// the same signals that stop the native view.
func runBrowser(apiBase string, logf func(string)) error {
	b, err := view.BrowserCommand(view.BrowserURL(apiBase), nil)
	if err != nil {
		return err
	}
	logf("kiosk browser: " + b.Name + " " + strings.Join(b.Args, " "))
	cmd := exec.Command(b.Name, b.Args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	ctx, cancel := stopSignals()
	defer cancel()
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		<-done
		return nil
	case err := <-done:
		return err
	}
}

// stopSignals ends on SIGTERM or SIGINT. Notify turns off Go's default exit, so
// whoever calls it must stop everything it runs when the context ends.
func stopSignals() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
}

// launchView starts the supervisor for one window per monitor and blocks until
// stop closes and the windows have exited. It is shared by `view` and
// `serve --with-view`.
func launchView(bin, apiBase, layer, backend string, logf func(string), stop <-chan struct{}) error {
	sup := view.NewSupervisor(view.SuperviseOptions{
		Binary:  bin,
		Env:     viewEnv(backend),
		APIBase: apiBase,
		Layer:   layer,
		Backend: backend,
		Logf:    logf,
	})
	return sup.Run(nil, stop)
}
