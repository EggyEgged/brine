package logx

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	for _, in := range []string{
		"cookie _|WARNING:-DO-NOT-SHARE-THIS.--Sharing-this-will-allow-someone-to-log-in-as-you-and-to-steal-your-ROBUX-and-items.|_ABCDEF123 end",
		"brine run roblox-studio-auth:/?code=SECRETCODE&state=SECRETSTATE",
		"GET https://x/cb?code=SECRETCODE&x=1",
		"Authorization: Bearer SECRETTOKEN",
		`{"token":"SECRETTOKEN"}`,
	} {
		out := Redact(in)
		for _, s := range []string{"SECRET", "ABCDEF123"} {
			if strings.Contains(out, s) {
				t.Errorf("Redact(%q) = %q still contains %s", in, out, s)
			}
		}
	}
	if Redact("plain log line") != "plain log line" {
		t.Fatal("changed a harmless line")
	}
}
