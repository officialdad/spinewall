package view

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

// restartPause is how long to wait before restarting a window that exited on its
// own, so a crash loop does not peg a core.
const restartPause = 2 * time.Second

// monitorPoll is how often the supervisor re-reads the compositor's monitors. A
// display reconfigure does not kill a window, so the supervisor must notice the
// new topology itself and re-place the affected windows. Reading the list starts
// a short-lived Qt process, so this is a backstop: the launcher exits when its own
// output goes away, and this poll catches an added output or a geometry move.
const monitorPoll = 10 * time.Second

// listTimeout bounds one monitor read. A Wayland client can stall during a
// display reconfigure, and a hung read must not block stop.
const listTimeout = 5 * time.Second

// Supervisor runs one launcher window per monitor and keeps them alive. It
// re-reads the monitor list on a timer and re-places a window when its monitor
// changes, so a display reconfigure does not leave an orphaned surface drawing
// nothing.
type Supervisor struct {
	bin     string
	env     []string
	apiBase string
	layer   string
	backend string
	logf    func(string)
	list    func(context.Context) ([]Monitor, error)
	poll    time.Duration
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
	// Backend is layer-shell or x11. Empty is layer-shell.
	Backend string
	// Logf receives progress lines. Nil discards them.
	Logf func(string)
	// ListMonitors overrides how the supervisor reads the current monitors. Nil
	// asks the launcher for them. A test sets it to change the topology without
	// a real compositor. The supervisor cancels ctx on a timeout or on stop.
	ListMonitors func(ctx context.Context) ([]Monitor, error)
	// PollInterval is how often to re-read the monitors. Zero uses monitorPoll.
	PollInterval time.Duration
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
	backend := opts.Backend
	if backend == "" {
		backend = BackendLayerShell
	}
	list := opts.ListMonitors
	if list == nil {
		list = func(ctx context.Context) ([]Monitor, error) { return ListMonitors(ctx, opts.Binary, opts.Env) }
	}
	poll := opts.PollInterval
	if poll <= 0 {
		poll = monitorPoll
	}
	return &Supervisor{bin: opts.Binary, env: opts.Env, apiBase: opts.APIBase, layer: layer, backend: backend, logf: logf, list: list, poll: poll}
}

// window is one monitor's launcher process, kept alive by its own goroutine
// until the supervisor retires it or stop closes.
type window struct {
	monitor Monitor
	retire  chan struct{}
	done    chan struct{}
}

// Run starts a window per monitor and blocks until stop closes and every window
// has exited. An empty monitors list lists them from the launcher. A window that
// exits on its own is restarted after a short pause. When the compositor's
// monitors change, the affected windows are retired and relaunched against the
// new outputs. The caller owns signal handling and closes stop, so a signal
// never leaves a restart loop behind.
func (s *Supervisor) Run(monitors []Monitor, stop <-chan struct{}) error {
	if len(monitors) == 0 {
		var err error
		monitors, err = s.listMonitors(stop)
		if err != nil {
			return err
		}
	}
	if len(monitors) == 0 {
		return fmt.Errorf("the compositor reports no monitors")
	}

	desired := monitors
	running := map[string]*window{}
	defer func() {
		for _, w := range running {
			s.retire(w)
		}
	}()

	s.reconcile(desired, running, stop)

	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ticker.C:
			next, err := s.listMonitors(stop)
			if err != nil {
				s.logf("list monitors: " + err.Error())
				continue
			}
			// An empty list is a transient read during a reconfigure, not a
			// command to close every window. The next poll settles it.
			if len(next) == 0 || sameTopology(desired, next) {
				continue
			}
			s.logf(fmt.Sprintf("monitor topology changed (%s -> %s), re-placing views",
				topologyString(desired), topologyString(next)))
			desired = next
			s.reconcile(desired, running, stop)
		}
	}
}

// listMonitors reads the monitors, giving up after listTimeout or when stop
// closes.
func (s *Supervisor) listMonitors(stop <-chan struct{}) ([]Monitor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return s.list(ctx)
}

