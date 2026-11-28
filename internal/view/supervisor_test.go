package view

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSupervisorStartsOneWindowPerMonitor proves the supervisor runs the
// launcher once for each monitor it is given, each with that monitor's screen
// key, and stops every window when its stop channel closes. A fake launcher
// records its argv, so the test reads what the supervisor would run.
func TestSupervisorStartsOneWindowPerMonitor(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "argv")
	fake := filepath.Join(dir, "spinewall-view")
	// Append our argv, one monitor per line, then idle so the supervisor sees a
	// live window until stop closes. The trap answers SIGTERM so the supervisor's
	// stop reaches this process, as the real launcher's Qt loop would.
	script := "#!/bin/sh\ntrap 'exit 0' TERM\nprintf '%s\\n' \"$*\" >> " + record + "\nwhile true; do sleep 0.2; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	monitors := []Monitor{
		{Name: "DP-2", Geometry: "3440x1440+0+0"},
		{Name: "HDMI-A-1", Geometry: "1920x1080+3440+236"},
	}
	sup := NewSupervisor(SuperviseOptions{Binary: fake, APIBase: "http://127.0.0.1:8777", Env: os.Environ()})
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- sup.Run(monitors, stop) }()

	// Wait for both windows to record their argv.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if lines(t, record) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := lines(t, record); got != 2 {
		close(stop)
		t.Fatalf("recorded %d launcher runs, want 2", got)
	}

	// The two runs carry the two monitor keys, so each window draws its own stage.
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--screen 3440x1440+0+0", "--screen 1920x1080+3440+236", "--output DP-2", "--output HDMI-A-1"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("launcher argv missing %q:\n%s", want, data)
		}
	}

	close(stop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("supervisor returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop after its stop channel closed")
	}
}

func lines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// TestSupervisorRestartsACrashedWindow proves a window that exits on its own is
// started again, so a crash does not leave the wallpaper blank.
func TestSupervisorRestartsACrashedWindow(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "runs")
	fake := filepath.Join(dir, "spinewall-view")
	// Exit at once, so the supervisor must restart it.
	script := "#!/bin/sh\nprintf 'run\\n' >> " + record + "\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sup := NewSupervisor(SuperviseOptions{Binary: fake, APIBase: "http://x", Env: os.Environ()})
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- sup.Run([]Monitor{{Name: "DP-2", Geometry: "1x1+0+0"}}, stop) }()

	// The restart pause is 2 s, so within ~5 s there are two runs.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if lines(t, record) >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	if got := lines(t, record); got < 2 {
		t.Fatalf("crashed window was not restarted: %d run(s)", got)
	}
}

// TestSupervisorFollowsMonitorChange proves that when the compositor's monitor
// list changes after startup, the window whose monitor moved is retired and
// launched again against the new geometry, while an unchanged monitor's window
// is left running. A fake launcher idles until SIGTERM and records its argv, so
// the test reads how many times each screen key was launched. This is the
// regression for the blank wallpaper: a display reconfigure never exits a
// window, so a supervisor that only restarts on process exit leaves an orphaned
// surface drawing nothing.
func TestSupervisorFollowsMonitorChange(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "argv")
	fake := filepath.Join(dir, "spinewall-view")
	script := "#!/bin/sh\ntrap 'exit 0' TERM\nprintf '%s\\n' \"$*\" >> " + record + "\nwhile true; do sleep 0.2; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// The monitor source starts with the pre-reconfigure topology and is swapped
	// underneath the running supervisor.
	var mu sync.Mutex
	monitors := []Monitor{
		{Name: "DP-2", Geometry: "3440x1440+0+0"},
		{Name: "HDMI-A-1", Geometry: "1920x1080+3440+236"},
	}
	list := func(context.Context) ([]Monitor, error) {
		mu.Lock()
		defer mu.Unlock()
		return append([]Monitor(nil), monitors...), nil
	}

	sup := NewSupervisor(SuperviseOptions{
		Binary:       fake,
		APIBase:      "http://127.0.0.1:8777",
		Env:          os.Environ(),
		ListMonitors: list,
		PollInterval: 40 * time.Millisecond,
	})
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- sup.Run(nil, stop) }()
	defer func() {
		close(stop)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("supervisor did not stop")
		}
	}()

	// Both initial windows launch against their starting outputs.
	if !waitFor(t, 5*time.Second, func() bool {
		return screencount(t, record, "3440x1440+0+0") >= 1 && screencount(t, record, "1920x1080+3440+236") >= 1
	}) {
		t.Fatalf("initial windows did not launch:\n%s", readAll(record))
	}

	// DP-2 re-enumerates onto a new geometry; HDMI-A-1 does not change. The
	// supervisor must relaunch only the affected window.
	mu.Lock()
	monitors = []Monitor{
		{Name: "DP-2", Geometry: "2560x1440+0+0"},
		{Name: "HDMI-A-1", Geometry: "1920x1080+3440+236"},
	}
	mu.Unlock()

	if !waitFor(t, 5*time.Second, func() bool {
		return screencount(t, record, "2560x1440+0+0") >= 1
	}) {
		t.Fatalf("view did not relaunch on the new DP-2 geometry:\n%s", readAll(record))
	}

	// The unchanged monitor's window kept its single run: it was not torn down.
	if got := screencount(t, record, "1920x1080+3440+236"); got != 1 {
		t.Errorf("unchanged HDMI-A-1 window relaunched %d times, want 1:\n%s", got, readAll(record))
	}
	// The old DP-2 geometry is gone from the desired set, so the new window must
	// carry the new output name and geometry.
	if !strings.Contains(readAll(record), "--output DP-2 --geometry 2560x1440+0+0") {
		t.Errorf("relaunched DP-2 window did not carry the new geometry:\n%s", readAll(record))
	}
}

