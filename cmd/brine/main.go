// Command brine installs, updates and runs Roblox Studio under Wine on
// Linux, and bridges Studio's built-in MCP server to Linux MCP clients.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"brine/internal/app"
	"brine/internal/config"
	"brine/internal/desktop"
	"brine/internal/dirs"
	"brine/internal/doctor"
	"brine/internal/logx"
	"brine/internal/mcp"
	"brine/internal/studio"
	"brine/internal/webview"
	"brine/internal/wine"
)

// Version is set at build time with -ldflags "-X main.Version=...".
var Version = "dev"

const usage = `brine - run Roblox Studio on Linux under Wine, with MCP support

Usage:
  brine setup [--runtime auto|system|managed|PATH] [--yes]
                               first-run wizard: Wine, prefix, Studio, desktop entries
  brine install [--force]      install the current Studio build
  brine update                 update Studio (the previous build is kept)
  brine rollback               switch back to the previous Studio build
  brine run [FILE|URI] [-v]    launch Studio; open a .rbxl/.rbxlx or a roblox-studio(-auth): URI
  brine kill                   stop Studio and its wineserver
  brine mcp                    stdio MCP server for MCP clients (Claude Code, Claude Desktop, ...)
  brine mcp --print-config     print client configuration for manual setup
  brine mcp connect claude-code [--scope user|local|project]
  brine mcp connect claude-desktop
  brine mcp enable             turn on Studio's "Enable Studio as MCP server" (Studio closed)
  brine mcp test [--luau CODE] round-trip list_roblox_studios (and optionally execute_luau)
  brine doctor [--quick]       check everything and suggest fixes
  brine runtime update         download/update the managed Wine build
  brine desktop install|remove (re)install or remove .desktop/MIME/URI handlers
  brine winecfg|regedit|wine ARGS...   run Wine tools in Brine's prefix
  brine config                 show config file location and effective settings
  brine version

Files:
  config  ` + "~/.config/brine/config.toml" + `
  data    ` + "~/.local/share/brine/  (prefix/, versions/, runtimes/)" + `
  logs    ` + "~/.local/state/brine/  (studio.log, mcp.log, helper.log)" + `
`

func main() {
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "brine: refusing to run as root; run it as your normal user")
		os.Exit(1)
	}
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "setup":
		err = cmdSetup(args)
	case "install":
		err = cmdInstall(args, false)
	case "update":
		err = cmdInstall(args, true)
	case "rollback":
		err = cmdRollback()
	case "run":
		err = cmdRun(args)
	case "kill":
		err = cmdKill()
	case "mcp":
		err = cmdMCP(args)
	case "doctor":
		err = cmdDoctor(args)
	case "runtime":
		err = cmdRuntime(args)
	case "desktop":
		err = cmdDesktop(args)
	case "winecfg", "regedit", "wine":
		err = cmdWineTool(cmd, args)
	case "config":
		err = cmdConfig()
	case "version", "--version", "-V":
		fmt.Println("brine", Version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		// Desktop environments may pass a URI or file directly.
		if strings.Contains(cmd, ":") || strings.HasSuffix(cmd, ".rbxl") || strings.HasSuffix(cmd, ".rbxlx") {
			err = cmdRun(os.Args[1:])
			break
		}
		fmt.Fprintf(os.Stderr, "brine: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "brine:", err)
		os.Exit(1)
	}
}

func say(format string, args ...any) { fmt.Fprintf(os.Stderr, "==> "+format+"\n", args...) }

func openApp() (*app.App, error) {
	if err := dirs.Ensure(); err != nil {
		return nil, err
	}
	a, err := app.Open()
	if err != nil {
		if a == nil {
			return nil, err
		}
		return a, err
	}
	return a, nil
}

func requirePrefix(a *app.App) error {
	if !a.Pfx.Exists() {
		return errors.New("the Wine prefix is not set up: run `brine setup`")
	}
	return nil
}

