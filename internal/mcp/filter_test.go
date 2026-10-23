package mcp

import "testing"

func TestClean(t *testing.T) {
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"plain", `{"jsonrpc":"2.0","id":1,"result":{}}` + "\n", `{"jsonrpc":"2.0","id":1,"result":{}}`, true},
		{"crlf", `{"jsonrpc":"2.0","id":1,"result":{}}` + "\r\n", `{"jsonrpc":"2.0","id":1,"result":{}}`, true},
		{"double cr", `{"jsonrpc":"2.0","id":1,"result":{}}` + "\r\r\n", `{"jsonrpc":"2.0","id":1,"result":{}}`, true},
		{"bom", "\xef\xbb\xbf" + `{"jsonrpc":"2.0","method":"x"}`, `{"jsonrpc":"2.0","method":"x"}`, true},
		{"wine prefix noise", `0024:fixme:ntdll:NtQuerySystemInformation info_class 0x99 {"jsonrpc":"2.0","id":2,"result":{}}`, `{"jsonrpc":"2.0","id":2,"result":{}}`, true},
		{"wine debug line", "0024:err:module:import_dll Library foo.dll not found\n", "", false},
		{"cmd banner", "Microsoft Windows [Version 10.0.19045]\r\n", "", false},
		{"blank", "\r\n", "", false},
		{"not jsonrpc", `{"hello":"world"}`, "", false},
		{"wrong version", `{"jsonrpc":"1.0","id":1}`, "", false},
		{"truncated", `{"jsonrpc":"2.0","id":1,"result":`, "", false},
		{"array", `[{"jsonrpc":"2.0"}]`, "", false},
		{"ansi", "\x1b[33mwarning\x1b[0m", "", false},
		{"embedded escaped newline kept", `{"jsonrpc":"2.0","id":3,"result":{"text":"a\r\nb"}}` + "\r\n", `{"jsonrpc":"2.0","id":3,"result":{"text":"a\r\nb"}}`, true},
	}
	for _, c := range cases {
		got, ok := Clean([]byte(c.in))
		if ok != c.ok || string(got) != c.want {
			t.Errorf("%s: Clean(%q) = %q, %v; want %q, %v", c.name, c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestIDKey(t *testing.T) {
	if IDKey([]byte(` 7 `)) != "7" || IDKey([]byte(`"abc"`)) != `"abc"` {
		t.Fatal("IDKey does not normalise")
	}
	if IDKey([]byte(`1`)) == IDKey([]byte(`"1"`)) {
		t.Fatal("numeric and string ids must differ")
	}
}

func TestParseStudios(t *testing.T) {
	s, err := ParseStudios([]byte(`{"content":[{"type":"text","text":"{\"studios\":[{\"id\":\"a\",\"name\":\"Place1\"}]}"}],"isError":false}`))
	if err != nil || len(s) != 1 || s[0]["name"] != "Place1" {
		t.Fatalf("got %v, %v", s, err)
	}
	if _, err := ParseStudios([]byte(`{"content":[{"type":"text","text":"boom"}],"isError":true}`)); err == nil {
		t.Fatal("expected error for isError result")
	}
}
