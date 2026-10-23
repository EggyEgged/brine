package logx

import (
	"os"
	"path/filepath"
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

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	os.WriteFile(p, make([]byte, 100), 0o600)
	rotate(p, 1000)
	if _, err := os.Stat(p + ".1"); err == nil {
		t.Fatal("rotated a small file")
	}
	rotate(p, 50)
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Fatal("did not rotate a big file")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("original still present")
	}
}

func TestRedactWriterSplitSecret(t *testing.T) {
	var out strings.Builder
	w := &RedactWriter{W: &out}
	// A cookie and an auth URI, each cut in the middle across writes.
	chunks := []string{"start _|WARNING:-DO-NOT-SHARE-TH", "IS.--secret-part-ABC|_XYZ789 more\nrun roblox-studio-au", "th:/?code=SECRETCODE&state=s\ntail without newline code=LASTSECRET"}
	for _, c := range chunks {
		if n, err := w.Write([]byte(c)); err != nil || n != len(c) {
			t.Fatal(n, err)
		}
	}
	w.Flush()
	got := out.String()
	for _, s := range []string{"secret-part", "XYZ789", "SECRETCODE", "LASTSECRET"} {
		if strings.Contains(got, s) {
			t.Errorf("leaked %q in %q", s, got)
		}
	}
	if !strings.Contains(got, "more") || !strings.Contains(got, "tail without newline") {
		t.Errorf("harmless text lost: %q", got)
	}
}
