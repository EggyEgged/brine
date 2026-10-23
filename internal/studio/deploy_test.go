package studio

import "testing"

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
