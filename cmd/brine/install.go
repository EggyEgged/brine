package main

import (
	"errors"
	"flag"
	"os"

	"brine/internal/studio"
)

func cmdInstall(args []string, isUpdate bool) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	force := fs.Bool("force", false, "re-download even if up to date")
	fs.Parse(args)
	lk, err := lockInstall()
	if err != nil {
		return err
	}
	defer lk.Release()
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
	lk, err := lockInstall()
	if err != nil {
		return err
	}
	defer lk.Release()
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
	say("Now using %s (%s). Automatic updates are paused; run `brine update` to go forward again.",
		a.State.Versions[a.State.Current].Version, a.State.Current)
	return nil
}

// ---- run / kill ----
