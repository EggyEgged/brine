package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStudioArgs(t *testing.T) {
	dir := t.TempDir()
	place := filepath.Join(dir, "My Place.rbxl")
	os.WriteFile(place, nil, 0o644)

	a, err := StudioArgs(place)
	if err != nil || len(a) != 4 || a[0] != "-task" || a[3] != "Z:"+windows(place) {
		t.Fatalf("got %v %v", a, err)
	}
	if a, _ := StudioArgs("roblox-studio-auth:/?code=1"); len(a) != 1 {
		t.Fatal("auth URI must be passed through")
	}
	if a, _ := StudioArgs(""); a != nil {
		t.Fatal("no args expected")
	}
	if _, err := StudioArgs(filepath.Join(dir, "missing.rbxl")); err == nil {
		t.Fatal("missing file accepted")
	}
	txt := filepath.Join(dir, "a.txt")
	os.WriteFile(txt, nil, 0o644)
	if _, err := StudioArgs(txt); err == nil {
		t.Fatal("non-place file accepted")
	}
}

func windows(p string) string {
	out := []byte(p)
	for i := range out {
		if out[i] == '/' {
			out[i] = '\\'
		}
	}
	return string(out)
}
