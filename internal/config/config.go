// Package config loads and saves ~/.config/brine/config.toml.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"brine/internal/dirs"
)

type Wine struct {
	// Runtime selects the Wine build: "auto", "system", "managed",
	// or a path to a Wine/Proton build directory, a wine binary, or umu-run.
	Runtime string `toml:"runtime"`
	// ProtonPath is used only when Runtime points at umu-run.
	ProtonPath string `toml:"proton_path"`
	// Debug is the WINEDEBUG value used for Studio and the MCP helper.
	Debug string `toml:"debug"`
}

type Graphics struct {
	// Renderer is one of "auto", "dxvk", "vulkan", "d3d11", "opengl".
	Renderer string `toml:"renderer"`
	// GPU is one of "auto", "discrete", "integrated".
	GPU         string `toml:"gpu"`
	DXVKVersion string `toml:"dxvk_version"`
}

type Studio struct {
	// WebView2 is the Edge WebView2 runtime version to install for Studio's
	// sign-in and web panels: a version, "latest", or "" to skip it.
	WebView2 string `toml:"webview2"`
	// KeepVersions is how many Studio builds to keep on disk (minimum 2 so
	// rollback works).
	KeepVersions int `toml:"keep_versions"`
	// Desktop runs Studio inside a Wine virtual desktop, e.g. "1920x1080".
	Desktop string `toml:"desktop"`
	// Launcher is prepended to the Wine command (e.g. "gamemoderun").
	Launcher string `toml:"launcher"`
	// AutoUpdate installs a newer Studio build (if any) on `brine run`.
	AutoUpdate bool `toml:"auto_update"`
}

type MCP struct {
	// AutostartStudio starts Studio when an MCP client connects and it isn't running.
	AutostartStudio bool `toml:"autostart_studio"`
	// StartTimeout is how long, in seconds, to wait for Studio to connect after autostart.
	StartTimeout int `toml:"start_timeout"`
	// ListTimeout is how long, in seconds, to wait for the helper to answer
	// tools/list before answering with an empty list.
	ListTimeout int `toml:"list_timeout"`
}

type Config struct {
	Wine     Wine              `toml:"wine"`
	Graphics Graphics          `toml:"graphics"`
	Studio   Studio            `toml:"studio"`
	MCP      MCP               `toml:"mcp"`
	Env      map[string]string `toml:"env"`
	FFlags   map[string]any    `toml:"fflags"`
}

const (
	DefaultDXVK = "3.1.1"
	// DefaultWebView2 is a WebView2 runtime release known to work under Wine.
	DefaultWebView2 = "144.0.3719.92"
)

var (
	Renderers = []string{"auto", "dxvk", "vulkan", "d3d11", "opengl"}
	GPUs      = []string{"auto", "discrete", "integrated"}
)

func Default() *Config {
	return &Config{
		Wine:     Wine{Runtime: "auto", Debug: "-all"},
		Graphics: Graphics{Renderer: "auto", GPU: "auto", DXVKVersion: DefaultDXVK},
		Studio:   Studio{WebView2: DefaultWebView2, KeepVersions: 2, AutoUpdate: true},
		MCP:      MCP{StartTimeout: 120, ListTimeout: 5},
		Env:      map[string]string{},
		FFlags:   map[string]any{},
	}
}

// Load reads the config file, falling back to defaults for anything unset.
// A missing file is not an error.
func Load() (*Config, error) {
	return LoadFile(dirs.ConfigFile())
}

func LoadFile(path string) (*Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := toml.Decode(string(b), c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Env == nil {
		c.Env = map[string]string{}
	}
	if c.FFlags == nil {
		c.FFlags = map[string]any{}
	}
	return c, c.Validate()
}

func (c *Config) Validate() error {
	c.Graphics.Renderer = strings.ToLower(c.Graphics.Renderer)
	c.Graphics.GPU = strings.ToLower(c.Graphics.GPU)
	if !slices.Contains(Renderers, c.Graphics.Renderer) {
		return fmt.Errorf("graphics.renderer must be one of %v, got %q", Renderers, c.Graphics.Renderer)
	}
	if !slices.Contains(GPUs, c.Graphics.GPU) {
		return fmt.Errorf("graphics.gpu must be one of %v, got %q", GPUs, c.Graphics.GPU)
	}
	if c.Graphics.DXVKVersion == "" {
		c.Graphics.DXVKVersion = DefaultDXVK
	}
	if c.Studio.KeepVersions < 2 {
		c.Studio.KeepVersions = 2
	}
	if c.MCP.StartTimeout <= 0 {
		c.MCP.StartTimeout = 120
	}
	if c.MCP.ListTimeout <= 0 {
		c.MCP.ListTimeout = 5
	}
	return nil
}

// Save writes the config atomically.
func (c *Config) Save() error {
	return c.SaveFile(dirs.ConfigFile())
}

func (c *Config) SaveFile(path string) error {
	var buf bytes.Buffer
	buf.WriteString(header)
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

const header = `# Brine configuration. See "brine help config" for every option.
#
# [wine] runtime: "auto" (system Wine, else the Brine-managed build),
#   "system", "managed", or a path to a Wine/Proton build or umu-run.
# [graphics] renderer: "auto", "dxvk", "vulkan", "d3d11" (wined3d), "opengl".
# [graphics] gpu: "auto" (prefer discrete), "discrete", "integrated".
# [env] extra environment variables for Studio and the MCP helper.
# [fflags] extra Studio fast flags written to ClientAppSettings.json.

`
