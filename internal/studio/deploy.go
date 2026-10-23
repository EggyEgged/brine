// Package studio installs, updates and rolls back Studio builds from
// Roblox's public deployment CDN.
package studio

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"

	"brine/internal/netx"
)

const (
	clientVersionURL = "https://clientsettingscdn.roblox.com/v2/client-version/WindowsStudio64"
	cdnBase          = "https://setup.rbxcdn.com/"
)

// Release is one deployed Studio build.
type Release struct {
	Version string `json:"version"`             // human version, e.g. 0.744.0.7441077
	GUID    string `json:"clientVersionUpload"` // e.g. version-7aaa6838d36c4448
}

// Latest asks Roblox which Studio build is current.
func Latest() (*Release, error) { return LatestWithin(netx.APITimeout) }

// LatestWithin is Latest with a custom deadline, for checks that must not
// delay starting Studio.
func LatestWithin(d time.Duration) (*Release, error) {
	var r Release
	if err := netx.GetJSONTimeout(clientVersionURL, d, &r); err != nil {
		return nil, fmt.Errorf("query current Studio version: %w", err)
	}
	if !strings.HasPrefix(r.GUID, "version-") {
		return nil, fmt.Errorf("unexpected Studio version %q", r.GUID)
	}
	return &r, nil
}

// Package is one entry of rbxPkgManifest.txt.
type Package struct {
	Name     string
	MD5      string
	Packed   int64
	Unpacked int64
}

func (r *Release) url(file string) string { return cdnBase + r.GUID + "-" + file }

// Manifest fetches and parses the build's package manifest.
func (r *Release) Manifest() ([]Package, error) {
	b, err := netx.GetBytes(r.url("rbxPkgManifest.txt"))
	if err != nil {
		return nil, err
	}
	return ParseManifest(string(b))
}

// ParseManifest parses the "v0" manifest format: a version line followed by
// groups of four lines (file name, md5, packed size, unpacked size).
func ParseManifest(s string) ([]Package, error) {
	sc := bufio.NewScanner(strings.NewReader(s))
	var lines []string
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 || lines[0] != "v0" {
		return nil, fmt.Errorf("unsupported package manifest format")
	}
	lines = lines[1:]
	if len(lines)%4 != 0 {
		return nil, fmt.Errorf("truncated package manifest (%d lines)", len(lines))
	}
	var pkgs []Package
	for i := 0; i < len(lines); i += 4 {
		packed, err1 := strconv.ParseInt(lines[i+2], 10, 64)
		unpacked, err2 := strconv.ParseInt(lines[i+3], 10, 64)
		if err1 != nil || err2 != nil || len(lines[i+1]) != 32 {
			return nil, fmt.Errorf("bad manifest entry for %s", lines[i])
		}
		pkgs = append(pkgs, Package{Name: lines[i], MD5: lines[i+1], Packed: packed, Unpacked: unpacked})
	}
	return pkgs, nil
}

// packageDirs maps package archives to their directory inside the Studio
// install. Archives not listed fall back to Dir's prefix rules. Derived by
// comparing each archive's contents with a working Windows install.
var packageDirs = map[string]string{
	"ApplicationConfig.zip":             "ApplicationConfig",
	"BuiltInPlugins.zip":                "BuiltInPlugins",
	"BuiltInStandalonePlugins.zip":      "BuiltInStandalonePlugins",
	"Plugins.zip":                       "Plugins",
	"StudioFonts.zip":                   "StudioFonts",
	"WebView2.zip":                      "",
	"WebView2RuntimeInstaller.zip":      "WebView2RuntimeInstaller",
	"RobloxStudio.zip":                  "",
	"Libraries.zip":                     "",
	"LibrariesQt5.zip":                  "",
	"redist.zip":                        "",
	"shaders.zip":                       "shaders",
	"ssl.zip":                           "ssl",
	"content-avatar.zip":                "content/avatar",
	"content-configs.zip":               "content/configs",
	"content-fonts.zip":                 "content/fonts",
	"content-models.zip":                "content/models",
	"content-qt_translations.zip":       "content/qt_translations",
	"content-sky.zip":                   "content/sky",
	"content-sounds.zip":                "content/sounds",
	"content-textures2.zip":             "content/textures",
	"content-studio_svg_textures.zip":   "content/studio_svg_textures",
	"content-api-docs.zip":              "content/api_docs",
	"content-textures3.zip":             "PlatformContent/pc/textures",
	"content-terrain.zip":               "PlatformContent/pc/terrain",
	"content-platform-fonts.zip":        "PlatformContent/pc/fonts",
	"content-platform-dictionaries.zip": "PlatformContent/pc/shared_compression_dictionaries",
	"extracontent-scripts.zip":          "ExtraContent/scripts",
	"extracontent-luapackages.zip":      "ExtraContent/LuaPackages",
	"extracontent-translations.zip":     "ExtraContent/translations",
	"extracontent-models.zip":           "ExtraContent/models",
	"extracontent-textures.zip":         "ExtraContent/textures",
	"studiocontent-models.zip":          "StudioContent/models",
	"studiocontent-textures.zip":        "StudioContent/textures",
}

// Dir returns the install subdirectory for a package and whether the
// mapping was known (as opposed to guessed from the name).
func Dir(pkg string) (string, bool) {
	if d, ok := packageDirs[pkg]; ok {
		return d, true
	}
	base := strings.TrimSuffix(pkg, ".zip")
	for prefix, root := range map[string]string{
		"content-":       "content/",
		"extracontent-":  "ExtraContent/",
		"studiocontent-": "StudioContent/",
	} {
		if rest, ok := strings.CutPrefix(base, prefix); ok {
			return root + rest, false
		}
	}
	return "", false
}
