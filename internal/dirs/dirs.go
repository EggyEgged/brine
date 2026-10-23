// Package dirs resolves Brine's XDG base directories.
package dirs

import (
	"os"
	"path/filepath"
)

func xdg(env, fallback string) string {
	if v := os.Getenv(env); v != "" && filepath.IsAbs(v) {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

var (
	// Config holds config.toml.
	Config = filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "brine")
	// Data holds the Wine prefix, Studio versions and managed runtimes.
	Data = filepath.Join(xdg("XDG_DATA_HOME", ".local/share"), "brine")
	// State holds logs.
	State = filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), "brine")
	// Cache holds downloads that can be thrown away.
	Cache = filepath.Join(xdg("XDG_CACHE_HOME", ".cache"), "brine")

	// XDGData is the user's data home, used for .desktop/MIME/icon files.
	XDGData = xdg("XDG_DATA_HOME", ".local/share")
	// XDGConfig is the user's config home.
	XDGConfig = xdg("XDG_CONFIG_HOME", ".config")
)

func ConfigFile() string { return filepath.Join(Config, "config.toml") }
func Prefix() string     { return filepath.Join(Data, "prefix") }
func Versions() string   { return filepath.Join(Data, "versions") }
func Runtimes() string   { return filepath.Join(Data, "runtimes") }
func StateFile() string  { return filepath.Join(Data, "state.json") }
func Logs() string       { return State }

// Ensure creates all Brine directories.
func Ensure() error {
	for _, d := range []string{Config, Data, State, Cache, Versions(), Runtimes()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
