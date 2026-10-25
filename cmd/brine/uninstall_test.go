package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveAllSafe(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "brine")
	os.MkdirAll(filepath.Join(good, "prefix"), 0o755)
	os.WriteFile(filepath.Join(good, "prefix", "f"), []byte("x"), 0o644)
	if err := removeAllSafe(good); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(good); err == nil {
		t.Fatal("not removed")
	}

	other := filepath.Join(root, "documents")
	os.MkdirAll(other, 0o755)
	home, _ := os.UserHomeDir()
	for _, bad := range []string{other, "/", home, "relative/brine", filepath.Join(root, "x", "..", "documents"), ""} {
		if err := removeAllSafe(bad); err == nil {
			t.Errorf("removeAllSafe(%q) should refuse", bad)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("deleted a directory it should have refused")
	}
}

func TestSubcommandsComplete(t *testing.T) {
	have := map[string]bool{}
	for _, c := range subcommands() {
		have[c] = true
	}
	for _, want := range []string{"setup", "run", "mcp", "doctor", "uninstall", "completion", "kill", "version"} {
		if !have[want] {
			t.Errorf("completion lacks %s", want)
		}
	}
}

func TestUserExtraPaths(t *testing.T) {
	got := userExtraPaths("/home/u/.local/bin/brine", "/home/u", "/home/u/.local/share", "/home/u/.config")
	want := []string{
		"/home/u/.local/share/man/man1/brine.1",
		"/home/u/.local/share/zsh/site-functions/_brine",
		"/home/u/.local/share/bash-completion/completions/brine",
		"/home/u/.config/fish/completions/brine.fish",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %s, want %s", got[i], want[i])
		}
	}
	// A packaged install is not ours to remove.
	if p := userExtraPaths("/usr/bin/brine", "/home/u", "/home/u/.local/share", "/home/u/.config"); p != nil {
		t.Errorf("touched a system install: %v", p)
	}
	if p := userExtraPaths("/home/u/.local/bin/brine", "", "", ""); p != nil {
		t.Errorf("no home but returned %v", p)
	}
}

func TestRootSafe(t *testing.T) {
	for _, c := range []string{"version", "--version", "-V", "help", "--help", "-h", "completion"} {
		if !rootSafe(c) {
			t.Errorf("%s should be allowed as root (it only prints text)", c)
		}
	}
	// Anything that touches Wine, files or the desktop must refuse root.
	for _, c := range []string{"setup", "run", "kill", "mcp", "doctor", "install", "update", "rollback", "uninstall", "desktop", "config", "wine", "winecfg", "regedit", "runtime", "", "roblox-studio:1+x"} {
		if rootSafe(c) {
			t.Errorf("%q must refuse to run as root", c)
		}
	}
}
