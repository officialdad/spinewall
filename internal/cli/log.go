package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// shortcutLogCap is the size at which the log restarts, so a log that grows
// without bound never fills the disk.
const shortcutLogCap = 102400

// logger forwards lines to the real stderr and, when there is no terminal,
// also appends each line with a timestamp to the shortcut log. A shortcut runs
// with no terminal, so an error would otherwise vanish.
type logger struct {
	real    io.Writer
	logPath string // empty disables the file
	mu      sync.Mutex
	file    *os.File
	partial []byte
}

// newLogger builds a logger. When logPath is empty it is a plain pass-through.
// Otherwise it mirrors every line to the log and rolls the log over past the cap.
func newLogger(real io.Writer, logPath string) *logger {
	l := &logger{real: real, logPath: logPath}
	if logPath != "" {
		// The directory exists even when no line is written, as the shell did.
		_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
		if info, err := os.Stat(logPath); err == nil && info.Size() > shortcutLogCap {
			_ = os.WriteFile(logPath, nil, 0o644)
		}
	}
	return l
}

// Write forwards p to the real stderr and logs each complete line.
func (l *logger) Write(p []byte) (int, error) {
	n, err := l.real.Write(p)
	if l.logPath == "" {
		return n, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.partial = append(l.partial, p...)
	for {
		idx := bytes.IndexByte(l.partial, '\n')
		if idx < 0 {
			break
		}
		line := string(l.partial[:idx])
		l.partial = l.partial[idx+1:]
		l.logLine(line)
	}
	return n, err
}

// logLine appends one timestamped line to the log, opening it on first use.
func (l *logger) logLine(line string) {
	if l.file == nil {
		f, err := os.OpenFile(l.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		l.file = f
	}
	stamp := time.Now().Format(time.RFC3339)
	fmt.Fprintf(l.file, "%s %s\n", stamp, line)
}

// close flushes any trailing partial line and closes the file.
func (l *logger) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	if len(l.partial) > 0 {
		l.logLine(string(l.partial))
		l.partial = nil
	}
	_ = l.file.Close()
	l.file = nil
}
