package install

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// hasCommand reports whether a program is on PATH.
func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// runQuiet runs a command and discards its output. A missing command or a
// non-zero exit is not fatal; callers use it for best-effort steps.
func runQuiet(name string, args ...string) {
	cmd := exec.Command(name, args...)
	_ = cmd.Run()
}

// processArgs is a pid's argv. A process that is gone or not readable has none.
func processArgs(pid int) []string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil || len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
}

// processPort is the SPINEWALL_PORT a pid runs with, defaulting to 8777 like
// the server, so a test server on another port is never mistaken for this one.
func processPort(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return ""
	}
	for _, kv := range strings.Split(string(data), "\x00") {
		if value, ok := strings.CutPrefix(kv, "SPINEWALL_PORT="); ok && value != "" {
			return value
		}
	}
	return "8777"
}

// isServeOnPort reports whether pid is `spinewall serve` listening on port. An
// editor, a shell, or a tmux session that merely names spinewall never matches.
func isServeOnPort(pid int, port string) bool {
	args := processArgs(pid)
	if len(args) < 2 || filepath.Base(args[0]) != "spinewall" || args[1] != "serve" {
		return false
	}
	return processPort(pid) == port
}

// isLegacyServer reports whether pid is the old Python server, run directly or
// through a python interpreter.
func isLegacyServer(pid int) bool {
	args := processArgs(pid)
	if len(args) == 0 {
		return false
	}
	if filepath.Base(args[0]) == "spinewall-server" {
		return true
	}
	return len(args) > 1 && strings.HasPrefix(filepath.Base(args[0]), "python") &&
		filepath.Base(args[1]) == "spinewall-server"
}

// killMatching sends SIGTERM to every process of this user that match accepts.
func killMatching(match func(pid int) bool, logf func(string)) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self || !match(pid) {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
			logf(fmt.Sprintf("stopped process %d", pid))
		}
	}
}

// StopServer stops the spinewall server on this install's port. It reads the
// pid file first, then looks for the listening server, so a stale pid file
// never leaves the port busy. A pid is only signalled when it is still a
// `spinewall serve` on that port, so a recycled pid is left alone.
func StopServer(paths Paths, logf func(string)) error {
	port := envPort()
	if pid, ok := readPidFile(paths.PidFile()); ok {
		if isServeOnPort(pid, port) && syscall.Kill(pid, syscall.SIGTERM) == nil {
			logf(fmt.Sprintf("stopped server pid %d", pid))
			waitGone(pid, 2*time.Second)
		}
		_ = os.Remove(paths.PidFile())
	}
	// A server started by the systemd unit or a shell is not in the pid file.
	if portBusy("127.0.0.1", port) {
		// The unit runs on the default port, so a server on another port is never
		// the unit's.
		if port == "8777" && hasCommand("systemctl") {
			runQuiet("systemctl", "--user", "stop", "spinewall.service")
		}
		killMatching(func(pid int) bool { return isServeOnPort(pid, port) }, logf)
	}
	return nil
}

func readPidFile(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func waitGone(pid int, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// portBusy reports whether something is listening on the port.
func portBusy(host, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// envPort is the server port from SPINEWALL_PORT, defaulting to 8777.
func envPort() string {
	if p := os.Getenv("SPINEWALL_PORT"); p != "" {
		return p
	}
	return "8777"
}
