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

// killByName stops every process whose executable base name matches or whose
// command line names it, which is how a hand-started python server is found
// without a process-tree walk.
func killByName(name string, logf func(string)) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		exe, _ := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if filepath.Base(exe) != name && !processCmdlineHas(pid, name) {
			continue
		}
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
			logf(fmt.Sprintf("stopped process %d (%s)", pid, name))
		}
	}
}

// processCmdlineHas reports whether a pid's command line mentions name, so a
// script run through an interpreter is still found.
func processCmdlineHas(pid int, name string) bool {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return false
	}
	for _, arg := range strings.Split(string(data), "\x00") {
		if filepath.Base(arg) == name {
			return true
		}
	}
	return false
}

// StopServer stops a spinewall server this installer or autostart started. It
// reads the pid file first, then falls back to the listening port, so a stale
// pid file never leaves the port busy.
func StopServer(paths Paths, logf func(string)) error {
	if pid, ok := readPidFile(paths.PidFile()); ok {
		if proc, err := os.FindProcess(pid); err == nil {
			if err := proc.Signal(syscall.SIGTERM); err == nil {
				logf(fmt.Sprintf("stopped server pid %d", pid))
			}
			waitGone(pid, 2*time.Second)
		}
		_ = os.Remove(paths.PidFile())
	}
	// A server started by the systemd unit or a shell is not in the pid file.
	if portBusy("127.0.0.1", envPort()) {
		if hasCommand("systemctl") {
			runQuiet("systemctl", "--user", "stop", "spinewall.service")
		}
		killByName("spinewall", logf)
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
