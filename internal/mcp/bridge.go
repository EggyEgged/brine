package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Bridge sits between an MCP client (our stdin/stdout) and Studio's MCP
// helper running under Wine.
//
// The helper hosts the local WebSocket that Studio connects to, so it is
// started immediately and restarted if it dies (for example after
// `brine kill` stops the wineserver). The bridge answers on its own when
// the helper can't: tools/call while Studio isn't running, a tools/list the
// helper is slow to answer, and every request when Studio isn't installed.
type Bridge struct {
	In  io.Reader
	Out io.Writer
	Log *log.Logger

	// NewHelper returns an unstarted command for the helper, or an error
	// explaining why it cannot run (e.g. Studio not installed).
	NewHelper func() (*exec.Cmd, error)
	// HelperLog receives the helper's stderr.
	HelperLog io.Writer
	// StudioRunning reports whether a Studio process exists in the prefix.
	StudioRunning func() bool
	// Autostart, if set, launches Studio in the background.
	Autostart    func() error
	StartTimeout time.Duration
	ListTimeout  time.Duration
	Version      string

	outMu sync.Mutex

	mu          sync.Mutex
	helper      *helperProc
	degraded    string // non-empty: reason the helper can't run
	closing     bool
	initReq     map[string]any // client's initialize request, replayed on restart
	initialized bool
	pending     map[string]pendingReq    // client requests the helper owes an answer
	answered    map[string]bool          // ids the bridge already answered itself
	internal    map[string]chan *Message // bridge-originated requests
	nextID      int
	autostarted bool
	restarting  bool
}

type pendingReq struct {
	method string
	timer  *time.Timer
}

type helperProc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex
	done    chan struct{}
}

const internalPrefix = "brine-internal-"

// NotRunning is the message returned for tool calls while Studio is closed.
const NotRunning = "Roblox Studio is not running. Start it with `brine run` (or set autostart_studio = true " +
	"under [mcp] in ~/.config/brine/config.toml), open a place, and make sure Assistant → … → " +
	"Manage MCP Servers → \"Enable Studio as MCP server\" is on (`brine mcp enable` sets it while Studio is closed)."

// Run serves until stdin closes, then stops the helper.
func (b *Bridge) Run() error {
	b.pending = map[string]pendingReq{}
	b.answered = map[string]bool{}
	b.internal = map[string]chan *Message{}
	if b.ListTimeout == 0 {
		b.ListTimeout = 5 * time.Second
	}
	if b.StartTimeout == 0 {
		b.StartTimeout = 120 * time.Second
	}

	if err := b.startHelper(); err != nil {
		b.degraded = err.Error()
		b.Log.Printf("helper unavailable, answering locally: %v", err)
	}

	r := bufio.NewReaderSize(b.In, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			b.fromClient(line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				b.Log.Printf("stdin: %v", err)
			}
			break
		}
	}
	b.Shutdown()
	return nil
}

// Shutdown stops the helper: close its stdin, give it a moment to exit,
// then terminate its process group.
func (b *Bridge) Shutdown() {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return
	}
	b.closing = true
	h := b.helper
	for _, p := range b.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
	}
	b.mu.Unlock()
	if h == nil {
		return
	}
	h.stdin.Close()
	select {
	case <-h.done:
		return
	case <-time.After(3 * time.Second):
	}
	pgid := h.cmd.Process.Pid
	syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-h.done:
		return
	case <-time.After(2 * time.Second):
	}
	syscall.Kill(-pgid, syscall.SIGKILL)
	<-h.done
}

func (b *Bridge) write(msg []byte) {
	b.outMu.Lock()
	defer b.outMu.Unlock()
	b.Out.Write(append(append([]byte{}, msg...), '\n'))
}

func (b *Bridge) fromClient(raw []byte) {
	raw = []byte(strings.TrimRight(string(raw), "\r\n"))
	m, err := Parse(raw)
	if err != nil {
		b.write(errorResponse(json.RawMessage("null"), -32700, "parse error"))
		return
	}

	b.mu.Lock()
	degraded := b.degraded
	switch m.Method {
	case "initialize":
		var req map[string]any
		json.Unmarshal(raw, &req)
		b.initReq = req
	case "notifications/initialized":
		b.initialized = true
	}
	b.mu.Unlock()

	if degraded != "" {
		b.answerLocally(m, degraded)
		return
	}

	if m.IsRequest() && m.Method == "tools/call" && b.StudioRunning != nil && !b.StudioRunning() {
		if b.Autostart == nil {
			b.write(toolError(m.ID, NotRunning))
			return
		}
		b.track(m, nil)
		go b.autostartThenForward(raw, m)
		return
	}

	if m.IsRequest() {
		var timer *time.Timer
		if m.Method == "tools/list" {
			id := m.ID
			timer = time.AfterFunc(b.ListTimeout, func() { b.listTimeout(id) })
		}
		b.track(m, timer)
	}
	b.forward(raw, m)
}

