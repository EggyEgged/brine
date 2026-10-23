package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableMCP(t *testing.T) {
	lad := t.TempDir()
	dir := filepath.Join(lad, "Roblox", "AssistantSettings")
	os.MkdirAll(dir, 0o755)
	f := filepath.Join(dir, "123.json")
	os.WriteFile(f, []byte(`{"AssistantSkillsCloudMigrated":true,"mcp-server":{"enabled":false}}`), 0o644)

	s, err := MCPSetting(lad)
	if err != nil || s["123"] {
		t.Fatalf("expected disabled, got %v %v", s, err)
	}
	if _, err := EnableMCP(lad); err != nil {
		t.Fatal(err)
	}
	s, _ = MCPSetting(lad)
	if !s["123"] {
		t.Fatal("not enabled")
	}
	b, _ := os.ReadFile(f)
	if !strings.Contains(string(b), "AssistantSkillsCloudMigrated") {
		t.Fatal("other settings lost")
	}
	if _, err := EnableMCP(t.TempDir()); err == nil {
		t.Fatal("expected error without any signed-in user")
	}
}
