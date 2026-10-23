package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.Graphics.Renderer = "vulkan"
	c.Env["FOO"] = "bar"
	c.FFlags["FFlagX"] = true
	if err := c.SaveFile(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Graphics.Renderer != "vulkan" || got.Env["FOO"] != "bar" || got.FFlags["FFlagX"] != true || got.Studio.KeepVersions != 2 {
		t.Fatalf("round trip lost data: %+v", got)
	}
	os.WriteFile(p, []byte("[graphics]\nrenderer = \"metal\"\n"), 0o644)
	if _, err := LoadFile(p); err == nil {
		t.Fatal("invalid renderer accepted")
	}
	if c, err := LoadFile(filepath.Join(t.TempDir(), "missing.toml")); err != nil || c.Wine.Runtime != "auto" {
		t.Fatal("missing file should give defaults")
	}
}
