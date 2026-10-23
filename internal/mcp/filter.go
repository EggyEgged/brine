// Package mcp implements `brine mcp`: a stdio MCP server that runs
// Studio's own MCP helper (StudioMCP.exe) under Wine and guarantees that
// the client only ever sees clean JSON-RPC.
package mcp

import (
	"bytes"
	"encoding/json"
)

// Clean turns one raw line of helper output into a JSON-RPC message.
// It strips CR/LF line endings, a UTF-8 BOM and any non-JSON prefix that
// Wine may have written on the same line. ok is false when the line holds
// no JSON-RPC 2.0 object at all; such lines belong in the log, not stdout.
func Clean(line []byte) (msg []byte, ok bool) {
	line = bytes.TrimRight(line, "\r\n")
	line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, false
	}
	if line[0] != '{' {
		i := bytes.IndexByte(line, '{')
		if i < 0 {
			return nil, false
		}
		line = line[i:]
	}
	if !isJSONRPC(line) {
		return nil, false
	}
	return line, true
}

func isJSONRPC(b []byte) bool {
	if !json.Valid(b) {
		return false
	}
	var m struct {
		JSONRPC string `json:"jsonrpc"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	return m.JSONRPC == "2.0"
}

// Message is the subset of a JSON-RPC message the bridge routes on.
type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

func Parse(b []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// IDKey normalises a JSON-RPC id (number or string) for map lookups.
func IDKey(id json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, id); err != nil {
		return string(id)
	}
	return buf.String()
}

// IsRequest reports whether m is a request (has method and id).
func (m *Message) IsRequest() bool { return m.Method != "" && len(m.ID) > 0 && string(m.ID) != "null" }

// IsResponse reports whether m is a response (has id, no method).
func (m *Message) IsResponse() bool { return m.Method == "" && len(m.ID) > 0 }

// ToolName returns params.name for a tools/call request.
func (m *Message) ToolName() string {
	var p struct {
		Name string `json:"name"`
	}
	json.Unmarshal(m.Params, &p)
	return p.Name
}

func response(id json.RawMessage, result any) []byte {
	b, _ := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result})
	return b
}

func errorResponse(id json.RawMessage, code int, msg string) []byte {
	type rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	b, _ := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   rpcErr          `json:"error"`
	}{"2.0", id, rpcErr{code, msg}})
	return b
}

// toolError is a tools/call result the model can read and act on.
func toolError(id json.RawMessage, text string) []byte {
	return response(id, map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": true,
	})
}
