package wine

import "testing"

func TestIsProgram(t *testing.T) {
	yes := [][]string{
		{"/home/u/.local/share/brine/versions/v/RobloxStudioBeta.exe"},
		{`C:\x\RobloxStudioBeta.exe`, "-task", "EditFile"},
		{"/opt/wine/lib/wine/x86_64-unix/wine", "/v/RobloxStudioBeta.exe", "-task"},
		{"wine64", `Z:\v\robloxstudiobeta.EXE`},
	}
	no := [][]string{
		{`C:\Program Files (x86)\Microsoft\EdgeWebView\Application\1\msedgewebview2.exe`, "--webview-exe-name=RobloxStudioBeta.exe"},
		{"/usr/bin/grep", "RobloxStudioBeta.exe"},
		{"wine", "StudioMCP.exe"},
		{},
	}
	for _, a := range yes {
		if !IsProgram(a, "RobloxStudioBeta.exe") {
			t.Errorf("IsProgram(%q) = false", a)
		}
	}
	for _, a := range no {
		if IsProgram(a, "RobloxStudioBeta.exe") {
			t.Errorf("IsProgram(%q) = true", a)
		}
	}
}

func TestWinPath(t *testing.T) {
	if got := WinPath("/home/u/My Place.rbxl"); got != `Z:\home\u\My Place.rbxl` {
		t.Fatalf("got %s", got)
	}
}
