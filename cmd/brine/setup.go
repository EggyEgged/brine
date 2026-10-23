package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"brine/internal/config"
	"brine/internal/desktop"
	"brine/internal/dirs"
	"brine/internal/mcp"
	"brine/internal/studio"
	"brine/internal/webview"
	"brine/internal/wine"
)

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	runtimeFlag := fs.String("runtime", "", "Wine runtime: auto, system, managed, or a path")
	yes := fs.Bool("yes", false, "accept defaults without asking")
	fs.Parse(args)

	if err := dirs.Ensure(); err != nil {
		return err
	}
	lk, err := lockInstall()
	if err != nil {
		return err
	}
	defer lk.Release()
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
	changed := choice != "" && choice != cfg.Wine.Runtime
	if changed {
		cfg.Wine.Runtime = choice
	}
	// Only write the file when there is something new to record, so a
	// hand-edited config (with its comments) survives re-running setup.
	if firstRun || changed {
		if err := cfg.Save(); err != nil {
			return err
		}
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
