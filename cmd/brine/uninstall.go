package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"brine/internal/app"
	"brine/internal/desktop"
	"brine/internal/dirs"
	"brine/internal/mcp"
	"brine/internal/netx"
)

// cmdUninstall removes what Brine put outside its own directories (desktop
// integration, MCP client registrations) and, with --purge, the directories too.
// It never deletes the brine binary: the package manager or install.sh owns that.
func cmdUninstall(args []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ExitOnError)
	purge := flags.Bool("purge", false, "also delete the Wine prefix (including your Studio sign-in), Studio builds, config, logs and caches")
	yes := flags.Bool("yes", false, "don't ask for confirmation")
	flags.Parse(args)

	var doomed []string
	if *purge {
		doomed = purgeTargets()
		fmt.Fprintln(os.Stderr, "This will permanently delete:")
		for _, d := range doomed {
			fmt.Fprintf(os.Stderr, "  %-45s %s\n", d, netx.Size(dirSize(d)))
		}
		fmt.Fprintln(os.Stderr, "Your Roblox sign-in lives in the Wine prefix and is lost with it. Your places and Studio settings stored in Roblox's cloud are not affected.")
	}
	if !*yes {
		if !isTerminal(os.Stdin) {
			return errors.New("not a terminal: pass --yes to uninstall without asking")
		}
		fmt.Fprint(os.Stderr, "Continue? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
			return errors.New("cancelled")
		}
	}

	lk, err := lockInstall()
	if err != nil {
		return err
	}
	defer lk.Release()

	if a, _ := app.Open(); a != nil && a.Pfx != nil {
		if a.Pfx.ServerRunning() {
			say("Stopping Studio")
			if err := a.Pfx.Kill(); err != nil {
				return err
			}
		}
	}

	if ok, err := mcp.DisconnectClaudeCode("user", os.Stderr); err != nil {
		say("Claude Code: %v", err)
	} else if ok {
		say("Removed %s from Claude Code (user scope)", mcp.ServerName)
	}
	if ok, backup, err := mcp.DisconnectClaudeDesktop(mcp.DesktopConfigPath()); err != nil {
		say("Claude Desktop: %v", err)
	} else if ok {
		say("Removed %s from Claude Desktop (backup: %s)", mcp.ServerName, backup)
	}
	desktop.Uninstall()
	say("Removed the desktop entry, MIME types and icon")
	if exe, err := os.Executable(); err == nil {
		home, _ := os.UserHomeDir()
		for _, p := range userExtraPaths(exe, home, dirs.XDGData, dirs.XDGConfig) {
			if err := os.Remove(p); err == nil {
				say("Removed %s", p)
			}
		}
	}

	if *purge {
		for _, d := range doomed {
			if err := removeAllSafe(d); err != nil {
				return err
			}
			say("Deleted %s", d)
		}
	} else {
		say("Kept %s (Wine prefix, Studio builds) and %s; use --purge to delete them", dirs.Data, dirs.Config)
	}
	if exe, err := os.Executable(); err == nil {
		say("To finish, remove the program itself: via your package manager, or `rm %s`", exe)
	}
	say("Project-scoped Claude Code entries can't be found automatically; if you added one, run `claude mcp remove %s` in that project", mcp.ServerName)
	return nil
}

func purgeTargets() []string {
	var out []string
	for _, d := range []string{dirs.Data, dirs.Config, dirs.State, dirs.Cache} {
		if _, err := os.Lstat(d); err == nil {
			out = append(out, d)
		}
	}
	return out
}

// removeAllSafe deletes one of Brine's own directories and refuses anything
// that doesn't look like one, so a bad XDG variable can't turn --purge into
// a delete of the wrong tree.
func removeAllSafe(path string) error {
	home, _ := os.UserHomeDir()
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || filepath.Base(clean) != "brine" {
		return fmt.Errorf("refusing to delete %q: not a brine directory", path)
	}
	for _, bad := range []string{"/", home, filepath.Dir(home)} {
		if clean == bad {
			return fmt.Errorf("refusing to delete %q", path)
		}
	}
	return os.RemoveAll(clean)
}

func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// userExtraPaths lists the man page and shell completions that install.sh
// puts in the user's directories, so uninstall can take them back out. For an
// install outside the home directory (a distribution package) it returns
// nothing: the package manager owns those files.
func userExtraPaths(exe, home, dataHome, configHome string) []string {
	if home == "" || !strings.HasPrefix(exe, home+string(os.PathSeparator)) {
		return nil
	}
	prefix := filepath.Dir(filepath.Dir(exe))
	return []string{
		filepath.Join(prefix, "share", "man", "man1", "brine.1"),
		filepath.Join(prefix, "share", "zsh", "site-functions", "_brine"),
		filepath.Join(dataHome, "bash-completion", "completions", "brine"),
		filepath.Join(configHome, "fish", "completions", "brine.fish"),
	}
}
