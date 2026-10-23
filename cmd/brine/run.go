package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"brine/internal/app"
	"brine/internal/link"
	"brine/internal/lock"
	"brine/internal/logx"
	"brine/internal/studio"
)

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
	if !app.HasDisplay() {
		return app.ErrNoDisplay
	}
	target := strings.Join(fs.Args(), " ")
	res, err := link.Resolve(target, link.NetLookup)
	if err != nil {
		return err
	}
	studioArgs := res.Args
	// A browser, file manager or xdg-open starts us with no terminal and may
	// wait for us to exit; Studio runs for hours. Hand the work to a
	// background copy so the caller gets control back at once. Problems the
	// copy hits are reported as desktop notifications, as for any menu launch.
	if res.Kind != link.None && os.Getenv(detachedEnv) == "" && !isTerminal(os.Stdin) && !isTerminal(os.Stderr) {
		return detach(res.Canonical, *verbose)
	}
	logx.Setup("brine.log", *verbose)
	logx.Printf("run %s", target)

	// Sign-in callbacks must reach the running Studio quickly; skip updates.
	if res.Kind != link.Auth && !a.StudioRunning() && a.Cfg.Studio.AutoUpdate && !a.State.Pinned {
		autoUpdate(a)
	}
	if _, err := a.BuildDir(); err != nil {
		return err
	}
	if res.Kind != link.Auth {
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

// autoUpdate installs a newer Studio build if one exists. It never blocks
// starting Studio for long: the check has a short deadline and a running
// install/update by another brine command is not waited for.
func autoUpdate(a *app.App) {
	rel, err := studio.LatestWithin(5 * time.Second)
	if err != nil {
		say("Could not check for Studio updates (starting the installed build): %v", err)
		return
	}
	if rel.GUID == a.State.Current {
		return
	}
	lk, err := lock.Acquire(0, nil)
	if err != nil {
		say("Another brine command is updating Studio; starting the installed build")
		return
	}
	defer lk.Release()
	// Re-read: the other command may have installed it while we waited.
	if st, err := studio.LoadState(); err == nil {
		a.State = st
		if st.Current == rel.GUID {
			return
		}
	}
	say("Updating Studio to %s", rel.Version)
	if err := studio.Install(a.State, rel, a.Cfg.Studio.KeepVersions, func(f string, x ...any) { say(f, x...) }); err != nil {
		say("Update failed, starting the installed build: %v", err)
	}
}

// detachedEnv marks the background copy started by detach.
const detachedEnv = "BRINE_DETACHED"

// detach re-runs `brine run target` in its own session with no terminal and
// returns as soon as it has started.
func detach(target string, verbose bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"run"}
	if verbose {
		args = append(args, "-v")
	}
	cmd := exec.Command(self, append(args, target)...)
	cmd.Env = append(os.Environ(), detachedEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	logx.Setup("brine.log", false)
	logx.Printf("handed %q to background process %d", logx.Redact(target), cmd.Process.Pid)
	return cmd.Process.Release()
}
