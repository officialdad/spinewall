// Package cli is the spinewall command line tool. It talks to the local server
// over HTTP, exactly as the old bin/spinewall shell wrapper did, but in Go with
// no python3 and no curl.
package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Exit codes keep curl's, which the old shell wrapper passed through.
const (
	exitConnect = 7
	exitHTTP    = 22
)

// run holds one invocation. Tests build it directly with a stand-in base URL.
type run struct {
	base     string
	stdout   io.Writer
	stderr   io.Writer
	client   *http.Client
	trap     bool
	resolver func(*run) (string, error)
	// getenv and execCmd are injectable so a provider test never reads the real
	// session or runs a compositor command.
	getenv  func(string) string
	execCmd func(name string, args ...string) ([]byte, error)
	// kdeScreen is the KDE provider, injectable so a non-KDE test does not need
	// a session bus.
	kdeScreen func(*run) (string, error)
}

// env reads one environment variable, defaulting to the process environment.
func (r *run) env(key string) string {
	if r.getenv != nil {
		return r.getenv(key)
	}
	return os.Getenv(key)
}

// output runs a command and returns its stdout, defaulting to the real box.
func (r *run) output(name string, args ...string) ([]byte, error) {
	if r.execCmd != nil {
		return r.execCmd(name, args...)
	}
	return exec.Command(name, args...).Output()
}

// kde reads the active monitor from KWin, defaulting to the real D-Bus call.
func (r *run) kde() (string, error) {
	if r.kdeScreen != nil {
		return r.kdeScreen(r)
	}
	return r.kwinScreen()
}

// Run executes one invocation and returns the process exit code. A shortcut
// runs with no terminal, so the logger mirrors stderr to the shortcut log.
func Run(args []string) int {
	stderr := io.Writer(os.Stderr)
	trap := false
	var lg *logger
	if !isTerminal(os.Stderr) {
		logPath := os.Getenv("SPINEWALL_LOG")
		if logPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			logPath = filepath.Join(home, ".local", "state", "spinewall", "shortcut.log")
		}
		lg = newLogger(os.Stderr, logPath)
		defer lg.close()
		stderr = lg
		trap = true
	}
	r := &run{
		base:      "http://127.0.0.1:" + envOr("SPINEWALL_PORT", "8777"),
		stdout:    os.Stdout,
		stderr:    stderr,
		client:    &http.Client{Timeout: 30 * time.Second},
		trap:      trap,
		resolver:  (*run).resolveCurrentScreen,
		kdeScreen: (*run).kwinScreen,
	}
	return r.exec(args)
}

// exec parses the arguments, resolves --screen current, and sends the request.
func (r *run) exec(argv []string) int {
	positional, screen, code, ok := r.parseArgs(argv)
	if !ok {
		return code
	}
	command := "info"
	if len(positional) > 0 {
		command = positional[0]
	}
	if command == "edit" && (len(positional) > 2 || (len(positional) == 2 && positional[1] != "on" && positional[1] != "off")) {
		fmt.Fprintln(r.stderr, "error: edit takes at most one argument, on or off")
		return 2
	}
	path, hasScreen := routePath(command, positional)
	defaultStage := false
	if hasScreen && screen == "current" {
		resolved, err := r.resolver(r)
		switch {
		case err == nil:
			screen = resolved
		case errors.Is(err, errScreenUnsupported):
			fmt.Fprintln(r.stderr, "spinewall: --screen current is not supported on this desktop, using the default stage.")
			screen = ""
			defaultStage = true
		default:
			fmt.Fprintf(r.stderr, "error: %v\n", err)
			return r.fail(positional, screen, 1)
		}
	}

	query := ""
	if hasScreen && (screen != "" || defaultStage) {
		query = "screen=" + quote(screen)
	}
	status, body, err := r.get(path, query)
	if err != nil {
		fmt.Fprintf(r.stderr, "error: %v\n", err)
		return r.fail(positional, screen, exitConnect)
	}
	if status < 200 || status >= 300 {
		fmt.Fprintf(r.stderr, "error: the server answered %d %s\n", status, http.StatusText(status))
		return r.fail(positional, screen, exitHTTP)
	}
	_, _ = r.stdout.Write(body)
	fmt.Fprintln(r.stdout)
	return 0
}

// parseArgs splits --screen KEY from the positional arguments. It reports the
// positional args, the screen value, and a nonzero exit code when usage is wrong.
func (r *run) parseArgs(argv []string) (positional []string, screen string, code int, ok bool) {
	for i := 0; i < len(argv); i++ {
		if argv[i] == "--screen" {
			if i+1 >= len(argv) {
				fmt.Fprintln(r.stderr, "error: --screen needs a screen key. Run 'spinewall screens' to list them.")
				return nil, "", 2, false
			}
			screen = argv[i+1]
			i++
			continue
		}
		positional = append(positional, argv[i])
	}
	if len(positional) > 3 {
		fmt.Fprintf(r.stderr, "error: at most 3 arguments, got %d. Quote a name that has spaces.\n", len(positional))
		return nil, "", 2, false
	}
	return positional, screen, 0, true
}

// routePath builds the request path for a command. hasScreen is false for the
// plain reads, which never carry a screen key.
func routePath(command string, positional []string) (path string, hasScreen bool) {
	switch command {
	case "info":
		return "/info", false
	case "rescan":
		return "/rescan", false
	case "presets":
		return "/presets", false
	case "screens":
		return "/screens", false
	case "mask":
		// The native view writes each window's mask to its screen's stage.
		return "/mask", true
	case "edit":
		return editPath(positional[1:]), false
	}
	path = "/cmd/" + quote(command)
	// set-char-anim and set-char-model carry a uid and a value, which the server
	// reads as one uid/value pair.
	for _, value := range positional[1:] {
		path += "/" + quote(value)
	}
	return path, true
}

// editPath turns the optional on/off argument of `spinewall edit` into an edit
// path. exec has already rejected any other word. A bare `edit` toggles
// server-side, so a shortcut bound to it flips the flag whichever way it
// currently is.
func editPath(args []string) string {
	if len(args) == 0 {
		return "/edit/toggle"
	}
	if args[0] == "on" {
		return "/edit/1"
	}
	return "/edit/0"
}

// fail records the exit like the shell's ERR trap: a shortcut with no terminal
// gets a line naming the command, the screen and the status.
func (r *run) fail(positional []string, screen string, code int) int {
	if r.trap {
		s := screen
		if s == "" {
			s = "all"
		}
		fmt.Fprintf(r.stderr, "error: spinewall %s (screen: %s) exited with status %d\n", strings.Join(positional, " "), s, code)
	}
	return code
}

// get sends one GET and returns the status and body.
func (r *run) get(path, rawQuery string) (int, []byte, error) {
	req, err := newGet(r.base, path, rawQuery)
	if err != nil {
		return 0, nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// isTerminal reports whether f is a character device, which is how the shell's
// -t test tells a terminal from a pipe.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// envOr returns the variable, or def when it is empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
