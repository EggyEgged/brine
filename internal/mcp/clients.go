package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"brine/internal/dirs"
)

// ServerName is the name MCP clients show for Studio.
const ServerName = "Roblox_Studio"

// ServerCommand returns the command clients should run: this brine binary
// by absolute path (desktop apps often lack ~/.local/bin in PATH).
func ServerCommand() (string, []string) {
	exe, err := os.Executable()
	if err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
	} else {
		exe = "brine"
	}
	return exe, []string{"mcp"}
}

// ClaudeCodeArgs is the `claude` command line that registers Brine.
func ClaudeCodeArgs(scope string) []string {
	exe, args := ServerCommand()
	return append([]string{"mcp", "add", "--transport", "stdio", "--scope", scope, ServerName, "--", exe}, args...)
}

// ConnectClaudeCode registers Brine with Claude Code, replacing an existing
// Roblox_Studio entry in the same scope.
func ConnectClaudeCode(scope string, out io.Writer) error {
	claude, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("the `claude` command was not found in PATH; install Claude Code or run the command from `brine mcp --print-config` yourself")
	}
	// Ignore failure: the entry usually doesn't exist yet.
	rm := exec.Command(claude, "mcp", "remove", "--scope", scope, ServerName)
	if b, err := rm.CombinedOutput(); err == nil {
		fmt.Fprintf(out, "Replaced existing %s entry (%s scope).\n", ServerName, scope)
	} else {
		_ = b
	}
	cmd := exec.Command(claude, ClaudeCodeArgs(scope)...)
	cmd.Stdout, cmd.Stderr = out, out
	fmt.Fprintf(out, "$ claude %s\n", shellJoin(ClaudeCodeArgs(scope)))
	return cmd.Run()
}

// DesktopConfigPath is Claude Desktop's config on Linux.
func DesktopConfigPath() string {
	return filepath.Join(dirs.XDGConfig, "Claude", "claude_desktop_config.json")
}

// ServerEntry is an mcpServers entry, with command before args.
type ServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// DesktopEntry is the mcpServers entry for Claude Desktop.
func DesktopEntry() ServerEntry {
	exe, args := ServerCommand()
	return ServerEntry{Command: exe, Args: args}
}

// ConnectClaudeDesktop merges the Brine entry into Claude Desktop's config,
// keeping a timestamped backup of the previous file. It returns the backup
// path ("" when there was no previous file).
func ConnectClaudeDesktop(path string) (string, error) {
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	merged, err := MergeServer(old, ServerName, DesktopEntry())
	if err != nil {
		return "", fmt.Errorf("%s: %w (file left unchanged)", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o600)
	backup := ""
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		backup = fmt.Sprintf("%s.brine-backup-%s", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, old, mode); err != nil {
			return "", fmt.Errorf("backup: %w", err)
		}
	}
	tmp := path + ".brine-tmp"
	if err := os.WriteFile(tmp, merged, mode); err != nil {
		return backup, err
	}
	return backup, os.Rename(tmp, path)
}

// MergeServer sets mcpServers[name] = entry in a Claude Desktop config
// document. Every other key and server is kept, in its original order.
// Invalid JSON is an error: the caller must not overwrite such a file.
func MergeServer(doc []byte, name string, entry any) ([]byte, error) {
	if len(bytes.TrimSpace(doc)) == 0 {
		doc = []byte("{}")
	}
	top, err := decodeObject(doc)
	if err != nil {
		return nil, fmt.Errorf("not a valid JSON object: %w", err)
	}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	servers := orderedObject{}
	if raw, ok := top.get("mcpServers"); ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			raw = []byte("{}")
		}
		servers, err = decodeObject(raw)
		if err != nil {
			return nil, fmt.Errorf("\"mcpServers\" is not an object: %w", err)
		}
	}
	servers.set(name, entryJSON)
	top.set("mcpServers", servers.encode())

	var out bytes.Buffer
	if err := json.Indent(&out, top.encode(), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	if !json.Valid(out.Bytes()) {
		return nil, errors.New("merged config is not valid JSON")
	}
	return out.Bytes(), nil
}

type kv struct {
	key string
	val json.RawMessage
}

type orderedObject []kv

func (o orderedObject) get(k string) (json.RawMessage, bool) {
	for _, e := range o {
		if e.key == k {
			return e.val, true
		}
	}
	return nil, false
}

func (o *orderedObject) set(k string, v json.RawMessage) {
	for i, e := range *o {
		if e.key == k {
			(*o)[i].val = v
			return
		}
	}
	*o = append(*o, kv{k, v})
}

func (o orderedObject) encode() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(e.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// decodeObject parses a JSON object, preserving key order. Duplicate keys
// keep the last value, matching encoding/json.
func decodeObject(data []byte) (orderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("expected an object")
	}
	var o orderedObject
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("expected a key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.set(key, raw)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after object")
	}
	return o, nil
}

// PrintConfig writes manual setup instructions for both clients.
func PrintConfig(w io.Writer) {
	fmt.Fprintf(w, "# Claude Code\nclaude %s\n\n", shellJoin(ClaudeCodeArgs("user")))
	doc, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{ServerName: DesktopEntry()}}, "", "  ")
	fmt.Fprintf(w, "# Claude Desktop (%s)\n%s\n", DesktopConfigPath(), doc)
}

func shellJoin(args []string) string {
	var b bytes.Buffer
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if a == "" || bytes.ContainsAny([]byte(a), " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
			b.WriteString("'" + string(bytes.ReplaceAll([]byte(a), []byte("'"), []byte(`'\''`))) + "'")
		} else {
			b.WriteString(a)
		}
	}
	return b.String()
}
