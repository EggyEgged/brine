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
