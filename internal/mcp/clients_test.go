package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

var entry = map[string]any{"command": "/usr/bin/brine", "args": []string{"mcp"}}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, b)
	}
	return m
}

func TestMergeEmpty(t *testing.T) {
	for _, in := range []string{"", "  \n", "{}"} {
		out, err := MergeServer([]byte(in), ServerName, entry)
		if err != nil {
			t.Fatal(err)
		}
		m := decode(t, out)
		srv := m["mcpServers"].(map[string]any)[ServerName].(map[string]any)
		if srv["command"] != "/usr/bin/brine" {
			t.Fatalf("bad entry: %v", srv)
		}
	}
}

func TestMergePreservesOtherServersAndKeys(t *testing.T) {
	in := `{
  "globalShortcut": "Ctrl+Space",
  "mcpServers": {
    "zeta": {"command": "z", "env": {"A": "1"}},
    "alpha": {"command": "a", "args": ["--x", 1.50]}
  },
  "zzz": [1, 2, {"deep": null}]
}`
	out, err := MergeServer([]byte(in), ServerName, entry)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if m["globalShortcut"] != "Ctrl+Space" || m["zzz"] == nil {
		t.Fatalf("lost top-level keys: %s", out)
	}
	servers := m["mcpServers"].(map[string]any)
	if len(servers) != 3 || servers["zeta"] == nil || servers["alpha"] == nil {
		t.Fatalf("lost servers: %s", out)
	}
	s := string(out)
	// Key order is kept: existing keys first, new server appended.
	if !(strings.Index(s, "globalShortcut") < strings.Index(s, "mcpServers") &&
		strings.Index(s, "mcpServers") < strings.Index(s, `"zzz"`) &&
		strings.Index(s, `"zeta"`) < strings.Index(s, `"alpha"`) &&
		strings.Index(s, `"alpha"`) < strings.Index(s, ServerName)) {
		t.Fatalf("order not preserved:\n%s", s)
	}
	// Numbers are not reformatted.
	if !strings.Contains(s, "1.50") {
		t.Fatalf("number reformatted:\n%s", s)
	}
}

func TestMergeReplacesExisting(t *testing.T) {
	in := `{"mcpServers":{"Roblox_Studio":{"command":"cmd.exe","args":["/c","mcp.bat"]},"other":{"command":"o"}}}`
	out, err := MergeServer([]byte(in), ServerName, entry)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	servers := m["mcpServers"].(map[string]any)
	if servers[ServerName].(map[string]any)["command"] != "/usr/bin/brine" || servers["other"] == nil {
		t.Fatalf("bad merge: %s", out)
	}
	if strings.Count(string(out), ServerName) != 1 {
		t.Fatalf("duplicate entry: %s", out)
	}
}

func TestMergeNullServers(t *testing.T) {
	out, err := MergeServer([]byte(`{"mcpServers":null}`), ServerName, entry)
	if err != nil {
		t.Fatal(err)
	}
	decode(t, out)
}

func TestMergeRejectsInvalid(t *testing.T) {
	for _, in := range []string{
		`{"mcpServers": {`,              // truncated
		`[]`,                            // not an object
		`{"mcpServers": []}`,            // servers not an object
		`{"mcpServers": "x"}`,           // servers not an object
		`{"a":1} {"b":2}`,               // trailing data
		`{"a":1,}`,                      // trailing comma
		`// comment` + "\n" + `{"a":1}`, // comments are not JSON
	} {
		if _, err := MergeServer([]byte(in), ServerName, entry); err == nil {
			t.Errorf("MergeServer(%q) should fail", in)
		}
	}
}

func TestConnectClaudeDesktopBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/claude_desktop_config.json"
	orig := `{"mcpServers":{"keep":{"command":"k"}}}`
	if err := writeFile(path, orig); err != nil {
		t.Fatal(err)
	}
	backup, err := ConnectClaudeDesktop(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := readFile(backup); b != orig {
		t.Fatalf("backup content %q", b)
	}
	b, _ := readFile(path)
	m := decode(t, []byte(b))
	if m["mcpServers"].(map[string]any)["keep"] == nil {
		t.Fatal("lost existing server")
	}

	// Invalid JSON must be left untouched.
	bad := `{"mcpServers": {`
	writeFile(path, bad)
	if _, err := ConnectClaudeDesktop(path); err == nil {
		t.Fatal("expected error on invalid JSON")
	}
	if b, _ := readFile(path); b != bad {
		t.Fatal("invalid file was modified")
	}
}

func TestRemoveServer(t *testing.T) {
	in := `{"theme":"dark","mcpServers":{"a":{"command":"a"},"Roblox_Studio":{"command":"b"},"z":{"command":"z"}}}`
	out, removed, err := RemoveServer([]byte(in), ServerName)
	if err != nil || !removed {
		t.Fatal(removed, err)
	}
	m := decode(t, out)
	s := m["mcpServers"].(map[string]any)
	if len(s) != 2 || s["a"] == nil || s["z"] == nil || m["theme"] != "dark" {
		t.Fatalf("bad result: %s", out)
	}
	// Absent entry or section: unchanged, not an error.
	for _, doc := range []string{`{}`, `{"mcpServers":{"a":{}}}`} {
		out, removed, err := RemoveServer([]byte(doc), ServerName)
		if err != nil || removed || string(out) != doc {
			t.Errorf("RemoveServer(%s) = %q %v %v", doc, out, removed, err)
		}
	}
	if _, _, err := RemoveServer([]byte(`{"mcpServers": [`), ServerName); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestDisconnectClaudeDesktop(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/c.json"
	if ok, _, err := DisconnectClaudeDesktop(path); ok || err != nil {
		t.Fatalf("missing file: %v %v", ok, err)
	}
	writeFile(path, `{"mcpServers":{"Roblox_Studio":{"command":"x"},"keep":{"command":"k"}}}`)
	ok, backup, err := DisconnectClaudeDesktop(path)
	if err != nil || !ok || backup == "" {
		t.Fatal(ok, backup, err)
	}
	b, _ := readFile(path)
	if strings.Contains(b, "Roblox_Studio") || !strings.Contains(b, "keep") {
		t.Fatalf("bad result: %s", b)
	}
	if orig, _ := readFile(backup); !strings.Contains(orig, "Roblox_Studio") {
		t.Fatal("backup lost the original")
	}
}
