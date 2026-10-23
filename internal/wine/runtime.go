// Package wine locates a Wine runtime and runs programs in Brine's prefix.
package wine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"brine/internal/dirs"
)

type Kind string

const (
	System  Kind = "system"
	Managed Kind = "managed"
	Custom  Kind = "custom"
	Proton  Kind = "proton"
	UMU     Kind = "umu"
)

// Runtime is a resolved Wine installation.
type Runtime struct {
	Kind       Kind
	Wine       string // wine loader binary (or umu-run for UMU)
	Wineserver string
	// ProtonPath is set for UMU runtimes.
	ProtonPath string
}

var ErrNoRuntime = errors.New("no Wine runtime found: install Wine with your package manager or run `brine setup` to download one")

// ManagedDir is where the Brine-managed Wine build is linked.
func ManagedDir() string { return filepath.Join(dirs.Runtimes(), "current") }

// Resolve turns the config value ("auto", "system", "managed" or a path)
// into a Runtime.
func Resolve(spec, protonPath string) (*Runtime, error) {
	switch spec {
	case "", "auto":
		if rt, err := systemRuntime(); err == nil {
			return rt, nil
		}
		if rt, err := fromDir(ManagedDir(), Managed); err == nil {
			return rt, nil
		}
		return nil, ErrNoRuntime
	case "system":
		return systemRuntime()
	case "managed":
		rt, err := fromDir(ManagedDir(), Managed)
		if err != nil {
			return nil, fmt.Errorf("managed Wine build is not installed (run `brine setup` or `brine runtime update`): %w", err)
		}
		return rt, nil
	}
	return fromPath(expand(spec), protonPath)
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func systemRuntime() (*Runtime, error) {
	w, err := exec.LookPath("wine")
	if err != nil {
		return nil, fmt.Errorf("system Wine: %w", ErrNoRuntime)
	}
	rt := &Runtime{Kind: System, Wine: w}
	rt.Wineserver = findWineserver(filepath.Dir(w))
	if rt.Wineserver == "" {
		return nil, errors.New("system Wine found but wineserver is missing")
	}
	return rt, nil
}

func findWineserver(binDir string) string {
	candidates := []string{filepath.Join(binDir, "wineserver")}
	if p, err := exec.LookPath("wineserver"); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		"/usr/lib/wine/wineserver64", "/usr/lib/wine/wineserver",
		"/usr/lib64/wine/wineserver", "/usr/libexec/wine/wineserver",
		"/opt/wine-stable/bin/wineserver", "/opt/wine-staging/bin/wineserver")
	for _, c := range candidates {
		if isExec(c) {
			return c
		}
	}
	return ""
}

func fromPath(p, protonPath string) (*Runtime, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("wine runtime %q: %w", p, err)
	}
	if !st.IsDir() {
		if filepath.Base(p) == "umu-run" {
			if protonPath == "" {
				protonPath = os.Getenv("PROTONPATH")
			}
			if protonPath == "" {
				return nil, errors.New("umu-run requires wine.proton_path (a Proton directory, or \"GE-Proton\" to let UMU fetch it)")
			}
			rt := &Runtime{Kind: UMU, Wine: p, ProtonPath: expand(protonPath)}
			// For a local Proton directory, its wineserver can stop the prefix.
			rt.Wineserver = findWineserver(filepath.Join(rt.ProtonPath, "files", "bin"))
			return rt, nil
		}
		// A wine binary.
		rt := &Runtime{Kind: Custom, Wine: p, Wineserver: findWineserver(filepath.Dir(p))}
		if rt.Wineserver == "" {
			return nil, fmt.Errorf("no wineserver next to %s", p)
		}
		return rt, nil
	}
	if rt, err := fromDir(p, Custom); err == nil {
		return rt, nil
	}
	// Proton keeps Wine in files/ (or dist/ for older releases).
	for _, sub := range []string{"files", "dist"} {
		if rt, err := fromDir(filepath.Join(p, sub), Proton); err == nil {
			return rt, nil
		}
	}
	return nil, fmt.Errorf("%s does not look like a Wine or Proton build (no bin/wine)", p)
}

func fromDir(dir string, kind Kind) (*Runtime, error) {
	for _, name := range []string{"wine", "wine64"} {
		w := filepath.Join(dir, "bin", name)
		if isExec(w) {
			ws := filepath.Join(dir, "bin", "wineserver")
			if !isExec(ws) {
				return nil, fmt.Errorf("%s has no bin/wineserver", dir)
			}
			return &Runtime{Kind: kind, Wine: w, Wineserver: ws}, nil
		}
	}
	return nil, fmt.Errorf("%s has no bin/wine", dir)
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// Version returns `wine --version` output, e.g. "wine-11.20".
func (rt *Runtime) Version() (string, error) {
	if rt.Kind == UMU {
		return "umu (" + filepath.Base(rt.ProtonPath) + ")", nil
	}
	cmd := exec.Command(rt.Wine, "--version")
	cmd.Env = append(os.Environ(), "WINEDEBUG=-all")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (rt *Runtime) String() string {
	if rt.Kind == UMU {
		return fmt.Sprintf("umu: %s (PROTONPATH=%s)", rt.Wine, rt.ProtonPath)
	}
	return fmt.Sprintf("%s: %s", rt.Kind, rt.Wine)
}