// ---- setup ----

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	runtimeFlag := fs.String("runtime", "", "Wine runtime: auto, system, managed, or a path")
	yes := fs.Bool("yes", false, "accept defaults without asking")
	fs.Parse(args)

	if err := dirs.Ensure(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	_, statErr := os.Stat(dirs.ConfigFile())
	firstRun := os.IsNotExist(statErr)
	interactive := !*yes && isTerminal(os.Stdin)

	say("Brine setup")
	choice := *runtimeFlag
	if choice == "" && firstRun {
		choice = chooseRuntime(interactive)
	}
	if choice != "" {
		cfg.Wine.Runtime = choice
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	say("Config: %s (wine runtime: %s)", dirs.ConfigFile(), cfg.Wine.Runtime)

	if cfg.Wine.Runtime == "managed" || (cfg.Wine.Runtime == "auto" && !hasSystemWine()) {
		if _, err := wine.InstallManaged(func(f string, a ...any) { say(f, a...) }); err != nil {
			return fmt.Errorf("managed Wine: %w", err)
		}
	}

	a, err := openApp()
	if err != nil {
		return err
	}
	v, err := a.RT.Version()
	if err != nil {
		return fmt.Errorf("%s does not run: %w", a.RT.Wine, err)
	}
	say("Wine: %s (%s)", v, a.RT)

	if !a.Pfx.Exists() {
		say("Creating Wine prefix at %s (this takes a minute)", a.Pfx.Dir)
	} else {
		say("Updating Wine prefix at %s", a.Pfx.Dir)
	}
	if a.StudioRunning() {
		say("Studio is running; skipping prefix update")
	} else if err := a.Pfx.Init(); err != nil {
		return fmt.Errorf("prefix: %w", err)
	}

	rel, err := studio.Latest()
	if err != nil {
		return err
	}
	if err := studio.Install(a.State, rel, a.Cfg.Studio.KeepVersions, func(f string, x ...any) { say(f, x...) }); err != nil {
		return err
	}
	say("Studio %s installed", rel.Version)

	if wv := a.Cfg.Studio.WebView2; wv != "" {
		say("Installing WebView2 runtime %s (Studio's sign-in window)", wv)
		if err := webview.Install(a.Pfx, wv); err != nil {
			say("WebView2 failed: %v", err)
			say("Studio still works; sign in with the browser option instead.")
		}
	}
	if err := a.PrepareBuild(); err != nil {
		return err
	}
	if err := a.Pfx.Wait(time.Minute); err != nil {
		say("warning: %v", err)
	}

	exe, _ := mcp.ServerCommand()
	for _, e := range desktop.Install(exe) {
		say("desktop integration warning: %v", e)
	}
	say("Desktop entry, MIME types and roblox-studio(-auth): handlers installed")

	fmt.Fprint(os.Stderr, `
Setup complete. Next steps:
  1. brine run                         start Studio and sign in
  2. In Studio: Assistant → … → Manage MCP Servers → "Enable Studio as MCP server"
     (or close Studio and run: brine mcp enable)
  3. brine mcp connect claude-code     and/or: brine mcp connect claude-desktop
  4. brine doctor                      verify everything, including an MCP round trip
`)
	return nil
}

func hasSystemWine() bool {
	_, err := wine.Resolve("system", "")
	return err == nil
}

func chooseRuntime(interactive bool) string {
	sys, sysErr := wine.Resolve("system", "")
	def := "managed"
	if sysErr == nil {
		def = "system"
	}
	if !interactive {
		return def
	}
	fmt.Fprintln(os.Stderr, "\nWhich Wine should Brine use?")
	if sysErr == nil {
		v, _ := sys.Version()
		fmt.Fprintf(os.Stderr, "  [s] system Wine (%s, %s)\n", v, sys.Wine)
	} else {
		fmt.Fprintln(os.Stderr, "  [s] system Wine (not installed)")
	}
	fmt.Fprintln(os.Stderr, "  [m] download a Wine build tuned for Studio into ~/.local/share/brine (~30 MB)")
	fmt.Fprintln(os.Stderr, "  [c] a custom Wine/Proton build directory, wine binary, or umu-run")
	fmt.Fprintf(os.Stderr, "Choice [%c]: ", def[0])
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s":
		return "system"
	case "m":
		return "managed"
	case "c":
		fmt.Fprint(os.Stderr, "Path: ")
		p, _ := r.ReadString('\n')
		return strings.TrimSpace(p)
	}
	return def
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// ---- install / update / rollback ----

func cmdInstall(args []string, isUpdate bool) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	force := fs.Bool("force", false, "re-download even if up to date")
	fs.Parse(args)
	a, err := openApp()
	if a == nil {
		return err
	}
	rel, err := studio.Latest()
	if err != nil {
		return err
	}
	if a.State.Current == rel.GUID && studio.Complete(studio.VersionDir(rel.GUID)) && !*force {
		say("Studio %s is up to date", rel.Version)
		return nil
	}
	if *force {
		os.RemoveAll(studio.VersionDir(rel.GUID))
	}
	if a.StudioRunning() {
		say("Studio is running; the new build will be used next launch")
	}
	if err := studio.Install(a.State, rel, a.Cfg.Studio.KeepVersions, func(f string, x ...any) { say(f, x...) }); err != nil {
		return err
	}
	if a.Pfx != nil {
		if err := a.PrepareBuild(); err != nil {
			return err
		}
	}
	if isUpdate && a.State.Previous != "" {
		say("Updated to %s; `brine rollback` returns to %s", rel.Version, a.State.Previous)
	} else {
		say("Studio %s installed", rel.Version)
	}
	return nil
}

