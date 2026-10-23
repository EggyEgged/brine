package main

import (
	"errors"
	"flag"
	"os/exec"
	"strings"
	"time"

	"brine/internal/app"
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
	studioArgs, err := app.StudioArgs(target)
	if err != nil {
		return err
	}
	logx.Setup("brine.log", *verbose)
	logx.Printf("run %s", target)

	// Sign-in callbacks must reach the running Studio quickly; skip updates.
	if !app.IsAuthURI(target) && !a.StudioRunning() && a.Cfg.Studio.AutoUpdate && !a.State.Pinned {
		autoUpdate(a)
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
