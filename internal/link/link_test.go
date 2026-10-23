package link

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStudioLinks(t *testing.T) {
	for _, l := range []string{
		"roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1818+universeId:13058",
		"roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1",
		"roblox-studio:1+launchmode:plugin+task:InstallPlugin+pluginId:123+launchtime:1792784686123",
		"roblox-studio:1+launchmode:edit+task:TryAsset+assetId:4+x:a%20b",
	} {
		r, err := Resolve(l, nil)
		if err != nil || r.Kind != Studio || len(r.Args) != 1 || r.Args[0] != l || r.Canonical != l {
			t.Errorf("Resolve(%q) = %+v, %v", l, r, err)
		}
	}
}

// Anything that could split into extra Studio arguments, or isn't a plain link, is refused.
func TestStudioLinkInjection(t *testing.T) {
	for _, l := range []string{
		"roblox-studio:1+x -task EditFile -localPlaceFile Z:\\etc\\passwd",
		`roblox-studio:1+x" -localPlaceFile "Z:\tmp\evil.rbxl`,
		"roblox-studio:1+x\n-task EditFile",
		"roblox-studio:1+x\t-y",
		"roblox-studio:1+x'y",
		`roblox-studio:1+x\y`,
		"roblox-studio:1+x`id`",
		"roblox-studio:1+x$(id)",
		"roblox-studio:" + strings.Repeat("a", MaxLength),
		"roblox-studio:",
		"roblox-studio-auth:/?code=a b",
		"roblox-studio-auth:/?code=\"x\"",
	} {
		r, err := Resolve(l, nil)
		if err == nil {
			t.Errorf("Resolve(%q) accepted: %+v", l, r)
		}
	}
}

func TestAuthLink(t *testing.T) {
	l := "roblox-studio-auth:/?code=abc-DEF_123&state=s.t~u"
	r, err := Resolve(l, nil)
	if err != nil || r.Kind != Auth || r.Args[0] != l {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestEmptyAndUnknownSchemes(t *testing.T) {
	if r, err := Resolve("  ", nil); err != nil || r.Kind != None || r.Args != nil {
		t.Fatalf("%+v %v", r, err)
	}
	for _, l := range []string{"javascript:alert(1)", "ftp://x/y.rbxl", "roblox://placeId=1", "data:text/plain,hi"} {
		if _, err := Resolve(l, nil); err == nil {
			t.Errorf("accepted %q", l)
		}
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	odd := filepath.Join(dir, "My Place #1 100% café.rbxl")
	os.WriteFile(odd, nil, 0o644)
	plain := filepath.Join(dir, "a.rbxlx")
	os.WriteFile(plain, nil, 0o644)
	txt := filepath.Join(dir, "a.txt")
	os.WriteFile(txt, nil, 0o644)

	for _, in := range []string{
		odd,
		"file://" + strings.NewReplacer(" ", "%20", "#", "%23", "%", "%25", "é", "%C3%A9").Replace(odd),
		"file://localhost" + strings.NewReplacer(" ", "%20", "#", "%23", "%", "%25", "é", "%C3%A9").Replace(odd),
	} {
		r, err := Resolve(in, nil)
		if err != nil || r.Kind != File || r.Canonical != odd || len(r.Args) != 4 || r.Args[0] != "-task" || r.Args[3] != "Z:"+strings.ReplaceAll(odd, "/", `\`) {
			t.Errorf("Resolve(%q) = %+v, %v", in, r, err)
		}
	}
	if r, err := Resolve(plain, nil); err != nil || r.Kind != File {
		t.Fatalf("%+v %v", r, err)
	}
	for _, bad := range []string{txt, dir, filepath.Join(dir, "missing.rbxl"), "file://otherhost/tmp/a.rbxl", "file://%zz"} {
		if _, err := Resolve(bad, nil); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestWebLinks(t *testing.T) {
	lookup := func(u string) (string, error) {
		if u == "13058" {
			return "1818", nil
		}
		return "", errors.New("no such experience")
	}
	cases := map[string]string{
		"https://www.roblox.com/games/1818/Crossroads":                                       "roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1818",
		"https://roblox.com/games/1818":                                                      "roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1818",
		"https://www.roblox.com/de/games/1818/Crossroads?privateServerLinkCode=1":            "roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1818",
		"https://create.roblox.com/dashboard/creations/experiences/13058/overview":           "roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1818+universeId:13058",
		"https://create.roblox.com/dashboard/creations/experiences/13058/places/77/settings": "roblox-studio:1+launchmode:edit+task:EditPlace+placeId:77+universeId:13058",
	}
	for in, want := range cases {
		r, err := Resolve(in, lookup)
		if err != nil || r.Kind != Studio || r.Args[0] != want || r.Canonical != want {
			t.Errorf("Resolve(%q) = %+v, %v; want %s", in, r, err, want)
		}
	}
	for _, bad := range []string{
		"https://www.roblox.com/users/1/profile",
		"https://www.roblox.com/games/abc",
		"https://evil.example/games/1818",
		"https://www.roblox.com.evil.example/games/1818",
		"https://create.roblox.com/dashboard/creations/experiences/999/overview", // lookup fails
		"https://create.roblox.com/store",
	} {
		if _, err := Resolve(bad, lookup); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := Resolve("https://create.roblox.com/dashboard/creations/experiences/13058/overview", nil); err == nil {
		t.Error("dashboard address without a lookup should fail")
	}
}

func TestSchemeCaseNormalised(t *testing.T) {
	r, err := Resolve("ROBLOX-STUDIO:1+launchmode:edit+task:EditPlace+placeId:1", nil)
	if err != nil || r.Args[0] != "roblox-studio:1+launchmode:edit+task:EditPlace+placeId:1" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Resolve("Roblox-Studio-Auth:/?code=a", nil)
	if err != nil || r.Kind != Auth || r.Args[0] != "roblox-studio-auth:/?code=a" {
		t.Fatalf("%+v %v", r, err)
	}
}
