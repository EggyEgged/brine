package wine

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeWine(t *testing.T, root string, withServer bool) {
	t.Helper()
	bin := filepath.Join(root, "bin")
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, "wine"), []byte("#!/bin/sh\necho wine-9.0\n"), 0o755)
	if withServer {
		os.WriteFile(filepath.Join(bin, "wineserver"), []byte("#!/bin/sh\n"), 0o755)
	}
}

func TestResolveDirectory(t *testing.T) {
	d := t.TempDir()
	fakeWine(t, d, true)
	rt, err := Resolve(d, "")
	if err != nil || rt.Kind != Custom || rt.Wine != filepath.Join(d, "bin", "wine") || rt.Wineserver != filepath.Join(d, "bin", "wineserver") {
		t.Fatalf("got %+v, %v", rt, err)
	}
	if v, err := rt.Version(); err != nil || v != "wine-9.0" {
		t.Fatalf("version %q %v", v, err)
	}
}

func TestResolveProtonLayout(t *testing.T) {
	d := t.TempDir()
	fakeWine(t, filepath.Join(d, "files"), true)
	rt, err := Resolve(d, "")
	if err != nil || rt.Kind != Proton {
		t.Fatalf("got %+v, %v", rt, err)
	}
}

func TestResolveBinaryNeedsWineserver(t *testing.T) {
	d := t.TempDir()
	fakeWine(t, d, false)
	if _, err := Resolve(filepath.Join(d, "bin", "wine"), ""); err == nil {
		t.Fatal("accepted a wine binary with no wineserver")
	}
	fakeWine(t, d, true)
	if rt, err := Resolve(filepath.Join(d, "bin", "wine"), ""); err != nil || rt.Wineserver == "" {
		t.Fatalf("got %+v, %v", rt, err)
	}
}

func TestResolveRejectsJunk(t *testing.T) {
	d := t.TempDir()
	if _, err := Resolve(d, ""); err == nil {
		t.Fatal("empty dir accepted")
	}
	if _, err := Resolve(filepath.Join(d, "nope"), ""); err == nil {
		t.Fatal("missing path accepted")
	}
}

func TestResolveUMU(t *testing.T) {
	d := t.TempDir()
	umu := filepath.Join(d, "umu-run")
	os.WriteFile(umu, []byte("#!/bin/sh\n"), 0o755)
	if _, err := Resolve(umu, ""); err == nil {
		t.Fatal("umu-run without a Proton path accepted")
	}
	rt, err := Resolve(umu, "GE-Proton")
	if err != nil || rt.Kind != UMU || rt.ProtonPath != "GE-Proton" {
		t.Fatalf("got %+v, %v", rt, err)
	}
}

func TestEnvironIsolatesWineVariables(t *testing.T) {
	t.Setenv("WINEPREFIX", "/somewhere/else")
	t.Setenv("WINEDEBUG", "+all")
	t.Setenv("WINESERVER", "/usr/bin/other-wineserver")
	d := t.TempDir()
	fakeWine(t, d, true)
	rt, _ := Resolve(d, "")
	p := NewPrefix("/pfx", rt)
	got := map[string]string{}
	for _, kv := range p.Environ() {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				got[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	if got["WINEPREFIX"] != "/pfx" || got["WINEDEBUG"] != "-all" || got["WINESERVER"] != rt.Wineserver {
		t.Fatalf("inherited Wine variables leaked: %v", got)
	}
}

func TestUserDirDetection(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "drive_c", "users", "Public"), 0o755)
	os.MkdirAll(filepath.Join(d, "drive_c", "users", "someone-else"), 0o755)
	p := &Prefix{Dir: d}
	t.Setenv("USER", "alice")
	if got := p.UserDir(); filepath.Base(got) != "someone-else" {
		t.Fatalf("want the on-disk user, got %s", got)
	}
	if got := p.LocalAppData(); filepath.Base(filepath.Dir(filepath.Dir(got))) != "someone-else" {
		t.Fatalf("LocalAppData %s", got)
	}
}
