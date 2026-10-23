// Package webview installs the Edge WebView2 runtime that Studio uses for
// sign-in and its web-based panels.
package webview

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"brine/internal/dirs"
	"brine/internal/netx"
	"brine/internal/wine"
)

// Microsoft's update API is signed by "Microsoft Root Certificate Authority
// 2011", which most Linux CA bundles do not ship.
// SHA-256 847DF6A78497943F27FC72EB93F9A637320A02B561D0A91B09E87A7807ED7C61.
//
//go:embed microsoft-root-2011.pem
var microsoftRoot []byte

const api = "https://msedge.api.cdp.microsoft.com/api/v1.1/contents/Browser/namespaces/Default/names/msedge-stable-win-x64/versions/"

func client() *http.Client {
	pool, _ := x509.SystemCertPool()
	if pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(microsoftRoot)
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{RootCAs: pool}
	// The API rejects compressed requests.
	t.DisableCompression = true
	return &http.Client{Transport: t}
}

func post(url, body string, v any) error {
	resp, err := client().Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("WebView2 API: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Latest returns the newest stable WebView2 version.
func Latest() (string, error) {
	var r struct {
		ContentID struct {
			Version string `json:"Version"`
		} `json:"ContentId"`
	}
	err := post(api+"latest?action=select", `{"targetingAttributes":{"Updater":"MicrosoftEdgeUpdate"}}`, &r)
	return r.ContentID.Version, err
}

// runtimeURL finds the standalone WebView2 runtime installer for version.
// The file list mixes full Edge installers (MicrosoftEdge_X64_<v>_<edge>.exe)
// with the runtime (MicrosoftEdge_X64_<v>.exe).
func runtimeURL(version string) (url, sha256 string, err error) {
	var files []struct {
		URL    string `json:"Url"`
		File   string `json:"FileId"`
		Hashes struct {
			SHA256 string `json:"Sha256"`
		} `json:"Hashes"`
	}
	if err := post(api+version+"/files?action=GenerateDownloadInfo", "", &files); err != nil {
		return "", "", err
	}
	want := "MicrosoftEdge_X64_" + version + ".exe"
	for _, f := range files {
		if f.File == want {
			return f.URL, f.Hashes.SHA256, nil
		}
	}
	return "", "", fmt.Errorf("WebView2 %s runtime not found", version)
}

// Dir is where the runtime lands inside the prefix.
func Dir(prefix, version string) string {
	return filepath.Join(prefix, "drive_c", "Program Files (x86)", "Microsoft", "EdgeWebView", "Application", version)
}

// Installed reports whether version is installed in the prefix.
func Installed(prefix, version string) bool {
	_, err := os.Stat(filepath.Join(Dir(prefix, version), "msedgewebview2.exe"))
	return err == nil
}

// InstalledVersions lists every WebView2 runtime in the prefix.
func InstalledVersions(prefix string) []string {
	m, _ := filepath.Glob(filepath.Join(Dir(prefix, "*"), "msedgewebview2.exe"))
	var out []string
	for _, p := range m {
		out = append(out, filepath.Base(filepath.Dir(p)))
	}
	return out
}

// Install downloads and silently installs the WebView2 runtime.
func Install(pfx *wine.Prefix, version string) error {
	if version == "latest" {
		v, err := Latest()
		if err != nil {
			return err
		}
		version = v
	}
	if Installed(pfx.Dir, version) {
		return nil
	}
	url, sum, err := runtimeURL(version)
	if err != nil {
		return err
	}
	if netx.NormalizeSHA256(sum) == "" {
		return fmt.Errorf("WebView2 %s: the update service gave no usable checksum; refusing to run an unverified installer", version)
	}
	inst := filepath.Join(dirs.Cache, "webview2-"+version+".exe")
	if err := netx.Download(url, inst, netx.Verify{SHA256: sum}, netx.Bar("webview2")); err != nil {
		return err
	}
	// WebView2's renderer misbehaves when it sees a modern Windows version
	// under Wine; reporting Windows 7 to it alone is the usual workaround.
	if err := pfx.ImportReg("REGEDIT4\n\n[HKEY_CURRENT_USER\\Software\\Wine\\AppDefaults\\msedgewebview2.exe]\n\"Version\"=\"win7\"\n"); err != nil {
		return err
	}
	cmd := pfx.Command(inst, "--msedgewebview", "--do-not-launch-msedge", "--system-level")
	out, err := cmd.CombinedOutput()
	if err != nil && !Installed(pfx.Dir, version) {
		return fmt.Errorf("WebView2 installer: %w\n%s", err, lastLines(string(out), 15))
	}
	if !Installed(pfx.Dir, version) {
		return fmt.Errorf("WebView2 installer finished but %s is missing", Dir(pfx.Dir, version))
	}
	os.Remove(inst)
	return nil
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}