func (b *Bridge) track(m *Message, timer *time.Timer) {
	b.mu.Lock()
	b.pending[IDKey(m.ID)] = pendingReq{method: m.Method, timer: timer}
	b.mu.Unlock()
}

// listTimeout answers a tools/list the helper hasn't answered yet. The
// helper waits for Studio to push its tools; when Studio connects later the
// helper sends notifications/tools/list_changed and the client asks again.
func (b *Bridge) listTimeout(id json.RawMessage) {
	key := IDKey(id)
	b.mu.Lock()
	if _, ok := b.pending[key]; !ok || b.closing {
		b.mu.Unlock()
		return
	}
	delete(b.pending, key)
	b.answered[key] = true
	b.mu.Unlock()
	b.Log.Printf("tools/list not answered within %s (Studio not connected yet); replying with no tools", b.ListTimeout)
	b.write(response(id, map[string]any{"tools": []any{}}))
}

func (b *Bridge) forward(raw []byte, m *Message) {
	b.mu.Lock()
	h := b.helper
	b.mu.Unlock()
	if h == nil {
		if m.IsRequest() {
			b.fail(m.ID, "Studio's MCP helper is not running (restarting); try again in a few seconds")
		}
		return
	}
	h.writeMu.Lock()
	_, err := h.stdin.Write(append(append([]byte{}, raw...), '\n'))
	h.writeMu.Unlock()
	if err != nil && m.IsRequest() {
		b.fail(m.ID, "Studio's MCP helper is not accepting input: "+err.Error())
	}
}

// fail answers a pending client request with an error.
func (b *Bridge) fail(id json.RawMessage, text string) {
	key := IDKey(id)
	b.mu.Lock()
	p, ok := b.pending[key]
	if ok {
		delete(b.pending, key)
		if p.timer != nil {
			p.timer.Stop()
		}
		b.answered[key] = true
	}
	b.mu.Unlock()
	if p.method == "tools/call" {
		b.write(toolError(id, text))
	} else {
		b.write(errorResponse(id, -32603, text))
	}
}

func (b *Bridge) fromHelper(line []byte) {
	msg, ok := Clean(line)
	if !ok {
		if s := strings.TrimSpace(string(line)); s != "" {
			b.Log.Printf("dropped non-JSON helper output: %q", truncate(s, 200))
		}
		return
	}
	m, err := Parse(msg)
	if err != nil {
		return
	}
	if m.IsResponse() {
		key := IDKey(m.ID)
		b.mu.Lock()
		if ch, ok := b.internal[key]; ok {
			delete(b.internal, key)
			b.mu.Unlock()
			ch <- m
			return
		}
		if b.answered[key] {
			delete(b.answered, key)
			b.mu.Unlock()
			return
		}
		if p, ok := b.pending[key]; ok {
			if p.timer != nil {
				p.timer.Stop()
			}
			delete(b.pending, key)
		}
		b.mu.Unlock()
	}
	b.write(msg)
}

// call sends a request of the bridge's own to the helper.
func (b *Bridge) call(method string, params any, timeout time.Duration) (*Message, error) {
	b.mu.Lock()
	h := b.helper
	b.nextID++
	id := fmt.Sprintf("%q", fmt.Sprintf("%s%d", internalPrefix, b.nextID))
	ch := make(chan *Message, 1)
	b.internal[id] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.internal, id)
		b.mu.Unlock()
	}()
	if h == nil {
		return nil, errors.New("helper not running")
	}
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	h.writeMu.Lock()
	_, err := h.stdin.Write(append(req, '\n'))
	h.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		return m, nil
	case <-h.done:
		return nil, errors.New("helper exited")
	case <-time.After(timeout):
		return nil, fmt.Errorf("%s timed out after %s", method, timeout)
	}
}

// ConnectedStudios asks the helper which Studio instances are connected.
func (b *Bridge) ConnectedStudios(timeout time.Duration) ([]map[string]any, error) {
	m, err := b.call("tools/call", map[string]any{"name": "list_roblox_studios", "arguments": map[string]any{}}, timeout)
	if err != nil {
		return nil, err
	}
	return ParseStudios(m.Result)
}

// ParseStudios extracts the studio list from a list_roblox_studios result.
func ParseStudios(result json.RawMessage) ([]map[string]any, error) {
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil, err
	}
	if r.IsError || len(r.Content) == 0 {
		return nil, fmt.Errorf("list_roblox_studios failed: %s", string(result))
	}
	var body struct {
		Studios []map[string]any `json:"studios"`
	}
	if err := json.Unmarshal([]byte(r.Content[0].Text), &body); err != nil {
		return nil, err
	}
	return body.Studios, nil
}

