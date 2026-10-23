package doctor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"brine/internal/mcp"
)

// Session drives `brine mcp` as a real client would, over stdio.
type Session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	nextID int
	// Stray holds any stdout line that was not valid JSON-RPC.
	Stray []string
}

// Start launches `<exe> mcp` and performs the MCP handshake.
func Start(exe string, timeout time.Duration) (*Session, json.RawMessage, error) {
	cmd := exec.Command(exe, "mcp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	s := &Session{cmd: cmd, stdin: stdin, lines: make(chan []byte, 64)}
	go func() {
		r := bufio.NewReaderSize(stdout, 1<<20)
		for {
			l, err := r.ReadBytes('\n')
			if len(l) > 0 {
				s.lines <- l
			}
			if err != nil {
				close(s.lines)
				return
			}
		}
	}()
	res, err := s.Call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "brine-doctor", "version": "1"},
	}, timeout)
	if err != nil {
		s.Close()
		return nil, nil, fmt.Errorf("initialize: %w", err)
	}
	s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return s, res, nil
}

func (s *Session) send(v any) error {
	b, _ := json.Marshal(v)
	_, err := s.stdin.Write(append(b, '\n'))
	return err
}

// Call sends a request and waits for its response, skipping notifications.
func (s *Session) Call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	s.nextID++
	id := s.nextID
	if err := s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	deadline := time.After(timeout)
	for {
		select {
		case l, ok := <-s.lines:
			if !ok {
				return nil, errors.New("brine mcp exited")
			}
			// Exactly one JSON object per line, LF-terminated, nothing else.
			if !strings.HasSuffix(string(l), "\n") || strings.HasSuffix(string(l), "\r\n") {
				s.Stray = append(s.Stray, fmt.Sprintf("bad line ending: %q", l))
			}
			clean, ok := mcp.Clean(l)
			if !ok || len(clean) != len(strings.TrimRight(string(l), "\n")) {
				s.Stray = append(s.Stray, string(l))
				continue
			}
			var m struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(clean, &m) != nil || m.ID == nil || *m.ID != id {
				continue
			}
			if m.Error != nil {
				return nil, errors.New(m.Error.Message)
			}
			return m.Result, nil
		case <-deadline:
			return nil, fmt.Errorf("%s: no response after %s", method, timeout)
		}
	}
}

// CallTool calls a tool and returns its text content and isError flag.
func (s *Session) CallTool(name string, args map[string]any, timeout time.Duration) (string, bool, error) {
	res, err := s.Call("tools/call", map[string]any{"name": name, "arguments": args}, timeout)
	if err != nil {
		return "", false, err
	}
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return "", false, err
	}
	var parts []string
	for _, c := range r.Content {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n"), r.IsError, nil
}

// Close ends the session by closing stdin, as clients do, and reports how
// long `brine mcp` took to exit.
func (s *Session) Close() (time.Duration, error) {
	start := time.Now()
	s.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		return time.Since(start), err
	case <-time.After(15 * time.Second):
		s.cmd.Process.Kill()
		return time.Since(start), errors.New("brine mcp did not exit within 15s of stdin closing")
	}
}
