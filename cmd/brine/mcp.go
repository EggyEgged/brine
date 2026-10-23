package main

import (
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

	"brine/internal/doctor"
	"brine/internal/logx"
	"brine/internal/mcp"
	"brine/internal/studio"
)

func cmdMCP(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "connect":
			return cmdMCPConnect(args[1:])
		case "disconnect":
			return cmdMCPDisconnect(args[1:])
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

func cmdMCPDisconnect(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: brine mcp disconnect claude-code|claude-desktop")
	}
	switch args[0] {
	case "claude-code":
		fs := flag.NewFlagSet("disconnect", flag.ExitOnError)
		scope := fs.String("scope", "user", "Claude Code scope: user, local or project")
		fs.Parse(args[1:])
		ok, err := mcp.DisconnectClaudeCode(*scope, os.Stderr)
		if err != nil {
			return err
		}
		if !ok {
			say("%s is not registered with Claude Code (or `claude` is not installed)", mcp.ServerName)
		}
	case "claude-desktop":
		ok, backup, err := mcp.DisconnectClaudeDesktop(mcp.DesktopConfigPath())
		if err != nil {
			return err
		}
		if ok {
			say("Removed %s from %s (backup: %s)", mcp.ServerName, mcp.DesktopConfigPath(), backup)
		} else {
			say("%s is not in %s", mcp.ServerName, mcp.DesktopConfigPath())
		}
	default:
		return fmt.Errorf("unknown client %q (supported: claude-code, claude-desktop)", args[0])
	}
	return nil
}
