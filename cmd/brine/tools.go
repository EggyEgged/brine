package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"brine/internal/app"
	"brine/internal/config"
	"brine/internal/desktop"
	"brine/internal/dirs"
	"brine/internal/doctor"
	"brine/internal/mcp"
	"brine/internal/wine"
)

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
