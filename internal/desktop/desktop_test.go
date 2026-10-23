package desktop

import (
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	if quote("/home/u/.local/bin/brine") != "/home/u/.local/bin/brine" {
		t.Fatal("plain path should be unquoted")
	}
	if got := quote(`/home/a b/$x"`); got != `"/home/a b/\\$x\\""` {
		t.Fatalf("got %s", got)
	}
}

func TestEntry(t *testing.T) {
	e := entry("/usr/bin/brine")
	for _, want := range []string{"Exec=/usr/bin/brine run %u", "x-scheme-handler/roblox-studio-auth", "Name=Brine"} {
		if !strings.Contains(e, want) {
			t.Errorf("entry missing %q", want)
		}
	}
	if strings.Contains(strings.SplitN(e, "\n", 4)[2], "Roblox") {
		t.Error("program name must not contain Roblox")
	}
}

func TestParseExec(t *testing.T) {
	for entry, want := range map[string]string{
		entry("/usr/bin/brine"):                "/usr/bin/brine",
		entry("/home/a b/.local/brine"):        "/home/a b/.local/brine",
		entry(`/home/u/"x"/brine`):             `/home/u/"x"/brine`,
		"[Desktop Entry]\nExec=brine run %u\n": "brine",
	} {
		got, err := parseExec(entry)
		if err != nil || got != want {
			t.Errorf("parseExec = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := parseExec("[Desktop Entry]\nName=x\n"); err == nil {
		t.Error("accepted an entry without Exec=")
	}
}
