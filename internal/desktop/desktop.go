// Package desktop installs Brine's user-level XDG integration: the .desktop
// entry, MIME types for places, the roblox-studio: and roblox-studio-auth:
// URI handlers, and the icon.
package desktop

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"brine/internal/dirs"
)

//go:embed brine.svg
var icon []byte

const (
	DesktopID = "brine.desktop"
	mimeFile  = "brine-studio.xml"
)

// Handled lists the MIME types and URI schemes Brine registers for.
var Handled = []string{
	"x-scheme-handler/roblox-studio",
	"x-scheme-handler/roblox-studio-auth",
	"application/x-roblox-place",
	"application/x-roblox-place+xml",
}

func DesktopPath() string { return filepath.Join(dirs.XDGData, "applications", DesktopID) }
func mimePath() string    { return filepath.Join(dirs.XDGData, "mime", "packages", mimeFile) }
func IconPath() string {
	return filepath.Join(dirs.XDGData, "icons", "hicolor", "scalable", "apps", "brine.svg")
}

func entry(exe string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Brine
GenericName=Studio Launcher
Comment=Run Roblox Studio under Wine, with MCP support
Exec=%s run %%u
TryExec=%s
Icon=brine
Terminal=false
StartupNotify=true
StartupWMClass=robloxstudiobeta.exe
Categories=Development;Game;
Keywords=studio;wine;mcp;
MimeType=%s;
Actions=kill;

[Desktop Action kill]
Name=Stop Studio
Exec=%s kill
`, quote(exe), quote(exe), strings.Join(Handled, ";"), quote(exe))
}

// quote escapes a path for a .desktop Exec key.
func quote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\$`") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", "$", `\\$`)
	return `"` + r.Replace(s) + `"`
}

const mimeXML = `<?xml version="1.0" encoding="UTF-8"?>
<mime-info xmlns="http://www.freedesktop.org/standards/shared-mime-info">
  <mime-type type="application/x-roblox-place">
    <comment>Studio place</comment>
    <glob pattern="*.rbxl"/>
  </mime-type>
  <mime-type type="application/x-roblox-place+xml">
    <comment>Studio place (XML)</comment>
    <sub-class-of type="application/xml"/>
    <glob pattern="*.rbxlx"/>
  </mime-type>
</mime-info>
`

// Install writes the desktop entry, MIME definitions and icon, then makes
// Brine the default handler. exe is the absolute path of the brine binary.
func Install(exe string) []error {
	var errs []error
	write := func(path string, data []byte) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			errs = append(errs, err)
			return
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			errs = append(errs, err)
		}
	}
	write(DesktopPath(), []byte(entry(exe)))
	write(mimePath(), []byte(mimeXML))
	write(IconPath(), icon)

	// These tools are optional; without them the files still work after
	// the desktop environment rescans.
	run := func(name string, args ...string) {
		if _, err := exec.LookPath(name); err != nil {
			return
		}
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out))))
		}
	}
	run("update-mime-database", filepath.Join(dirs.XDGData, "mime"))
	run("update-desktop-database", filepath.Join(dirs.XDGData, "applications"))
	run("gtk-update-icon-cache", "-q", "-t", filepath.Join(dirs.XDGData, "icons", "hicolor"))
	if _, err := exec.LookPath("xdg-mime"); err == nil {
		for _, t := range Handled {
			run("xdg-mime", "default", DesktopID, t)
		}
	} else {
		errs = append(errs, setDefaultsManually())
	}
	return nonNil(errs)
}

// setDefaultsManually edits ~/.config/mimeapps.list when xdg-mime is missing.
func setDefaultsManually() error {
	path := filepath.Join(dirs.XDGConfig, "mimeapps.list")
	b, _ := os.ReadFile(path)
	lines := strings.Split(string(b), "\n")
	section := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "[Default Applications]" {
			section = i
		}
	}
	if section < 0 {
		lines = append(lines, "[Default Applications]")
		section = len(lines) - 1
	}
	for _, t := range Handled {
		replaced := false
		for i := section + 1; i < len(lines) && !strings.HasPrefix(lines[i], "["); i++ {
			if strings.HasPrefix(lines[i], t+"=") {
				lines[i] = t + "=" + DesktopID
				replaced = true
			}
		}
		if !replaced {
			lines = append(lines[:section+1], append([]string{t + "=" + DesktopID}, lines[section+1:]...)...)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// Uninstall removes everything Install wrote.
func Uninstall() {
	for _, p := range []string{DesktopPath(), mimePath(), IconPath()} {
		os.Remove(p)
	}
	if _, err := exec.LookPath("update-mime-database"); err == nil {
		exec.Command("update-mime-database", filepath.Join(dirs.XDGData, "mime")).Run()
	}
	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		exec.Command("update-desktop-database", filepath.Join(dirs.XDGData, "applications")).Run()
	}
}

// Defaults reports which desktop entry handles each type, via xdg-mime.
func Defaults() map[string]string {
	out := map[string]string{}
	if _, err := exec.LookPath("xdg-mime"); err != nil {
		return out
	}
	for _, t := range Handled {
		b, _ := exec.Command("xdg-mime", "query", "default", t).Output()
		out[t] = strings.TrimSpace(string(b))
	}
	return out
}

func nonNil(errs []error) []error {
	var out []error
	for _, e := range errs {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}
