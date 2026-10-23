package wine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"brine/internal/dirs"
	"brine/internal/netx"
)

// The managed runtime is a prebuilt Wine (LGPL-2.1) published by the
// Vinegar project with fixes for Studio. Brine only downloads and runs it.
const managedRepo = "vinegarhq/kombucha"

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Size   int64  `json:"size"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// ManagedVersion returns the installed managed build's directory name, or "".
func ManagedVersion() string {
	t, err := os.Readlink(ManagedDir())
	if err != nil {
		return ""
	}
	return filepath.Base(t)
}

// LatestManaged returns the newest managed build tag.
func LatestManaged() (string, error) {
	var r ghRelease
	if err := netx.GetJSON("https://api.github.com/repos/"+managedRepo+"/releases/latest", &r); err != nil {
		return "", err
	}
	return r.Tag, nil
}

// InstallManaged downloads the latest managed Wine build (if newer than the
// installed one) and points runtimes/current at it. Older builds are removed.
func InstallManaged(log func(string, ...any)) (string, error) {
	var r ghRelease
	if err := netx.GetJSON("https://api.github.com/repos/"+managedRepo+"/releases/latest", &r); err != nil {
		return "", fmt.Errorf("query Wine builds: %w", err)
	}
	var url, digest, name string
	for _, a := range r.Assets {
		if strings.HasSuffix(a.Name, ".tar.xz") {
			url, digest, name = a.URL, a.Digest, a.Name
			break
		}
	}
	if url == "" {
		return "", fmt.Errorf("release %s has no .tar.xz asset", r.Tag)
	}
	dirName := strings.TrimSuffix(name, ".tar.xz")
	dest := filepath.Join(dirs.Runtimes(), dirName)
	if ManagedVersion() == dirName {
		if _, err := fromDir(dest, Managed); err == nil {
			log("Wine build %s is up to date", dirName)
			return dirName, nil
		}
	}

	archive := filepath.Join(dirs.Cache, name)
	log("Downloading Wine build %s", dirName)
	if digest == "" {
		log("warning: GitHub published no checksum for %s; it can't be verified", name)
	}
	if err := netx.Download(url, archive, netx.Verify{SHA256: digest}, netx.Bar("wine")); err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp(dirs.Runtimes(), ".extract-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	log("Extracting %s", name)
	if out, err := exec.Command("tar", "-xJf", archive, "-C", tmp, "--no-same-owner").CombinedOutput(); err != nil {
		return "", fmt.Errorf("extract: %w: %s", err, out)
	}
	// The archive holds one top-level directory.
	ents, err := os.ReadDir(tmp)
	if err != nil || len(ents) != 1 || !ents[0].IsDir() {
		return "", fmt.Errorf("unexpected layout in %s", name)
	}
	os.RemoveAll(dest)
	if err := os.Rename(filepath.Join(tmp, ents[0].Name()), dest); err != nil {
		return "", err
	}
	if _, err := fromDir(dest, Managed); err != nil {
		return "", err
	}

	old := ManagedVersion()
	link := ManagedDir()
	os.Remove(link + ".new")
	if err := os.Symlink(dirName, link+".new"); err != nil {
		return "", err
	}
	if err := os.Rename(link+".new", link); err != nil {
		return "", err
	}
	if old != "" && old != dirName {
		os.RemoveAll(filepath.Join(dirs.Runtimes(), old))
	}
	os.Remove(archive)
	return dirName, nil
}
