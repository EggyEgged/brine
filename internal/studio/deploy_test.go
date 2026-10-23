package studio

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"brine/internal/dirs"
)

func TestParseManifest(t *testing.T) {
	m := "v0\r\nRobloxStudio.zip\r\n1bc110926621997b7f605ff07975293c\r\n120405488\r\n260246513\r\n" +
		"ssl.zip\n0a8b2e32e2d9600a5457275fb665e8ff\n130569\n228725\n"
	pkgs, err := ParseManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 || pkgs[0].Name != "RobloxStudio.zip" || pkgs[1].Packed != 130569 || pkgs[0].MD5 != "1bc110926621997b7f605ff07975293c" {
		t.Fatalf("got %+v", pkgs)
	}
	for _, bad := range []string{"", "v1\nx", "v0\nonly.zip\nabc\n1", "v0\na.zip\nnot-md5\n1\n2"} {
		if _, err := ParseManifest(bad); err == nil {
			t.Errorf("ParseManifest(%q) should fail", bad)
		}
	}
}

func TestDir(t *testing.T) {
	cases := map[string]struct {
		dir   string
		known bool
	}{
		"RobloxStudio.zip":             {"", true},
		"content-textures2.zip":        {"content/textures", true},
		"content-textures3.zip":        {"PlatformContent/pc/textures", true},
		"extracontent-luapackages.zip": {"ExtraContent/LuaPackages", true},
		"content-brandnew.zip":         {"content/brandnew", false},
		"studiocontent-new.zip":        {"StudioContent/new", false},
		"Mystery.zip":                  {"", false},
	}
	for pkg, want := range cases {
		d, k := Dir(pkg)
		if d != want.dir || k != want.known {
			t.Errorf("Dir(%s) = %q,%v want %q,%v", pkg, d, k, want.dir, want.known)
		}
	}
}

func TestRendererFlags(t *testing.T) {
	f := RendererFlags("vulkan")
	if f["FFlagDebugGraphicsPreferVulkan"] != true || f["FFlagDebugGraphicsDisableD3D11"] != true || f["FFlagDebugGraphicsDisableVulkan"] != false {
		t.Fatalf("bad vulkan flags: %v", f)
	}
	if RendererFlags("dxvk")["FFlagDebugGraphicsPreferD3D11"] != true {
		t.Fatal("dxvk should prefer D3D11")
	}
}

func TestRollbackAndState(t *testing.T) {
	old := dirs.Data
	dirs.Data = t.TempDir()
	defer func() { dirs.Data = old }()
	os.MkdirAll(dirs.Versions(), 0o755)

	st := &State{Current: "version-new", Previous: "version-old", Versions: map[string]Version{}}
	// The previous build must be complete on disk.
	if err := Rollback(st); err == nil {
		t.Fatal("rolled back to a build that isn't installed")
	}
	for _, g := range []string{"version-new", "version-old"} {
		os.MkdirAll(VersionDir(g), 0o755)
		os.WriteFile(filepath.Join(VersionDir(g), completeMarker), nil, 0o644)
	}
	if err := Rollback(st); err != nil {
		t.Fatal(err)
	}
	if st.Current != "version-old" || st.Previous != "version-new" || !st.Pinned {
		t.Fatalf("after rollback: %+v", st)
	}
	got, err := LoadState()
	if err != nil || got.Current != "version-old" || !got.Pinned {
		t.Fatalf("state not persisted: %+v %v", got, err)
	}
	if d, err := got.CurrentDir(); err != nil || filepath.Base(d) != "version-old" {
		t.Fatalf("CurrentDir %q %v", d, err)
	}
	// A rolled-back state can roll forward again.
	if err := Rollback(got); err != nil || got.Current != "version-new" {
		t.Fatalf("roll forward: %+v %v", got, err)
	}
}

func TestPruneKeepsCurrentAndPrevious(t *testing.T) {
	old := dirs.Data
	dirs.Data = t.TempDir()
	defer func() { dirs.Data = old }()
	os.MkdirAll(dirs.Versions(), 0o755)
	now := time.Now()
	st := &State{Current: "c", Previous: "p", Versions: map[string]Version{
		"c": {Installed: now}, "p": {Installed: now.Add(-time.Hour)},
		"o1": {Installed: now.Add(-2 * time.Hour)}, "o2": {Installed: now.Add(-3 * time.Hour)},
	}}
	for g := range st.Versions {
		os.MkdirAll(VersionDir(g), 0o755)
	}
	os.MkdirAll(filepath.Join(dirs.Versions(), ".tmp-leftover"), 0o755)
	prune(st, 2, func(string, ...any) {})
	for _, g := range []string{"c", "p"} {
		if _, err := os.Stat(VersionDir(g)); err != nil {
			t.Errorf("pruned %s", g)
		}
	}
	for _, g := range []string{"o1", "o2", ".tmp-leftover"} {
		p := VersionDir(g)
		if g == ".tmp-leftover" {
			p = filepath.Join(dirs.Versions(), g)
		}
		if _, err := os.Stat(p); err == nil {
			t.Errorf("kept %s", g)
		}
	}
}

func zipWith(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.zip")
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
	return path
}

func TestExtractMapsAndNormalisesPaths(t *testing.T) {
	root := t.TempDir()
	// Windows-style separators, as in Roblox's archives.
	z := zipWith(t, map[string]string{`Optimized_Embedded_Signature\a.rbxm`: "x", "top.rbxm": "y"})
	if err := extract(z, root, "BuiltInPlugins.zip"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"BuiltInPlugins/Optimized_Embedded_Signature/a.rbxm", "BuiltInPlugins/top.rbxm"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../evil", `..\evil`, "a/../../evil"} {
		z := zipWith(t, map[string]string{name: "x"})
		if err := extract(z, root, "RobloxStudio.zip"); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "evil")); err == nil {
		t.Fatal("file escaped the install directory")
	}
}
