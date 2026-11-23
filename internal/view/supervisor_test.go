package view

import (
	"os"
	"path/filepath"
	"strings"
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
