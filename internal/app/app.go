// Package app wires config, Wine runtime, prefix and Studio builds
// together. Every Brine command that touches Wine goes through App so that
// Studio and the MCP helper always share one environment and wineserver.
package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"brine/internal/config"
	"brine/internal/dirs"
	"brine/internal/dxvk"
	"brine/internal/gpu"
	"brine/internal/logx"
	"brine/internal/studio"
	"brine/internal/wine"
)

type App struct {
	Cfg      *config.Config
	RT       *wine.Runtime
	Pfx      *wine.Prefix
	State    *studio.State
	Renderer string // resolved: dxvk, vulkan, d3d11 or opengl
}

// Open loads config and state and resolves the Wine runtime. A missing
// runtime is returned as an error alongside a usable App (RT nil) so
// diagnostics can still run.
func Open() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	st, err := studio.LoadState()
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, State: st}
	a.Renderer = ResolveRenderer(cfg.Graphics.Renderer)
	rt, err := wine.Resolve(cfg.Wine.Runtime, cfg.Wine.ProtonPath)
	if err != nil {
		return a, err
	}
	a.RT = rt
	a.Pfx = a.prefix()
	return a, nil
}

// ResolveRenderer turns "auto" into a concrete renderer.
func ResolveRenderer(r string) string {
	if r != "auto" {
		return r
	}
	if gpu.HasVulkan() {
		return "dxvk"
	}
	return "d3d11"
}

func (a *App) prefix() *wine.Prefix {
	p := wine.NewPrefix(dirs.Prefix(), a.RT)
	p.Debug = a.Cfg.Wine.Debug
	for k, v := range dxvk.Overrides(a.Renderer == "dxvk") {
		p.Overrides[k] = v
	}
	for k, v := range gpu.Env(gpu.Detect(), a.Cfg.Graphics.GPU) {
		p.Env[k] = v
	}
	p.Env["DXVK_LOG_LEVEL"] = "warn"
	p.Env["DXVK_LOG_PATH"] = "none"
	p.Env["DXVK_STATE_CACHE_PATH"] = filepath.Join(dirs.Cache, "dxvk")
	// Keep WebView2's GPU work in-process; its separate GPU process often
	// fails to start under Wine, which leaves sign-in and web panels blank.
	p.Env["WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"] = "--in-process-gpu"
	for k, v := range a.Cfg.Env {
		p.Env[k] = v
	}
	return p
}

// RequireRuntime returns an error if no Wine runtime was resolved.
func (a *App) RequireRuntime() error {
	if a.RT == nil {
		return wine.ErrNoRuntime
	}
	return nil
}

// BuildDir returns the active Studio build directory.
func (a *App) BuildDir() (string, error) { return a.State.CurrentDir() }

// StudioProcesses lists running Studio processes in Brine's prefix.
func (a *App) StudioProcesses() []wine.Process {
	if a.Pfx == nil {
		return nil
	}
	return a.Pfx.Processes(studio.StudioExe)
}

func (a *App) StudioRunning() bool { return len(a.StudioProcesses()) > 0 }

// PrepareBuild writes per-build settings (renderer fast flags) and makes
// sure the renderer's DLLs are installed.
func (a *App) PrepareBuild() error {
	dir, err := a.BuildDir()
	if err != nil {
		return err
	}
	if err := studio.WriteFFlags(dir, a.Renderer, a.Cfg.FFlags); err != nil {
		return fmt.Errorf("fast flags: %w", err)
	}
	if a.Renderer == "dxvk" && dxvk.Installed(a.Pfx.Dir) != a.Cfg.Graphics.DXVKVersion {
		fmt.Fprintf(os.Stderr, "Installing DXVK %s\n", a.Cfg.Graphics.DXVKVersion)
		if err := dxvk.Install(a.Pfx.Dir, a.Cfg.Graphics.DXVKVersion); err != nil {
			return fmt.Errorf("DXVK: %w", err)
		}
	}
	return nil
}

// StudioArgs converts `brine run` arguments into Studio's command line.
func StudioArgs(arg string) ([]string, error) {
	if arg == "" {
		return nil, nil
	}
	lower := strings.ToLower(arg)
	if strings.HasPrefix(lower, "roblox-studio:") || strings.HasPrefix(lower, "roblox-studio-auth:") {
		return []string{arg}, nil
	}
	if strings.HasPrefix(lower, "file://") {
		arg = strings.TrimPrefix(arg, "file://")
		arg = strings.ReplaceAll(arg, "%20", " ")
	}
	st, err := os.Stat(arg)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", arg, err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", arg)
	}
	switch strings.ToLower(filepath.Ext(arg)) {
	case ".rbxl", ".rbxlx":
		return []string{"-task", "EditFile", "-localPlaceFile", wine.WinPath(arg)}, nil
	}
	return nil, fmt.Errorf("%s: Studio opens .rbxl and .rbxlx places", arg)
}

// IsAuthURI reports whether arg is a sign-in callback for a running Studio.
func IsAuthURI(arg string) bool {
	return strings.HasPrefix(strings.ToLower(arg), "roblox-studio-auth:")
}

// StudioCommand builds the command that launches Studio with args.
func (a *App) StudioCommand(args []string) (*exec.Cmd, error) {
	dir, err := a.BuildDir()
	if err != nil {
		return nil, err
	}
	exe := filepath.Join(dir, studio.StudioExe)
	var cmd *exec.Cmd
	if d := a.Cfg.Studio.Desktop; d != "" && (len(args) == 0 || !IsAuthURI(args[0])) {
		cmd = a.Pfx.Command("explorer", append([]string{"/desktop=brine," + d, exe}, args...)...)
	} else {
		cmd = a.Pfx.Command(exe, args...)
	}
	cmd.Dir = dir
	if l := strings.Fields(a.Cfg.Studio.Launcher); len(l) > 0 {
		path, err := exec.LookPath(l[0])
		if err != nil {
			return nil, fmt.Errorf("studio.launcher: %w", err)
		}
		cmd.Args = append(append(l, cmd.Path), cmd.Args[1:]...)
		cmd.Path = path
	}
	return cmd, nil
}

// Launch starts Studio and waits for it to exit. Wine's output goes to
// studio.log (redacted); with verbose it is also copied to stderr.
func (a *App) Launch(args []string, verbose bool) error {
	cmd, err := a.StudioCommand(args)
	if err != nil {
		return err
	}
	f, err := logx.File("studio.log")
	if err != nil {
		return err
	}
	defer f.Close()
	var w io.Writer = f
	if verbose {
		w = io.MultiWriter(f, os.Stderr)
	}
	rw := &logx.RedactWriter{W: w}
	fmt.Fprintf(rw, "\n=== %s brine run %s\n", time.Now().Format(time.RFC3339), strings.Join(args, " "))
	cmd.Stdout = rw
	cmd.Stderr = rw
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}

// LaunchDetached starts Studio in the background in its own session so it
// outlives the caller (used by `brine mcp` autostart).
func (a *App) LaunchDetached() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "run")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