func (b *Bridge) autostartThenForward(raw []byte, m *Message) {
	b.mu.Lock()
	first := !b.autostarted
	b.autostarted = true
	b.mu.Unlock()
	if first {
		b.Log.Printf("Studio not running; starting it (autostart_studio)")
		if err := b.Autostart(); err != nil {
			b.fail(m.ID, "Could not start Studio: "+err.Error())
			return
		}
	}
	deadline := time.Now().Add(b.StartTimeout)
	for time.Now().Before(deadline) {
		if b.StudioRunning() {
			if s, err := b.ConnectedStudios(5 * time.Second); err == nil && len(s) > 0 {
				b.forward(raw, m)
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	b.mu.Lock()
	b.autostarted = false
	b.mu.Unlock()
	b.fail(m.ID, fmt.Sprintf("Studio was started but did not connect within %s. Sign in, open a place, and check that "+
		"\"Enable Studio as MCP server\" is on (Assistant → … → Manage MCP Servers).", b.StartTimeout))
}

func (b *Bridge) startHelper() error {
	cmd, err := b.NewHelper()
	if err != nil {
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if b.HelperLog != nil {
		cmd.Stderr = b.HelperLog
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start helper: %w", err)
	}
	b.Log.Printf("started helper pid %d: %s", cmd.Process.Pid, strings.Join(cmd.Args, " "))
	h := &helperProc{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	b.mu.Lock()
	b.helper = h
	b.mu.Unlock()

	go func() {
		r := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				b.fromHelper(line)
			}
			if err != nil {
				break
			}
		}
		err := cmd.Wait()
		close(h.done)
		b.helperExited(h, err)
	}()
	return nil
}

func (b *Bridge) helperExited(h *helperProc, err error) {
	b.mu.Lock()
	if b.helper == h {
		b.helper = nil
	}
	closing := b.closing
	var owed []json.RawMessage
	for k := range b.pending {
		owed = append(owed, json.RawMessage(k))
	}
	b.mu.Unlock()
	if closing {
		return
	}
	b.Log.Printf("helper exited unexpectedly: %v", err)
	for _, id := range owed {
		b.fail(id, "Studio's MCP helper stopped (was Studio or its wineserver killed?). Retry the call.")
	}
	go b.restartLoop()
}

// restartLoop brings the helper back and replays the client's handshake so
// the client's session keeps working.
func (b *Bridge) restartLoop() {
	b.mu.Lock()
	if b.restarting {
		b.mu.Unlock()
		return
	}
	b.restarting = true
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.restarting = false
		b.mu.Unlock()
	}()

	delay := time.Second
	for {
		time.Sleep(delay)
		b.mu.Lock()
		closing := b.closing
		b.mu.Unlock()
		if closing {
			return
		}
		if err := b.startHelper(); err != nil {
			b.Log.Printf("restart helper: %v", err)
			if delay < 30*time.Second {
				delay *= 2
			}
			continue
		}
		b.replayHandshake()
		return
	}
}

func (b *Bridge) replayHandshake() {
	b.mu.Lock()
	req := b.initReq
	initialized := b.initialized
	b.mu.Unlock()
	if req == nil {
		return
	}
	params := req["params"]
	if _, err := b.call("initialize", params, 30*time.Second); err != nil {
		b.Log.Printf("replay initialize: %v", err)
		return
	}
	if initialized {
		b.mu.Lock()
		h := b.helper
		b.mu.Unlock()
		if h != nil {
			h.writeMu.Lock()
			h.stdin.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"))
			h.writeMu.Unlock()
		}
	}
	// The client's cached tool list may be stale after a restart.
	b.write([]byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`))
	b.Log.Printf("helper restarted and handshake replayed")
}

// answerLocally serves requests when the helper can't run at all.
func (b *Bridge) answerLocally(m *Message, reason string) {
	if !m.IsRequest() {
		return
	}
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(m.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		b.write(response(m.ID, map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "brine", "version": b.Version},
			"instructions":    "Brine cannot reach Roblox Studio: " + reason,
		}))
	case "ping":
		b.write(response(m.ID, map[string]any{}))
	case "tools/list":
		b.write(response(m.ID, map[string]any{"tools": []any{}}))
	case "tools/call":
		b.write(toolError(m.ID, "Brine cannot run Studio's MCP helper: "+reason))
	case "resources/list":
		b.write(response(m.ID, map[string]any{"resources": []any{}}))
	case "prompts/list":
		b.write(response(m.ID, map[string]any{"prompts": []any{}}))
	default:
		b.write(errorResponse(m.ID, -32601, "method not available: "+reason))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
