// Command brine installs, updates and runs Roblox Studio under Wine on
// Linux, and bridges Studio's built-in MCP server to Linux MCP clients.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"brine/internal/app"
	"brine/internal/dirs"
	"brine/internal/lock"
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
  brine mcp disconnect claude-code|claude-desktop
  brine mcp enable             turn on Studio's "Enable Studio as MCP server" (Studio closed)
  brine mcp test [--luau CODE] round-trip list_roblox_studios (and optionally execute_luau)
  brine doctor [--quick]       check everything and suggest fixes
  brine runtime update         download/update the managed Wine build
  brine desktop install|remove (re)install or remove .desktop/MIME/URI handlers
  brine winecfg|regedit|wine ARGS...   run Wine tools in Brine's prefix
  brine config                 show config file location and effective settings
  brine uninstall [--purge] [--yes]
                               remove desktop integration and MCP registrations;
                               --purge also deletes the prefix (and your Studio sign-in), builds and caches
  brine completion bash|zsh|fish
                               print a shell completion script
  brine version

Files:
  config  ~/.config/brine/config.toml
  data    ~/.local/share/brine/  (prefix/, versions/, runtimes/)
  logs    ~/.local/state/brine/  (studio.log, mcp.log, brine.log)

Docs: man brine, or the README.
`

// commands maps each subcommand to its implementation.
var commands map[string]func(args []string) error

func init() {
	commands = map[string]func(args []string) error{
		"setup":      cmdSetup,
		"install":    func(a []string) error { return cmdInstall(a, false) },
		"update":     func(a []string) error { return cmdInstall(a, true) },
		"rollback":   func([]string) error { return cmdRollback() },
		"run":        cmdRun,
		"kill":       func([]string) error { return cmdKill() },
		"mcp":        cmdMCP,
		"doctor":     cmdDoctor,
		"runtime":    cmdRuntime,
		"desktop":    cmdDesktop,
		"uninstall":  cmdUninstall,
		"completion": cmdCompletion,
		"config":     func([]string) error { return cmdConfig() },
		"winecfg":    func(a []string) error { return cmdWineTool("winecfg", a) },
		"regedit":    func(a []string) error { return cmdWineTool("regedit", a) },
		"wine":       func(a []string) error { return cmdWineTool("wine", a) },
	}
}

// rootSafe lists the commands that only print text and touch nothing, so
// package builds (which often run as root in a container) can use them.
// Everything else refuses to run as root: Brine works on your files, your
// Wine prefix and your desktop, and never on the system's.
func rootSafe(cmd string) bool {
	switch cmd {
	case "version", "--version", "-V", "help", "-h", "--help", "completion":
		return true
	}
	return false
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	if os.Geteuid() == 0 && !rootSafe(cmd) {
		fmt.Fprintln(os.Stderr, "brine: refusing to run as root; run it as your normal user")
		os.Exit(1)
	}
	switch cmd {
	case "version", "--version", "-V":
		fmt.Println("brine", Version)
		return
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	}
	fn, ok := commands[cmd]
	if !ok {
		// Desktop environments may pass a URI or file directly.
		if strings.Contains(cmd, ":") || strings.HasSuffix(cmd, ".rbxl") || strings.HasSuffix(cmd, ".rbxlx") {
			fn, args, cmd = cmdRun, os.Args[1:], "run"
		} else {
			fmt.Fprintf(os.Stderr, "brine: unknown command %q\n\n%s", cmd, usage)
			os.Exit(2)
		}
	}
	if err := fn(args); err != nil {
		fmt.Fprintln(os.Stderr, "brine:", err)
		// Launched from a menu or a link there is no terminal to read the error in.
		if cmd == "run" && !isTerminal(os.Stderr) {
			notify("Brine could not start Studio", err.Error())
		}
		os.Exit(1)
	}
}

// lockInstall takes the cross-process lock that protects the installation.
func lockInstall() (*lock.Lock, error) {
	return lock.Acquire(30*time.Minute, func(m string) { say("%s", m) })
}

// notify shows a desktop notification if notify-send is available.
func notify(title, body string) {
	if p, err := exec.LookPath("notify-send"); err == nil {
		exec.Command(p, "--app-name=Brine", "--icon=brine", title, body).Run()
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

// isTerminal reports whether f is a terminal. A character-device check is not
// enough: /dev/null is one too.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// ---- install / update / rollback ----