func cmdRollback() error {
	a, err := openApp()
	if a == nil {
		return err
	}
	if a.StudioRunning() {
		return errors.New("close Studio first (or run `brine kill`)")
	}
	if err := studio.Rollback(a.State); err != nil {
		return err
	}
	a.Cfg.Studio.AutoUpdate = false
	if err := a.Cfg.Save(); err != nil {
		return err
	}
	say("Now using %s (%s). Automatic updates are off; run `brine update` to go forward again.",
		a.State.Versions[a.State.Current].Version, a.State.Current)
	return nil
}

// ---- run / kill ----

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	verbose := fs.Bool("v", false, "copy Wine output to the terminal")
	fs.Parse(args)
	a, err := openApp()
	if err != nil {
		return err
	}
	if err := requirePrefix(a); err != nil {
		return err
	}
	target := strings.Join(fs.Args(), " ")
	studioArgs, err := app.StudioArgs(target)
	if err != nil {
		return err
	}
	logx.Setup("brine.log", *verbose)
	logx.Printf("run %s", target)

	// Sign-in callbacks must reach the running Studio quickly; skip updates.
	if !app.IsAuthURI(target) && !a.StudioRunning() && a.Cfg.Studio.AutoUpdate {
		if rel, err := studio.Latest(); err != nil {
			say("Could not check for Studio updates: %v", err)
		} else if rel.GUID != a.State.Current {
			say("Updating Studio to %s", rel.Version)
			if err := studio.Install(a.State, rel, a.Cfg.Studio.KeepVersions, func(f string, x ...any) { say(f, x...) }); err != nil {
				say("Update failed, starting the installed build: %v", err)
			}
		}
	}
	if _, err := a.BuildDir(); err != nil {
		return err
	}
	if !app.IsAuthURI(target) {
		if err := a.PrepareBuild(); err != nil {
			return err
		}
	}
	err = a.Launch(studioArgs, *verbose)
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// Studio's exit code is not meaningful to the user (a sign-in
		// hand-off to a running instance exits non-zero, for example).
		logx.Printf("Studio exited: %v", err)
		return nil
	}
	return err
}

func cmdKill() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	n := len(a.StudioProcesses())
	if !a.Pfx.ServerRunning() {
		say("Nothing to stop")
		return nil
	}
	if err := a.Pfx.Kill(); err != nil {
		return err
	}
	say("Stopped %d Studio process(es) and the prefix's wineserver", n)
	return nil
}

// ---- mcp ----

