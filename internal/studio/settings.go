package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RendererFlags returns the fast flags that pick Studio's graphics API for
// a resolved renderer ("dxvk" and "d3d11" both mean Direct3D 11; they differ
// only in which DLLs Wine loads).
func RendererFlags(renderer string) map[string]any {
	apis := []string{"D3D11", "D3D11FL10", "Vulkan", "OpenGL"}
	want := map[string]string{"dxvk": "D3D11", "d3d11": "D3D11", "vulkan": "Vulkan", "opengl": "OpenGL"}[renderer]
	flags := map[string]any{}
	if want == "" {
		return flags
	}
	for _, api := range apis {
		flags["FFlagDebugGraphicsPrefer"+api] = api == want
		flags["FFlagDebugGraphicsDisable"+api] = api != want
	}
	return flags
}

// WriteFFlags writes ClientSettings/ClientAppSettings.json into a Studio
// build. extra (from [fflags] in config) overrides the renderer flags.
func WriteFFlags(buildDir, renderer string, extra map[string]any) error {
	flags := RendererFlags(renderer)
	for k, v := range extra {
		flags[k] = v
	}
	dir := filepath.Join(buildDir, "ClientSettings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(flags, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "ClientAppSettings.json"), b, 0o644)
}

// RobloxDir is %LOCALAPPDATA%\Roblox for a given LocalAppData directory.
func RobloxDir(localAppData string) string { return filepath.Join(localAppData, "Roblox") }

// MCPSetting reports Studio's "Enable Studio as MCP server" toggle for each
// signed-in user, read from AssistantSettings/<userId>.json.
func MCPSetting(localAppData string) (map[string]bool, error) {
	files, _ := filepath.Glob(filepath.Join(RobloxDir(localAppData), "AssistantSettings", "*.json"))
	out := map[string]bool{}
	for _, f := range files {
		uid := strings.TrimSuffix(filepath.Base(f), ".json")
		m, err := readJSONObject(f)
		if err != nil {
			return nil, err
		}
		enabled := false
		if srv, ok := m["mcp-server"].(map[string]any); ok {
			enabled, _ = srv["enabled"].(bool)
		}
		out[uid] = enabled
	}
	return out, nil
}

// EnableMCP turns on "Enable Studio as MCP server" for every user that has
// signed in to Studio in this prefix. Studio must not be running, or it
// will overwrite the file on exit.
func EnableMCP(localAppData string) ([]string, error) {
	files, _ := filepath.Glob(filepath.Join(RobloxDir(localAppData), "AssistantSettings", "*.json"))
	if len(files) == 0 {
		return nil, errors.New("no Studio user settings found: sign in to Studio once, close it, then retry")
	}
	sort.Strings(files)
	var users []string
	for _, f := range files {
		m, err := readJSONObject(f)
		if err != nil {
			return users, err
		}
		srv, _ := m["mcp-server"].(map[string]any)
		if srv == nil {
			srv = map[string]any{}
		}
		srv["enabled"] = true
		m["mcp-server"] = srv
		b, err := json.MarshalIndent(m, "", "    ")
		if err != nil {
			return users, err
		}
		if err := os.WriteFile(f, b, 0o644); err != nil {
			return users, err
		}
		users = append(users, strings.TrimSuffix(filepath.Base(f), ".json"))
	}
	return users, nil
}

func readJSONObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if len(strings.TrimSpace(string(b))) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}
