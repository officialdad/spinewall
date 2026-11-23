package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// startProcess runs a shell under a chosen argv, so the matcher sees the same
// /proc cmdline and environ a real process would show. sh reads the script
// named by argv[1], relative to dir unless it is absolute.
func startProcess(t *testing.T, port string, args ...string) *exec.Cmd {
	t.Helper()
	dir := t.TempDir()
	script := args[1]
	if !filepath.IsAbs(script) {
		script = filepath.Join(dir, script)
	}
	if err := os.WriteFile(script, []byte("sleep 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	cmd := &exec.Cmd{Path: sh, Args: args, Dir: dir, Env: []string{"PATH=" + os.Getenv("PATH"), "SPINEWALL_PORT=" + port}}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitArgs(t, cmd.Process.Pid, args[0])
	return cmd
}

func waitArgs(t *testing.T, pid int, argv0 string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if args := processArgs(pid); len(args) > 0 && args[0] == argv0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid %d never showed argv0 %s", pid, argv0)
}

func alive(cmd *exec.Cmd) bool {
	return syscall.Kill(cmd.Process.Pid, 0) == nil && processArgs(cmd.Process.Pid) != nil
}

func TestOnlySpinewallServeOnThePortMatches(t *testing.T) {
	serve := startProcess(t, "18901", "spinewall", "serve")
	other := startProcess(t, "18902", "/opt/x/spinewall", "serve")
	editor := startProcess(t, "18901", "nvim", "spinewall")
	client := startProcess(t, "18901", "spinewall", "next-model")

	if !isServeOnPort(serve.Process.Pid, "18901") {
		t.Error("spinewall serve on the port did not match")
	}
	if isServeOnPort(other.Process.Pid, "18901") {
		t.Error("a server on another port matched")
	}
	if !isServeOnPort(other.Process.Pid, "18902") {
		t.Error("a full-path spinewall serve did not match its own port")
	}
	if isServeOnPort(editor.Process.Pid, "18901") {
		t.Error("an editor naming spinewall matched")
	}
	if isServeOnPort(client.Process.Pid, "18901") {
		t.Error("a client command matched")
	}
}

func TestLegacyMatcherFindsOnlyThisHomesPythonServer(t *testing.T) {
	script := filepath.Join(t.TempDir(), "spinewall-server")
	other := filepath.Join(t.TempDir(), "spinewall-server")
	legacy := startProcess(t, "8777", "python3", script)
	elsewhere := startProcess(t, "8777", "python3", other)
	editor := startProcess(t, "8777", "nvim", script)
	if !isLegacyServer(legacy.Process.Pid, script) {
		t.Error("python3 spinewall-server in this home did not match")
	}
	if isLegacyServer(elsewhere.Process.Pid, script) {
		t.Error("a python server from another home matched")
	}
	if isLegacyServer(editor.Process.Pid, script) {
		t.Error("an editor on spinewall-server matched")
	}
}

func TestStopServerLeavesARecycledPidAlone(t *testing.T) {
	paths := testPaths(t)
	t.Setenv("SPINEWALL_PORT", "18903")
	// A stale pid file now names an unrelated process.
	stranger := startProcess(t, "18903", "nvim", "notes")
	if err := os.MkdirAll(paths.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PidFile(), []byte(strconv.Itoa(stranger.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopServer(paths, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !alive(stranger) {
		t.Fatal("StopServer signalled a pid that is not spinewall serve")
	}
	if _, err := os.Stat(paths.PidFile()); err == nil {
		t.Error("stale pid file kept")
	}
}

func TestStopServerStopsItsOwnServer(t *testing.T) {
	paths := testPaths(t)
	t.Setenv("SPINEWALL_PORT", "18904")
	serve := startProcess(t, "18904", "spinewall", "serve")
	if err := os.MkdirAll(paths.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PidFile(), []byte(strconv.Itoa(serve.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopServer(paths, func(string) {}); err != nil {
		t.Fatal(err)
	}
	_ = serve.Wait()
	if serve.ProcessState == nil || serve.ProcessState.Success() {
		t.Errorf("server was not stopped: %v", serve.ProcessState)
	}
}