func cmdMCP(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "connect":
			return cmdMCPConnect(args[1:])
		case "enable":
			return cmdMCPEnable()
		case "test":
			return cmdMCPTest(args[1:])
		}
	}
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	printCfg := fs.Bool("print-config", false, "print client configuration")
	verbose := fs.Bool("v", false, "also log to stderr")
	debug := fs.Bool("debug", false, "run the helper with verbose logging")
	fs.Parse(args)
	if *printCfg {
		mcp.PrintConfig(os.Stdout)
		return nil
	}
	return serveMCP(*verbose, *debug)
}

func serveMCP(verbose, debug bool) error {
	logw, closeLog := logx.Setup("mcp.log", verbose)
	defer closeLog()
	logx.Printf("brine %s mcp starting (pid %d)", Version, os.Getpid())

	// Open errors (missing runtime, bad config) become in-band MCP errors.
	a, openErr := openApp()
	helperLog := &prefixWriter{prefix: "helper: ", w: logw}

	b := &mcp.Bridge{
		In:        os.Stdin,
		Out:       os.Stdout,
		Log:       logx.Logger,
		HelperLog: helperLog,
		Version:   Version,
		NewHelper: func() (*exec.Cmd, error) {
			if openErr != nil {
				return nil, openErr
			}
			if err := requirePrefix(a); err != nil {
				return nil, err
			}
			dir, err := a.BuildDir()
			if err != nil {
				return nil, err
			}
			helperArgs := []string{}
			if debug {
				helperArgs = append(helperArgs, "--verbose")
			}
			cmd := a.Pfx.Command(filepath.Join(dir, studio.MCPExe), helperArgs...)
			cmd.Dir = dir
			return cmd, nil
		},
	}
	if a != nil && openErr == nil {
		b.StudioRunning = a.StudioRunning
		b.StartTimeout = time.Duration(a.Cfg.MCP.StartTimeout) * time.Second
		b.ListTimeout = time.Duration(a.Cfg.MCP.ListTimeout) * time.Second
		if a.Cfg.MCP.AutostartStudio {
			b.Autostart = a.LaunchDetached
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-sig
		logx.Printf("received %s, shutting down", s)
		b.Shutdown()
		os.Exit(0)
	}()
	signal.Ignore(syscall.SIGPIPE)

	err := b.Run()
	logx.Printf("stdin closed; exiting")
	return err
}

func cmdMCPConnect(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: brine mcp connect claude-code|claude-desktop")
	}
	switch args[0] {
	case "claude-code":
		fs := flag.NewFlagSet("connect", flag.ExitOnError)
		scope := fs.String("scope", "user", "Claude Code scope: user, local or project")
		fs.Parse(args[1:])
		if err := mcp.ConnectClaudeCode(*scope, os.Stderr); err != nil {
			return err
		}
		say("Claude Code will start `brine mcp` as %s. Check with: claude mcp list", mcp.ServerName)
	case "claude-desktop":
		path := mcp.DesktopConfigPath()
		backup, err := mcp.ConnectClaudeDesktop(path)
		if err != nil {
			return err
		}
		if backup != "" {
			say("Backed up %s to %s", path, backup)
		}
		say("Added %s to %s; restart Claude Desktop to pick it up", mcp.ServerName, path)
	default:
		return fmt.Errorf("unknown client %q (supported: claude-code, claude-desktop)", args[0])
	}
	return nil
}

func cmdMCPEnable() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	if a.StudioRunning() {
		return errors.New("close Studio first (it rewrites its settings on exit), or toggle it in Studio: Assistant → … → Manage MCP Servers")
	}
	users, err := studio.EnableMCP(a.Pfx.LocalAppData())
	if err != nil {
		return err
	}
	say("Enabled \"Studio as MCP server\" for user(s) %s", strings.Join(users, ", "))
	return nil
}

func cmdMCPTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	luau := fs.String("luau", "", "also run this code with execute_luau")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for Studio to connect")
	fs.Parse(args)
	exe, _ := mcp.ServerCommand()
	s, _, err := doctor.Start(exe, 60*time.Second)
	if err != nil {
		return err
	}
	defer s.Close()
	say("initialize: ok")
	var text string
	var studios []map[string]any
	deadline := time.Now().Add(*wait)
	for {
		var isErr bool
		text, isErr, err = s.CallTool("list_roblox_studios", map[string]any{}, 30*time.Second)
		if err != nil {
			return err
		}
		if isErr {
			return errors.New(text)
		}
		studios, _ = mcp.ParseStudios([]byte(fmt.Sprintf(`{"content":[{"type":"text","text":%q}]}`, text)))
		if len(studios) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	fmt.Println(text)
	if len(studios) == 0 {
		return errors.New("no Studio connected: open a place in Studio and enable \"Studio as MCP server\"")
	}
	if *luau != "" {
		id := fmt.Sprint(studios[0]["id"])
		out, isErr, err := s.CallTool("execute_luau", map[string]any{"studio_id": id, "code": *luau, "datamodel_type": "Edit"}, 60*time.Second)
		if err != nil {
			return err
		}
		fmt.Println(out)
		if isErr {
			return errors.New("execute_luau reported an error")
		}
	}
	if len(s.Stray) > 0 {
		return fmt.Errorf("stdout carried %d non-JSON-RPC line(s): %q", len(s.Stray), s.Stray[0])
	}
	return nil
}

// ---- doctor / misc ----

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	quick := fs.Bool("quick", false, "skip the MCP round trip")
	fs.Parse(args)
	dirs.Ensure()
	a, err := app.Open()
	if a == nil {
		return err
	}
	fmt.Printf("brine %s doctor\n", Version)
	if fails := doctor.Run(a, err, os.Stdout, *quick); fails > 0 {
		return fmt.Errorf("%d check(s) failed", fails)
	}
	return nil
}

func cmdRuntime(args []string) error {
	if len(args) == 0 || args[0] != "update" {
		return errors.New("usage: brine runtime update")
	}
	dirs.Ensure()
	tag, err := wine.InstallManaged(func(f string, a ...any) { say(f, a...) })
	if err != nil {
		return err
	}
	say("Managed Wine build: %s (used when [wine] runtime is \"managed\", or \"auto\" without system Wine)", tag)
	return nil
}

func cmdDesktop(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: brine desktop install|remove")
	}
	switch args[0] {
	case "install":
		exe, _ := mcp.ServerCommand()
		for _, e := range desktop.Install(exe) {
			say("warning: %v", e)
		}
		say("Installed %s", desktop.DesktopPath())
	case "remove":
		desktop.Uninstall()
		say("Removed Brine's desktop entry, MIME types and icon")
	default:
		return errors.New("usage: brine desktop install|remove")
	}
	return nil
}

func cmdWineTool(tool string, args []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	if tool == "wine" {
		if len(args) == 0 {
			return errors.New("usage: brine wine PROGRAM [ARGS...]")
		}
		tool, args = args[0], args[1:]
	}
	cmd := a.Pfx.Command(tool, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func cmdConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n", dirs.ConfigFile())
	fmt.Printf("# data: %s\n# logs: %s\n# resolved renderer: %s\n\n", dirs.Data, dirs.State, app.ResolveRenderer(cfg.Graphics.Renderer))
	tmp := filepath.Join(dirs.Cache, "config-view.toml")
	os.MkdirAll(dirs.Cache, 0o755)
	if err := cfg.SaveFile(tmp); err != nil {
		return err
	}
	b, _ := os.ReadFile(tmp)
	os.Remove(tmp)
	fmt.Print(string(b))
	return nil
}

// prefixWriter prefixes each line written to it (helper stderr → log).
type prefixWriter struct {
	prefix string
	w      io.Writer
	mu     sync.Mutex
	buf    []byte
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimRight(p.buf[:i], "\r")
		p.w.Write([]byte(p.prefix + string(line) + "\n"))
		p.buf = p.buf[i+1:]
	}
	return len(b), nil
}
