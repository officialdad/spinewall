package view

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// restartPause is how long to wait before restarting a window that exited on its
// own, so a crash loop does not peg a core.
const restartPause = 2 * time.Second

// Supervisor runs one launcher window per monitor and keeps them alive.
type Supervisor struct {
	bin     string
	env     []string
	apiBase string
	layer   string
	logf    func(string)
}

// SuperviseOptions configure one launch.
type SuperviseOptions struct {
	// Binary is the built launcher.
	Binary string
	// Env is the environment for the windows. It must include the Wayland
	// session variables and QT_WAYLAND_SHELL_INTEGRATION=layer-shell.
	Env []string
	// APIBase is the server origin, such as http://127.0.0.1:8777.
	APIBase string
	// Layer is "bottom" or "background". Empty is bottom.
	Layer string
	// Logf receives progress lines. Nil discards them.
	Logf func(string)
}

// NewSupervisor prepares a supervisor. It does not start anything.
func NewSupervisor(opts SuperviseOptions) *Supervisor {
	logf := opts.Logf
	if logf == nil {
		logf = func(string) {}
	}
	layer := opts.Layer
	if layer == "" {
		layer = "bottom"
	}
	return &Supervisor{bin: opts.Binary, env: opts.Env, apiBase: opts.APIBase, layer: layer, logf: logf}
}

// Run starts a window per monitor and blocks until stop closes and every window
// has exited. An empty monitors list lists them from the launcher. A window that
// exits on its own is restarted after a short pause. The caller owns signal
// handling and closes stop, so a signal never leaves a restart loop behind.
func (s *Supervisor) Run(monitors []Monitor, stop <-chan struct{}) error {
	if len(monitors) == 0 {
		var err error
		monitors, err = ListMonitors(s.bin, s.env)
		if err != nil {
			return err
		}
	}
	if len(monitors) == 0 {
		return fmt.Errorf("the compositor reports no monitors")
	}

	var wg sync.WaitGroup
	for _, m := range monitors {
		wg.Add(1)
		go func(m Monitor) {
			defer wg.Done()
			s.superviseOne(m, stop)
		}(m)
	}

	wg.Wait()
	return nil
}

// superviseOne keeps one monitor's window running until stop closes.
func (s *Supervisor) superviseOne(m Monitor, stop <-chan struct{}) {
	for {
		if stopped(stop) {
			return
		}
		cmd := exec.Command(s.bin, LaunchArgs(s.bin, PageURL(s.apiBase, ScreenKey(m)), s.apiBase, m, s.layer)...)
		cmd.Env = s.env
		// The launcher prints its screen, mask changes and errors on stderr, so
		// the server's log and the journal show what happened.
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			s.logf(fmt.Sprintf("view on %s: %v", monitorName(m), err))
			if stoppedPause(stop) {
				return
			}
			continue
		}

		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		select {
		case <-stop:
			_ = cmd.Process.Signal(syscall.SIGTERM)
			<-exited
			return
		case err := <-exited:
			if stopped(stop) {
				return
			}
			s.logf(fmt.Sprintf("view on %s exited: %v, restarting", monitorName(m), err))
			if stoppedPause(stop) {
				return
			}
		}
	}
}

func monitorName(m Monitor) string {
	if m.Name != "" {
		return m.Name
	}
	return m.Geometry
}

// stopped reports whether stop has closed.
func stopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// stoppedPause waits the restart pause, or returns true at once when stop
// closed, so the caller returns instead of restarting.
func stoppedPause(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	case <-time.After(restartPause):
		return false
	}
}
