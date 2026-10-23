package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestMain lets the test binary double as a fake StudioMCP.exe that
// behaves like the real one under Wine: noisy stderr, a banner and debug
// lines on stdout, CRLF line endings, and a slow tools/list.
func TestMain(m *testing.M) {
	if os.Getenv("BRINE_FAKE_HELPER") == "1" {
		fakeHelper()
		return
	}
	os.Exit(m.Run())
}

func fakeHelper() {
	fmt.Fprintln(os.Stderr, "kombucha: ntsync is unavailable")
	fmt.Print("Microsoft Windows [Version 10.0.19045]\r\n")
	fmt.Print("0024:fixme:ntdll:NtQuerySystemInformation info_class 0x99\n")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.Unmarshal(sc.Bytes(), &req)
		switch req.Method {
		case "initialize":
			fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{"listChanged":true}},"serverInfo":{"name":"RobloxStudio","version":"1.0.0"}}}`+"\r\n", req.ID)
		case "tools/list":
			go func(id json.RawMessage) {
				time.Sleep(400 * time.Millisecond)
				fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"late"}]}}`+"\r\n", id)
			}(req.ID)
		case "tools/call":
			if req.Params.Name == "die" {
				os.Exit(3)
			}
			fmt.Printf(`0030:err:winsock:x {"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"{\"studios\":[{\"id\":\"s1\",\"name\":\"Place1\"}]}"}],"isError":false}}`+"\r\n", req.ID)
		}
	}
}

type harness struct {
	t      *testing.T
	in     *io.PipeWriter
	out    *bufio.Reader
	done   chan struct{}
	bridge *Bridge
}

func newHarness(t *testing.T, running bool, helperErr error) *harness {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var studio atomic.Bool
	studio.Store(running)
	b := &Bridge{
		In:  inR,
		Out: outW,
		Log: log.New(io.Discard, "", 0),
		NewHelper: func() (*exec.Cmd, error) {
			if helperErr != nil {
				return nil, helperErr
			}
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), "BRINE_FAKE_HELPER=1")
			return cmd, nil
		},
		HelperLog:     io.Discard,
		StudioRunning: studio.Load,
		ListTimeout:   100 * time.Millisecond,
	}
	h := &harness{t: t, in: inW, out: bufio.NewReader(outR), done: make(chan struct{}), bridge: b}
	go func() {
		b.Run()
		outW.Close()
		close(h.done)
	}()
	return h
}

func (h *harness) send(s string) {
	if _, err := io.WriteString(h.in, s+"\n"); err != nil {
		h.t.Fatal(err)
	}
}

// recv reads one line and checks it is exactly one LF-terminated JSON-RPC object.
func (h *harness) recv() map[string]any {
	h.t.Helper()
	ch := make(chan string, 1)
	go func() {
		l, _ := h.out.ReadString('\n')
		ch <- l
	}()
	select {
	case l := <-ch:
		if !strings.HasSuffix(l, "\n") || strings.Contains(l, "\r") {
			h.t.Fatalf("bad line framing: %q", l)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil || m["jsonrpc"] != "2.0" {
			h.t.Fatalf("stdout carried non-JSON-RPC: %q", l)
		}
		return m
	case <-time.After(5 * time.Second):
		h.t.Fatal("timed out waiting for output")
	}
	return nil
}

func (h *harness) close() {
	h.in.Close()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		h.t.Fatal("bridge did not exit after stdin closed")
	}
}

const initReq = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

func TestBridgeFiltersAndForwards(t *testing.T) {
	h := newHarness(t, true, nil)
	h.send(initReq)
	if r := h.recv(); r["id"].(float64) != 1 || r["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "RobloxStudio" {
		t.Fatalf("bad initialize: %v", r)
	}
	h.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	h.send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_roblox_studios","arguments":{}}}`)
	r := h.recv()
	if r["id"].(float64) != 2 || r["result"] == nil {
		t.Fatalf("bad tools/call: %v", r)
	}
	h.close()
}

func TestBridgeListTimeoutDropsLateReply(t *testing.T) {
	h := newHarness(t, true, nil)
	h.send(initReq)
	h.recv()
	h.send(`{"jsonrpc":"2.0","id":"L","method":"tools/list"}`)
	r := h.recv()
	if r["id"] != "L" || len(r["result"].(map[string]any)["tools"].([]any)) != 0 {
		t.Fatalf("expected empty tool list from timeout, got %v", r)
	}
	// The helper's late answer (after 400ms) must not reach the client as a
	// second response to the same id.
	h.send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"x","arguments":{}}}`)
	time.Sleep(600 * time.Millisecond)
	r = h.recv()
	if r["id"].(float64) != 3 {
		t.Fatalf("expected response 3, got %v", r)
	}
	h.close()
}

func TestBridgeStudioNotRunning(t *testing.T) {
	h := newHarness(t, false, nil)
	h.send(initReq)
	h.recv()
	start := time.Now()
	h.send(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"execute_luau","arguments":{"code":"print(1)"}}}`)
	r := h.recv()
	res := r["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(fmt.Sprint(res["content"]), "not running") {
		t.Fatalf("expected not-running error, got %v", r)
	}
	if time.Since(start) > time.Second {
		t.Fatal("not-running error was slow")
	}
	h.close()
}

func TestBridgeDegraded(t *testing.T) {
	h := newHarness(t, false, fmt.Errorf("Studio is not installed"))
	h.send(initReq)
	r := h.recv()
	if r["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "brine" {
		t.Fatalf("expected local initialize, got %v", r)
	}
	h.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if r := h.recv(); r["id"].(float64) != 2 {
		t.Fatalf("bad tools/list: %v", r)
	}
	h.send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"x"}}`)
	r = h.recv()
	if r["result"].(map[string]any)["isError"] != true {
		t.Fatalf("expected error result: %v", r)
	}
	h.send(`{"jsonrpc":"2.0","id":4,"method":"bogus"}`)
	if r := h.recv(); r["error"] == nil {
		t.Fatalf("expected JSON-RPC error: %v", r)
	}
	h.close()
}

func TestBridgeParseError(t *testing.T) {
	h := newHarness(t, true, nil)
	h.send(`not json`)
	if r := h.recv(); r["error"] == nil {
		t.Fatalf("expected parse error, got %v", r)
	}
	h.close()
}

func TestBridgeRestartsHelperAndReplaysHandshake(t *testing.T) {
	h := newHarness(t, true, nil)
	h.send(initReq)
	h.recv()
	h.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	// Simulate `brine kill`: the helper dies mid-call.
	h.send(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"die","arguments":{}}}`)
	r := h.recv()
	if r["id"].(float64) != 7 || r["result"].(map[string]any)["isError"] != true {
		t.Fatalf("expected error for the interrupted call, got %v", r)
	}
	// After the restart the client is told to refresh its tools...
	r = h.recv()
	if r["method"] != "notifications/tools/list_changed" {
		t.Fatalf("expected list_changed after restart, got %v", r)
	}
	// ...and calls work again without a new initialize from the client.
	h.send(`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"list_roblox_studios","arguments":{}}}`)
	r = h.recv()
	if r["id"].(float64) != 8 || r["result"].(map[string]any)["isError"] == true {
		t.Fatalf("call after restart failed: %v", r)
	}
	h.close()
}
