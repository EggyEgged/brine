package studio

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"brine/internal/dirs"
	"brine/internal/netx"
)

// State records which Studio builds are installed and which is active.
type State struct {
	Current  string             `json:"current"`
	Previous string             `json:"previous,omitempty"`
	Versions map[string]Version `json:"versions"`
}

type Version struct {
	Version   string    `json:"version"`
	Installed time.Time `json:"installed"`
}

const completeMarker = ".brine-complete"

func LoadState() (*State, error) {
	s := &State{Versions: map[string]Version{}}
	b, err := os.ReadFile(dirs.StateFile())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w", dirs.StateFile(), err)
	}
	if s.Versions == nil {
		s.Versions = map[string]Version{}
	}
	return s, nil
}

func (s *State) Save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := dirs.StateFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dirs.StateFile())
}

// VersionDir is the install directory of a build.
func VersionDir(guid string) string { return filepath.Join(dirs.Versions(), guid) }

// CurrentDir returns the active build's directory, or an error if Studio
// is not installed.
func (s *State) CurrentDir() (string, error) {
	if s.Current == "" {
		return "", errors.New("Studio is not installed (run `brine install`)")
	}
	d := VersionDir(s.Current)
	if !Complete(d) {
		return "", fmt.Errorf("Studio build %s is incomplete (run `brine install`)", s.Current)
	}
	return d, nil
}

func Complete(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, completeMarker))
	return err == nil
}

// Executable names inside a build directory.
const (
	StudioExe = "RobloxStudioBeta.exe"
	MCPExe    = "StudioMCP.exe"
)

// Install downloads rel into the versions directory (if needed) and makes it
// current. The previously current build is kept for rollback.
func Install(s *State, rel *Release, keep int, log func(string, ...any)) error {
	dir := VersionDir(rel.GUID)
	if !Complete(dir) {
		if err := download(rel, dir, log); err != nil {
			return err
		}
	} else {
		log("Studio %s is already downloaded", rel.Version)
	}
	if s.Current != rel.GUID {
		if s.Current != "" {
			s.Previous = s.Current
		}
		s.Current = rel.GUID
	}
	s.Versions[rel.GUID] = Version{Version: rel.Version, Installed: time.Now().UTC()}
	if err := s.Save(); err != nil {
		return err
	}
	prune(s, keep, log)
	return s.Save()
}

// Rollback swaps the current and previous builds.
func Rollback(s *State) error {
	if s.Previous == "" || !Complete(VersionDir(s.Previous)) {
		return errors.New("no previous Studio build to roll back to")
	}
	s.Current, s.Previous = s.Previous, s.Current
	return s.Save()
}

// prune removes old builds beyond keep, never touching current or previous.
func prune(s *State, keep int, log func(string, ...any)) {
	type v struct {
		guid string
		t    time.Time
	}
	var all []v
	for g, info := range s.Versions {
		all = append(all, v{g, info.Installed})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
	kept := 0
	for _, x := range all {
		if x.guid == s.Current || x.guid == s.Previous {
			kept++
			continue
		}
		if kept < keep {
			kept++
			continue
		}
		log("Removing old Studio build %s", x.guid)
		os.RemoveAll(VersionDir(x.guid))
		delete(s.Versions, x.guid)
	}
	// Remove leftovers of interrupted installs.
	ents, _ := os.ReadDir(dirs.Versions())
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			os.RemoveAll(filepath.Join(dirs.Versions(), e.Name()))
		}
	}
}

func download(rel *Release, dir string, log func(string, ...any)) error {
	pkgs, err := rel.Manifest()
	if err != nil {
		return fmt.Errorf("package manifest: %w", err)
	}
	var total int64
	for _, p := range pkgs {
		total += p.Packed
	}
	log("Downloading Studio %s (%d packages, %s)", rel.Version, len(pkgs), netx.Size(total))

	tmp := filepath.Join(dirs.Versions(), ".tmp-"+rel.GUID)
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	cache := filepath.Join(dirs.Cache, "packages")

	var (
		mu       sync.Mutex
		done     int64
		firstErr error
		wg       sync.WaitGroup
	)
	bar := netx.Bar("studio")
	sem := make(chan struct{}, 4)
	for _, p := range pkgs {
		wg.Add(1)
		go func(p Package) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mu.Lock()
			failed := firstErr != nil
			mu.Unlock()
			if failed {
				return
			}
			archive := filepath.Join(cache, p.MD5+"-"+p.Name)
			err := netx.Download(rel.url(p.Name), archive, p.MD5, nil)
			if err == nil {
				err = extract(archive, tmp, p.Name)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", p.Name, err)
			}
			done += p.Packed
			bar(done, total)
		}(p)
	}
	wg.Wait()
	if firstErr != nil {
		os.RemoveAll(tmp)
		return firstErr
	}

	appSettings := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n<Settings>\r\n" +
		"\t<ContentFolder>content</ContentFolder>\r\n" +
		"\t<BaseUrl>http://www.roblox.com</BaseUrl>\r\n</Settings>\r\n"
	if err := os.WriteFile(filepath.Join(tmp, "AppSettings.xml"), []byte(appSettings), 0o644); err != nil {
		return err
	}
	for _, exe := range []string{StudioExe, MCPExe} {
		if _, err := os.Stat(filepath.Join(tmp, exe)); err != nil {
			os.RemoveAll(tmp)
			return fmt.Errorf("downloaded build is missing %s", exe)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, completeMarker), []byte(rel.Version+"\n"), 0o644); err != nil {
		return err
	}
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return err
	}
	// Downloaded archives are only useful for retrying a failed install.
	for _, p := range pkgs {
		os.Remove(filepath.Join(cache, p.MD5+"-"+p.Name))
	}
	return nil
}

// extract unpacks one package archive into root/<mapped dir>.
func extract(archive, root, pkg string) error {
	sub, known := Dir(pkg)
	if !known {
		fmt.Fprintf(os.Stderr, "\n  warning: unknown package %s, installing to %q\n", pkg, sub)
	}
	base := filepath.Join(root, filepath.FromSlash(sub))
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.Trim(strings.ReplaceAll(f.Name, `\`, "/"), "/")
		if name == "" {
			continue
		}
		dest := filepath.Join(base, filepath.FromSlash(name))
		if !strings.HasPrefix(dest, base+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry escapes install dir: %q", f.Name)
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, `\`) || strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := writeEntry(f, dest); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