// TestSupervisorRemovesWindowForGoneMonitor proves that when a monitor leaves
// the compositor's list, its window is retired, so a surface for an output that
// no longer exists does not linger. Two monitors start; one is removed; the
// surviving monitor's window keeps its single run.
func TestSupervisorRemovesWindowForGoneMonitor(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "argv")
	terms := filepath.Join(dir, "terms")
	fake := filepath.Join(dir, "spinewall-view")
	// The fake records each launch on argv and each SIGTERM on terms, so the
	// test can prove the gone monitor's process was actually asked to exit,
	// which the pre-fix supervisor never did for a monitor that disappeared.
	script := "#!/bin/sh\n" +
		"trap 'printf \"%s\\n\" \"$*\" >> " + terms + "; exit 0' TERM\n" +
		"printf '%s\\n' \"$*\" >> " + record + "\n" +
		"while true; do sleep 0.2; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	monitors := []Monitor{
		{Name: "DP-2", Geometry: "3440x1440+0+0"},
		{Name: "HDMI-A-1", Geometry: "1920x1080+3440+236"},
	}
	list := func(context.Context) ([]Monitor, error) {
		mu.Lock()
		defer mu.Unlock()
		return append([]Monitor(nil), monitors...), nil
	}

	sup := NewSupervisor(SuperviseOptions{
		Binary:       fake,
		APIBase:      "http://127.0.0.1:8777",
		Env:          os.Environ(),
		ListMonitors: list,
		PollInterval: 40 * time.Millisecond,
	})
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- sup.Run(nil, stop) }()
	defer func() {
		close(stop)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("supervisor did not stop")
		}
	}()

	if !waitFor(t, 5*time.Second, func() bool {
		return screencount(t, record, "3440x1440+0+0") >= 1 && screencount(t, record, "1920x1080+3440+236") >= 1
	}) {
		t.Fatalf("initial windows did not launch:\n%s", readAll(record))
	}

	// HDMI-A-1 disappears; only DP-2 remains.
	mu.Lock()
	monitors = []Monitor{{Name: "DP-2", Geometry: "3440x1440+0+0"}}
	mu.Unlock()

	// The supervisor retires the gone monitor's window: its process must be
	// terminated, while the surviving DP-2 window keeps its single run.
	if !waitFor(t, 5*time.Second, func() bool {
		return screencount(t, terms, "1920x1080+3440+236") >= 1
	}) {
		t.Fatalf("gone HDMI-A-1 window was not retired:\nterms:\n%s\nruns:\n%s", readAll(terms), readAll(record))
	}
	if got := screencount(t, record, "1920x1080+3440+236"); got != 1 {
		t.Errorf("gone HDMI-A-1 window relaunched %d times, want 1:\n%s", got, readAll(record))
	}
	if got := screencount(t, record, "3440x1440+0+0"); got != 1 {
		t.Errorf("surviving DP-2 window ran %d times, want 1:\n%s", got, readAll(record))
	}
}

// TestSupervisorStopsDuringHungMonitorList proves a monitor read that never
// returns on its own does not block stop. A Wayland client can stall during a
// display reconfigure, and serve --with-view must still exit on SIGTERM.
func TestSupervisorStopsDuringHungMonitorList(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "spinewall-view")
	script := "#!/bin/sh\ntrap 'exit 0' TERM\nwhile true; do sleep 0.2; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	listing := make(chan struct{}, 1)
	list := func(ctx context.Context) ([]Monitor, error) {
		select {
		case listing <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	sup := NewSupervisor(SuperviseOptions{
		Binary:       fake,
		APIBase:      "http://127.0.0.1:8777",
		Env:          os.Environ(),
		ListMonitors: list,
		PollInterval: 20 * time.Millisecond,
	})
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- sup.Run([]Monitor{{Name: "DP-2", Geometry: "3440x1440+0+0"}}, stop) }()

	select {
	case <-listing:
	case <-time.After(5 * time.Second):
		close(stop)
		t.Fatal("supervisor never polled the monitors")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop while a monitor read hung")
	}
}

// TestSupervisorStartupReadHasNoDeadline proves the first monitor read gets no
// timeout, so a slow cold start at login still brings the view up, while a
// polled read is bounded and cannot block stop.
func TestSupervisorStartupReadHasNoDeadline(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "spinewall-view")
	script := "#!/bin/sh\ntrap 'exit 0' TERM\nwhile true; do sleep 0.2; done\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	type read struct{ deadline bool }
	reads := make(chan read, 16)
	list := func(ctx context.Context) ([]Monitor, error) {
		_, ok := ctx.Deadline()
		select {
		case reads <- read{deadline: ok}:
		default:
		}
		return []Monitor{{Name: "DP-2", Geometry: "3440x1440+0+0"}}, nil
	}
	sup := NewSupervisor(SuperviseOptions{
		Binary:       fake,
		APIBase:      "http://127.0.0.1:8777",
		Env:          os.Environ(),
		ListMonitors: list,
		PollInterval: 20 * time.Millisecond,
	})
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- sup.Run(nil, stop) }()
	defer func() {
		close(stop)
		<-done
	}()

	for i, want := range []bool{false, true} {
		select {
		case r := <-reads:
			if r.deadline != want {
				t.Errorf("read %d has deadline %v, want %v", i, r.deadline, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("read %d never happened", i)
		}
	}
}

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// screencount counts recorded launcher runs whose --screen key matches key.
func screencount(t *testing.T, path, key string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(readAll(path), "\n") {
		if strings.Contains(line, "--screen "+key+" ") || strings.HasSuffix(line, "--screen "+key) {
			n++
		}
	}
	return n
}

// readAll reads a record file, returning "" when it does not exist yet.
func readAll(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
