package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/officialdad/spinewall/internal/view"
)

// TestServeWithViewStopsOnSIGTERM proves a SIGTERM, which setup and uninstall
// send through StopServer, ends `serve --with-view` as a whole: the window stops
// without a restart and the port is free for the next server. A fake launcher
// stands in for the Qt build, placed where view.Build keeps an up-to-date binary.
func TestServeWithViewStopsOnSIGTERM(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "spinewall")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	home := filepath.Join(tmp, "home")
	cache := filepath.Join(tmp, "cache")
	viewDir := filepath.Join(cache, "spinewall", "view")
	if err := os.MkdirAll(viewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(viewDir, "launcher.cpp"), view.LauncherSource, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(viewDir, "view.qml"), view.SurfaceQML, 0o644); err != nil {
		t.Fatal(err)
	}
	starts := filepath.Join(tmp, "starts")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --list-screens ]; then printf 'DP-2\\t1x1+0+0\\n'; exit 0; fi\n" +
		"trap 'exit 0' TERM\n" +
		"echo $$ >> " + starts + "\n" +
		"while true; do sleep 0.1; done\n"
	launcher := filepath.Join(viewDir, "spinewall-view")
	if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(launcher, future, future); err != nil {
		t.Fatal(err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(l.Addr().String())
	l.Close()

	serve := exec.Command(bin, "serve", "--with-view")
	serve.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CACHE_HOME="+cache,
		"SPINEWALL_PORT="+port,
		"SPINEWALL_OFFLINE=1",
		"SPINEWALL_MODELS="+filepath.Join(tmp, "models"),
	)
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- serve.Wait() }()
	t.Cleanup(func() { _ = serve.Process.Kill() })

	deadline := time.Now().Add(10 * time.Second)
	for launcherStarts(t, starts) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the view never started its window")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := serve.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("serve exited with %v, want 0", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve --with-view kept running after SIGTERM")
	}

	if n := launcherStarts(t, starts); n != 1 {
		t.Errorf("the window started %d times, want 1 with no restart", n)
	}
	free, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("port %s still held after SIGTERM: %v", port, err)
	}
	free.Close()
}

func launcherStarts(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if _, err := strconv.Atoi(line); err == nil {
			n++
		}
	}
	return n
}
