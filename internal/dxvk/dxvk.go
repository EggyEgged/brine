// Package dxvk installs DXVK's 64-bit DLLs into a Wine prefix.
package dxvk

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"brine/internal/dirs"
	"brine/internal/netx"
)

// DLLs are the Direct3D DLLs DXVK replaces. Studio is 64-bit only.
var DLLs = []string{"d3d9", "d3d10core", "d3d11", "dxgi"}

func marker(prefix string) string { return filepath.Join(prefix, ".brine-dxvk") }

// Installed returns the DXVK version installed in the prefix, or "".
func Installed(prefix string) string {
	b, _ := os.ReadFile(marker(prefix))
	return strings.TrimSpace(string(b))
}

// Install downloads DXVK version and copies its x64 DLLs into
// drive_c/windows/system32. Wine only loads them while the DLL overrides
// from Overrides are set, so switching renderers needs no reinstall.
func Install(prefix, version string) error {
	if Installed(prefix) == version {
		return nil
	}
	url := fmt.Sprintf("https://github.com/doitsujin/dxvk/releases/download/v%s/dxvk-%s.tar.gz", version, version)
	archive := filepath.Join(dirs.Cache, "dxvk-"+version+".tar.gz")
	if err := netx.Download(url, archive, "", netx.Bar("dxvk")); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	sys32 := filepath.Join(prefix, "drive_c", "windows", "system32")
	tr := tar.NewReader(gz)
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(filepath.Dir(h.Name)) != "x64" || !strings.HasSuffix(h.Name, ".dll") {
			continue
		}
		dst := filepath.Join(sys32, filepath.Base(h.Name))
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
		n++
	}
	if n == 0 {
		return fmt.Errorf("no x64 DLLs in %s", filepath.Base(archive))
	}
	return os.WriteFile(marker(prefix), []byte(version+"\n"), 0o644)
}

// Overrides returns WINEDLLOVERRIDES entries: native DXVK when enabled,
// Wine's builtin wined3d otherwise.
func Overrides(enabled bool) map[string]string {
	mode := "b"
	if enabled {
		mode = "n,b"
	}
	o := map[string]string{}
	for _, d := range DLLs {
		o[d] = mode
	}
	return o
}
