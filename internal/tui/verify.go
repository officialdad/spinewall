package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/officialdad/spinewall/internal/install"
	"github.com/officialdad/spinewall/internal/runtime"
)

// verifyThemeRuntime checks that every runtime file the cache holds is reachable
// from the theme folder. The KDE backend opens the page from file:// and the page
// links each player by relative URL, so a cache the theme folder cannot reach is
// what the wallpaper reports as "runtime not installed". The fetch step already
// reported an empty cache, so only a cached file with no link is a failure here.
func verifyThemeRuntime(paths install.Paths) error {
	var missing []string
	for _, name := range runtime.ServedNames() {
		if _, err := os.Stat(filepath.Join(paths.Runtime, name)); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(paths.ThemeDir(), name)); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the theme folder %s cannot load %s. Run 'spinewall setup' again to relink them", paths.ThemeDir(), strings.Join(missing, ", "))
	}
	return nil
}

// LicenseNotice is the Spine licence consent text. It reuses the README Legal
// notice wording so the wizard and the docs say the same thing.
func LicenseNotice() string {
	return strings.Join([]string{
		"Spinewall is a viewer. It includes no Spine runtime.",
		"The Spine players are fetched from Esoteric Software on this machine and never",
		"redistributed by Spinewall. Each user needs their own Spine Editor licence",
		"under the Spine Runtimes License (https://esotericsoftware.com/spine-runtimes-license).",
		"Use only models you own or have the right to use.",
	}, "\n")
}

// infoReply is the subset of /info the wizard reads to verify a start.
type infoReply struct {
	Models  int            `json:"models"`
	Skipped map[string]any `json:"skipped"`
	Input   map[string]int `json:"input"`
	// Legacy is true when the old Python server answered. Python's http.server
	// names itself in the Server header; the Go server sends none.
	Legacy bool `json:"-"`
}

// startAndVerify starts the server, waits for /info, and returns the model and
// skipped counts. It leaves the server running so the wallpaper can use it.
func startAndVerify(opts Options, logf func(string)) (models, skipped int, verified bool, err error) {
	executable := opts.Executable
	if executable == "" {
		executable, _ = os.Executable()
	}
	if executable == "" {
		return 0, 0, false, fmt.Errorf("cannot find the spinewall binary to start")
	}
	port := envOr("SPINEWALL_PORT", "8777")

	// If a server already answers, verify it instead of starting a second one.
	// The old Python server answers the same /info, so it is named, not passed.
	if reply, ok := queryInfo(port, 500*time.Millisecond); ok {
		if reply.Legacy {
			return 0, 0, false, fmt.Errorf("the old Python server answers on port %s. Run 'spinewall uninstall --legacy', then 'spinewall setup' again", port)
		}
		return reply.Models, len(reply.Skipped), true, nil
	}

	cmd := exec.Command(executable, "serve")
	cmd.Env = serverEnv(opts)
	// The server writes its pid so uninstall can stop it. Create the state dir
	// first; the server makes it too, but the pid file needs it to exist.
	_ = os.MkdirAll(opts.Paths.StateDir, 0o755)
	logFile, err := os.OpenFile(opts.Paths.StateDir+"/server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logFile = nil
	}
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		defer logFile.Close()
	}
	if err := cmd.Start(); err != nil {
		return 0, 0, false, fmt.Errorf("start the server: %w", err)
	}
	// Record the pid so StopServer can find it. The manifest records this file
	// when setup runs under install; here it is created directly and uninstall
	// removes the whole state dir when asked, or the file when present.
	_ = os.WriteFile(opts.Paths.PidFile(), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)

	// Detach: a setup run must not leave a child that dies with it. The process
	// keeps running after setup exits, so it becomes the wallpaper server.
	_ = cmd.Process.Release()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if reply, ok := queryInfo(port, 700*time.Millisecond); ok {
			logf(fmt.Sprintf("server answered on port %s: %d model(s), %d skipped", port, reply.Models, len(reply.Skipped)))
			return reply.Models, len(reply.Skipped), true, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return 0, 0, false, fmt.Errorf("the server did not answer /info on port %s", port)
}

// serverEnv builds the child server's environment from the chosen Paths, not
// the ambient HOME. A setup run under a throwaway HOME must start a server that
// reads that throwaway state and never the real one, so every SPINEWALL_* path
// is pinned explicitly.
func serverEnv(opts Options) []string {
	paths := opts.Paths
	// SPINEWALL_MODELS stays the default dir. A chosen extra folder is in
	// config.json, which the server reads for additional_model_dirs.
	env := []string{
		"HOME=" + paths.Home,
		"PATH=" + os.Getenv("PATH"),
		"SPINEWALL_PORT=" + envOr("SPINEWALL_PORT", "8777"),
		"SPINEWALL_MODELS=" + paths.ModelsDir(),
		"SPINEWALL_CONFIG=" + paths.Config,
		"SPINEWALL_STATE=" + filepath.Join(paths.StateDir, "state.json"),
		"SPINEWALL_PREFS=" + filepath.Join(paths.StateDir, "prefs.json"),
		"SPINEWALL_LAYOUT=" + filepath.Join(paths.StateDir, "layout.json"),
		"SPINEWALL_SCREENS=" + filepath.Join(paths.StateDir, "screens.json"),
		"SPINEWALL_THUMBS=" + filepath.Join(paths.CacheDir, "thumbs"),
		"SPINEWALL_RUNTIME_DIR=" + paths.Runtime,
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		env = append(env, "XDG_RUNTIME_DIR="+xdg)
	}
	if display := os.Getenv("DISPLAY"); display != "" {
		env = append(env, "DISPLAY="+display)
	}
	if opts.Offline {
		env = append(env, "SPINEWALL_OFFLINE=1")
	}
	return env
}

// queryInfo asks the server for /info. The timeout bounds one call so the wait
// loop can retry.
func queryInfo(port string, timeout time.Duration) (infoReply, bool) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://127.0.0.1:" + port + "/info")
	if err != nil {
		return infoReply{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return infoReply{}, false
	}
	var reply infoReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return infoReply{}, false
	}
	reply.Legacy = strings.Contains(resp.Header.Get("Server"), "Python")
	return reply, true
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
