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
	"path/filepath"
	"strings"
	"time"
)

// run holds one invocation. Tests build it directly with a stand-in base URL.
type run struct {
	base     string
	stdout   io.Writer
	stderr   io.Writer
	client   *http.Client
	trap     bool
	resolver func(*run) (string, error)
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
		base:     "http://127.0.0.1:" + envOr("SPINEWALL_PORT", "8777"),
		stdout:   os.Stdout,
		stderr:   stderr,
		client:   &http.Client{Timeout: 30 * time.Second},
		trap:     trap,
		resolver: (*run).resolveCurrentScreen,
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
	path, hasScreen := routePath(command, positional)
	if hasScreen && screen == "current" {
		resolved, err := r.resolver(r)
		switch {
		case err == nil:
			screen = resolved
		case errors.Is(err, errScreenUnsupported):
			fmt.Fprintln(r.stderr, "spinewall: --screen current is not supported on this desktop, using the default stage.")
			screen = ""
		default:
			fmt.Fprintf(r.stderr, "error: %v\n", err)
			return r.fail(positional, screen, 1)
		}
	}

	query := ""
	if hasScreen && screen != "" {
		query = quote(screen)
	}
	status, body, err := r.get(path, query)
	if err != nil {
		fmt.Fprintf(r.stderr, "error: %v\n", err)
		return r.fail(positional, screen, 1)
	}
	if status < 200 || status >= 300 {
		fmt.Fprintf(r.stderr, "error: the server answered %d %s\n", status, http.StatusText(status))
		return r.fail(positional, screen, 1)
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
	}
	path = "/cmd/" + quote(command)
	// set-char-anim and set-char-model carry a uid and a value, which the server
	// reads as one uid/value pair.
	for _, value := range positional[1:] {
		path += "/" + quote(value)
	}
	return path, true
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
func (r *run) get(path, screenQuery string) (int, []byte, error) {
	req, err := newGet(r.base, path, screenQuery)
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