// reconcile makes the running windows match the monitor list: it retires a
// window whose monitor is gone or moved, and starts one for a new monitor.
func (s *Supervisor) reconcile(desired []Monitor, running map[string]*window, stop <-chan struct{}) {
	want, order := desiredWindows(desired)
	for key, w := range running {
		m, ok := want[key]
		if !ok || m != w.monitor {
			s.retire(w)
			delete(running, key)
		}
	}
	for _, key := range order {
		if _, ok := running[key]; ok {
			continue
		}
		running[key] = s.startWindow(want[key], stop)
	}
}

// desiredWindows keys each monitor by name, or by geometry when it has no name,
// so reconcile can match a window to its monitor across a re-read.
func desiredWindows(monitors []Monitor) (map[string]Monitor, []string) {
	want := make(map[string]Monitor, len(monitors))
	order := make([]string, 0, len(monitors))
	for _, m := range monitors {
		key := monitorKey(m)
		if _, ok := want[key]; ok {
			// Two outputs sharing a name still get their own window.
			key += "|" + m.Geometry
		}
		want[key] = m
		order = append(order, key)
	}
	return want, order
}

// monitorKey is a monitor's stable identity across re-reads.
func monitorKey(m Monitor) string {
	if m.Name != "" {
		return "name:" + m.Name
	}
	return "geom:" + m.Geometry
}

// startWindow launches a window's goroutine and returns its handle.
func (s *Supervisor) startWindow(m Monitor, stop <-chan struct{}) *window {
	w := &window{monitor: m, retire: make(chan struct{}), done: make(chan struct{})}
	go s.runWindow(w, stop)
	return w
}

// retire asks a window to stop and waits for its process to exit.
func (s *Supervisor) retire(w *window) {
	close(w.retire)
	<-w.done
}

// runWindow keeps one monitor's window running until stop closes or the
// supervisor retires it. A window that exits on its own is restarted, so a crash
// does not leave the wallpaper blank.
func (s *Supervisor) runWindow(w *window, stop <-chan struct{}) {
	defer close(w.done)
	for {
		if stopped(stop) || stopped(w.retire) {
			return
		}
		cmd := exec.Command(s.bin, LaunchArgs(s.bin, PageURL(s.apiBase, ScreenKey(w.monitor)), s.apiBase, w.monitor, s.layer, s.backend)...)
		cmd.Env = s.env
		// The launcher prints its screen, mask changes and errors on stderr, so
		// the server's log and the journal show what happened.
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			s.logf(fmt.Sprintf("view on %s: %v", monitorName(w.monitor), err))
			if pauseOrStop(stop, w.retire) {
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
		case <-w.retire:
			_ = cmd.Process.Signal(syscall.SIGTERM)
			<-exited
			return
		case err := <-exited:
			if stopped(stop) || stopped(w.retire) {
				return
			}
			s.logf(fmt.Sprintf("view on %s exited: %v, restarting", monitorName(w.monitor), err))
			if pauseOrStop(stop, w.retire) {
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

// sameTopology reports whether two monitor lists describe the same outputs and
// geometry, ignoring order.
func sameTopology(a, b []Monitor) bool {
	if len(a) != len(b) {
		return false
	}
	x := topologyKeys(a)
	y := topologyKeys(b)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// topologyKeys returns the sorted name+geometry of every monitor.
func topologyKeys(monitors []Monitor) []string {
	keys := make([]string, 0, len(monitors))
	for _, m := range monitors {
		keys = append(keys, m.Name+"="+m.Geometry)
	}
	sort.Strings(keys)
	return keys
}

// topologyString renders a monitor list for the log.
func topologyString(monitors []Monitor) string {
	return strings.Join(topologyKeys(monitors), ", ")
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

// pauseOrStop waits the restart pause, or returns true at once when stop or a
// retire request closed, so the caller returns instead of restarting.
func pauseOrStop(stop, retire <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	case <-retire:
		return true
	case <-time.After(restartPause):
		return false
	}
}
