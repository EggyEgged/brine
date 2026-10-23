// Package doctor checks a Brine installation end to end and explains how
// to fix each problem in plain language.
package doctor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"brine/internal/app"
	"brine/internal/desktop"
	"brine/internal/dirs"
	"brine/internal/dxvk"
	"brine/internal/gpu"
	"brine/internal/mcp"
	"brine/internal/studio"
	"brine/internal/webview"
)

type Status int

const (
	OK Status = iota
	Warn
	Fail
	Info
)

func (s Status) String() string {
	return [...]string{" ok ", "warn", "FAIL", "info"}[s]
}

type Result struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}

// MCPPort is the local port StudioMCP.exe hosts and Studio connects to.
const MCPPort = 13469

// Run executes every check, printing each result as it completes.
func Run(a *app.App, rtErr error, w io.Writer, skipRoundTrip bool) (fails int) {
	report := func(r Result) {
		fmt.Fprintf(w, "[%s] %-22s %s\n", r.Status, r.Name, r.Detail)
		if r.Fix != "" && (r.Status == Fail || r.Status == Warn) {
			for _, l := range strings.Split(r.Fix, "\n") {
				fmt.Fprintf(w, "       %-22s → %s\n", "", l)
			}
		}
		if r.Status == Fail {
			fails++
		}
	}

	if os.Geteuid() == 0 {
		report(Result{"user", Fail, "running as root", "Run Brine as your normal user; it never needs root."})
	}

	// Wine runtime.
	if a.RT == nil {
		report(Result{"wine runtime", Fail, fmt.Sprint(rtErr),
			"Install Wine 9 or newer from your distribution, or run `brine setup` and choose the managed Wine build."})
	} else {
		v, err := a.RT.Version()
		if err != nil {
			report(Result{"wine runtime", Fail, fmt.Sprintf("%s does not run: %v", a.RT.Wine, err),
				"Reinstall that Wine build, or set [wine] runtime = \"managed\" and run `brine runtime update`."})
		} else {
			st := OK
			fix := ""
			if major := wineMajor(v); major > 0 && major < 9 {
				st, fix = Warn, "Studio needs a recent Wine. Install Wine 9+ or set [wine] runtime = \"managed\"."
			}
			report(Result{"wine runtime", st, fmt.Sprintf("%s (%s)", v, a.RT), fix})
		}
		if a.RT.Wineserver == "" {
			report(Result{"wineserver", Warn, "not found next to the Wine binary", "Brine needs wineserver to stop Studio cleanly; point [wine] runtime at a complete Wine build."})
		}
	}

	// Graphics.
	if app.HasDisplay() {
		report(Result{"display", OK, firstNonEmpty(os.Getenv("WAYLAND_DISPLAY"), os.Getenv("DISPLAY")), ""})
	} else {
		report(Result{"display", Warn, "no DISPLAY or WAYLAND_DISPLAY in this shell",
			"Studio needs a graphical session. Run brine from a terminal in your desktop session, or from the app menu."})
	}
	cards := gpu.Detect()
	var names []string
	for _, c := range cards {
		names = append(names, fmt.Sprintf("%s:%s(%s)", c.Name, c.Vendor, c.Driver))
	}
	report(Result{"gpus", Info, strings.Join(names, " "), ""})
	if gpu.HasVulkan() {
		detail := "Vulkan ICDs installed"
		if out, err := exec.Command("vulkaninfo", "--summary").Output(); err == nil {
			for _, l := range strings.Split(string(out), "\n") {
				if strings.Contains(l, "deviceName") {
					detail += "; " + strings.TrimSpace(strings.SplitN(l, "=", 2)[1])
				}
			}
		}
		report(Result{"vulkan", OK, detail, ""})
	} else {
		st := Warn
		if a.Renderer == "dxvk" || a.Renderer == "vulkan" {
			st = Fail
		}
		report(Result{"vulkan", st, "no Vulkan driver found",
			"Install your GPU's Vulkan driver (mesa-vulkan-drivers / vulkan-radeon / vulkan-intel / nvidia-utils), or set [graphics] renderer = \"opengl\"."})
	}
	env := gpu.Env(cards, a.Cfg.Graphics.GPU)
	report(Result{"renderer", Info, fmt.Sprintf("%s (config: %s, gpu: %s, env: %v)", a.Renderer, a.Cfg.Graphics.Renderer, a.Cfg.Graphics.GPU, env), ""})

	// Prefix.
	pfxDir := dirs.Prefix()
	if _, err := os.Stat(filepath.Join(pfxDir, "system.reg")); err != nil {
		report(Result{"wine prefix", Fail, pfxDir + " is not set up", "Run `brine setup`."})
	} else if _, err := os.Readlink(filepath.Join(pfxDir, "dosdevices", "z:")); err != nil {
		report(Result{"wine prefix", Warn, "Z: drive is missing, so Studio can't reach your files", "Run `brine setup` again to repair the prefix."})
	} else {
		report(Result{"wine prefix", OK, pfxDir, ""})
	}

	// Studio build.
	dir, err := a.BuildDir()
	if err != nil {
		report(Result{"studio install", Fail, err.Error(), "Run `brine install`."})
	} else {
		v := a.State.Versions[a.State.Current].Version
		detail := fmt.Sprintf("%s (%s)", v, a.State.Current)
		st, fix := OK, ""
		if latest, err := studio.Latest(); err != nil {
			detail += "; could not check for updates: " + err.Error()
			st = Warn
		} else if latest.GUID != a.State.Current {
			st, fix = Warn, "Run `brine update`."
			detail += "; " + latest.Version + " is available"
		}
		if a.State.Previous != "" {
			detail += "; rollback: " + a.State.Previous
		}
		if a.State.Pinned {
			st, fix = Warn, "Automatic updates are paused after a rollback. Run `brine update` to resume them."
			detail += "; updates paused"
		}
		report(Result{"studio install", st, detail, fix})
		for _, exe := range []string{studio.StudioExe, studio.MCPExe} {
			if _, err := os.Stat(filepath.Join(dir, exe)); err != nil {
				report(Result{"studio files", Fail, exe + " missing", "Run `brine install --force`."})
			}
		}
	}

	if wv := a.Cfg.Studio.WebView2; wv != "" {
		have := webview.InstalledVersions(pfxDir)
		switch {
		case len(have) == 0:
			report(Result{"webview2", Warn, "not installed; Studio's sign-in window and web panels will be blank",
				"Run `brine setup` (or set [studio] webview2 = \"\" and sign in through your browser)."})
		case wv != "latest" && !webview.Installed(pfxDir, wv):
			report(Result{"webview2", Warn, fmt.Sprintf("installed %v, configured %s", have, wv), "Run `brine setup` to install the configured version."})
		default:
			report(Result{"webview2", OK, strings.Join(have, ", "), ""})
		}
	}
	if a.Renderer == "dxvk" {
		if v := dxvk.Installed(pfxDir); v == "" {
			report(Result{"dxvk", Warn, "not installed yet", "`brine run` installs it automatically; or run `brine setup`."})
		} else {
			report(Result{"dxvk", OK, v, ""})
		}
	}

	// Desktop integration.
	if _, err := os.Stat(desktop.DesktopPath()); err != nil {
		report(Result{"desktop entry", Warn, "not installed", "Run `brine setup` (or `brine desktop install`)."})
	} else {
		var wrong []string
		for t, d := range desktop.Defaults() {
			if d != desktop.DesktopID {
				wrong = append(wrong, fmt.Sprintf("%s→%q", t, d))
			}
		}
		if len(wrong) > 0 {
			report(Result{"desktop entry", Warn, "other handlers: " + strings.Join(wrong, ", "),
				"Run `brine desktop install` so browser sign-in and \"Edit in Studio\" links reach Brine."})
		} else if exe, err := desktop.ExecPath(); err != nil {
			report(Result{"desktop entry", Warn, "cannot read the handler command: " + err.Error(), "Run `brine desktop install`."})
		} else if st, err := os.Stat(exe); err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
			report(Result{"desktop entry", Fail, "links are sent to " + exe + ", which does not exist or is not executable",
				"Run `brine desktop install` from the brine you want to keep, so roblox-studio: links reach it."})
		} else {
			report(Result{"desktop entry", OK, "roblox-studio: links, sign-in callbacks and place files go to " + exe, ""})
		}
	}

	// Studio MCP setting.
	var running bool
	if a.Pfx != nil {
		lad := a.Pfx.LocalAppData()
		settings, err := studio.MCPSetting(lad)
		switch {
		case err != nil:
			report(Result{"mcp setting", Warn, err.Error(), ""})
		case len(settings) == 0:
			report(Result{"mcp setting", Warn, "nobody has signed in to Studio in this prefix yet",
				"Run `brine run`, sign in, then enable Assistant → … → Manage MCP Servers → \"Enable Studio as MCP server\"."})
		default:
			var off []string
			for uid, on := range settings {
				if !on {
					off = append(off, uid)
				}
			}
			if len(off) > 0 {
				report(Result{"mcp setting", Fail, "\"Enable Studio as MCP server\" is off for user(s) " + strings.Join(off, ", "),
					"In Studio: Assistant → … → Manage MCP Servers → turn on \"Enable Studio as MCP server\".\nOr close Studio and run `brine mcp enable`."})
			} else {
				report(Result{"mcp setting", OK, fmt.Sprintf("enabled for %d user(s)", len(settings)), ""})
			}
		}
		procs := a.StudioProcesses()
		running = len(procs) > 0
		if running {
			report(Result{"studio running", OK, fmt.Sprintf("pid %d", procs[0].PID), ""})
		} else {
			report(Result{"studio running", Info, "no", ""})
		}
	}

	// Port ownership.
	if owner := PortOwner(MCPPort); owner != nil {
		mine := a.Pfx != nil && owner.Prefix == a.Pfx.Dir
		detail := fmt.Sprintf("127.0.0.1:%d held by pid %d (%s)", MCPPort, owner.PID, owner.Name)
		if mine {
			report(Result{"mcp port", OK, detail, ""})
		} else {
			report(Result{"mcp port", Warn, detail + ", which is not Brine's prefix",
				"Another Studio MCP helper (e.g. a Windows VM or other launcher) owns the port. Stop it so Studio and Brine meet on the same helper."})
		}
	} else {
		report(Result{"mcp port", Info, fmt.Sprintf("%d free (no MCP client is connected right now)", MCPPort), ""})
	}

	// Round trip through `brine mcp` exactly as a client runs it.
	if !skipRoundTrip {
		fails += roundTrip(report, running)
	}

	// Client registrations.
	checkClients(report)
	return fails
}

func roundTrip(report func(Result), running bool) int {
	exe, _ := mcp.ServerCommand()
	s, init, err := Start(exe, 60*time.Second)
	if err != nil {
		report(Result{"mcp round trip", Fail, err.Error(), "Check ~/.local/state/brine/mcp.log for the helper's output."})
		return 0
	}
	var info struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
		Instructions string `json:"instructions"`
	}
	json.Unmarshal(init, &info)
	if info.ServerInfo.Name == "brine" {
		s.Close()
		report(Result{"mcp round trip", Fail, info.Instructions, "Fix the problems above, then run `brine doctor` again."})
		return 0
	}
	report(Result{"mcp initialize", OK, "helper answered as " + info.ServerInfo.Name, ""})

	// Studio reconnects every few seconds; give it time to find the helper.
	var studios []map[string]any
	var text string
	var isErr bool
	deadline := time.Now().Add(20 * time.Second)
	for {
		text, isErr, err = s.CallTool("list_roblox_studios", map[string]any{}, 30*time.Second)
		if err == nil && !isErr {
			var body struct {
				Studios []map[string]any `json:"studios"`
			}
			json.Unmarshal([]byte(text), &body)
			studios = body.Studios
		}
		if !running || len(studios) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	exitAfter, closeErr := s.Close()
	switch {
	case err != nil:
		report(Result{"list_roblox_studios", Fail, err.Error(), "See ~/.local/state/brine/mcp.log."})
	case !running:
		report(Result{"list_roblox_studios", Info, "Studio is not running: " + truncate(text, 80), "Start Studio with `brine run` and rerun `brine doctor` for a full check."})
	case isErr:
		report(Result{"list_roblox_studios", Fail, text, ""})
	case len(studios) == 0:
		report(Result{"list_roblox_studios", Fail, "Studio is running but has not connected to the helper",
			"Open a place (MCP starts once a place is open), and make sure \"Enable Studio as MCP server\" is on."})
	default:
		var names []string
		for _, st := range studios {
			names = append(names, fmt.Sprint(st["name"]))
		}
		report(Result{"list_roblox_studios", OK, strings.Join(names, "; "), ""})
	}
	if len(s.Stray) > 0 {
		report(Result{"stdout hygiene", Fail, fmt.Sprintf("%d non-JSON-RPC line(s) on stdout, e.g. %q", len(s.Stray), truncate(s.Stray[0], 80)),
			"Please report this; Brine should filter all helper noise."})
	} else {
		report(Result{"stdout hygiene", OK, "only JSON-RPC on stdout", ""})
	}
	if closeErr != nil {
		report(Result{"mcp shutdown", Fail, closeErr.Error(), ""})
	} else {
		report(Result{"mcp shutdown", OK, fmt.Sprintf("exited %s after stdin closed", exitAfter.Round(time.Millisecond)), ""})
	}
	return 0
}

func checkClients(report func(Result)) {
	exe, _ := mcp.ServerCommand()
	if claude, err := exec.LookPath("claude"); err == nil {
		out, err := exec.Command(claude, "mcp", "get", mcp.ServerName).CombinedOutput()
		switch {
		case err != nil:
			report(Result{"claude code", Warn, mcp.ServerName + " is not registered", "Run `brine mcp connect claude-code`."})
		case !strings.Contains(string(out), exe):
			report(Result{"claude code", Warn, mcp.ServerName + " points somewhere else", "Run `brine mcp connect claude-code` to update it."})
		default:
			report(Result{"claude code", OK, mcp.ServerName + " → " + exe + " mcp", ""})
		}
		if list, err := exec.Command(claude, "mcp", "list").Output(); err == nil {
			for _, l := range strings.Split(string(list), "\n") {
				if strings.Contains(l, "org.vinegarhq.Vinegar") || strings.Contains(l, "mcp.bat") {
					name := strings.TrimSpace(strings.SplitN(l, ":", 2)[0])
					report(Result{"claude code", Warn, fmt.Sprintf("stale Studio entry %q still configured", name),
						fmt.Sprintf("Remove it with `claude mcp remove %s` (check its scope with `claude mcp get %s`).", name, name)})
				}
			}
		}
	}
	path := mcp.DesktopConfigPath()
	if b, err := os.ReadFile(path); err == nil {
		var doc struct {
			MCPServers map[string]struct {
				Command string `json:"command"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			report(Result{"claude desktop", Fail, path + " is not valid JSON: " + err.Error(), "Fix the file by hand; Brine will not overwrite it."})
		} else if e, ok := doc.MCPServers[mcp.ServerName]; !ok {
			report(Result{"claude desktop", Info, "not configured", "Run `brine mcp connect claude-desktop` if you use Claude Desktop."})
		} else if e.Command != exe {
			report(Result{"claude desktop", Warn, "entry points to " + e.Command, "Run `brine mcp connect claude-desktop`."})
		} else {
			report(Result{"claude desktop", OK, "configured", ""})
		}
	}
}

// Owner is the process listening on a local TCP port.
type Owner struct {
	PID    int
	Name   string
	Prefix string
}

// PortOwner finds the process listening on 127.0.0.1:port via /proc.
func PortOwner(port int) *Owner {
	inodes := map[string]bool{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			_, p, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			if n, err := strconv.ParseInt(p, 16, 32); err == nil && int(n) == port {
				inodes[fields[9]] = true
			}
		}
		fh.Close()
	}
	if len(inodes) == 0 {
		return nil
	}
	procs, _ := os.ReadDir("/proc")
	for _, pe := range procs {
		pid, err := strconv.Atoi(pe.Name())
		if err != nil {
			continue
		}
		fds, err := os.ReadDir(filepath.Join("/proc", pe.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join("/proc", pe.Name(), "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			if inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] {
				o := &Owner{PID: pid}
				comm, _ := os.ReadFile(filepath.Join("/proc", pe.Name(), "comm"))
				o.Name = strings.TrimSpace(string(comm))
				env, _ := os.ReadFile(filepath.Join("/proc", pe.Name(), "environ"))
				for _, kv := range strings.Split(string(env), "\x00") {
					if v, ok := strings.CutPrefix(kv, "WINEPREFIX="); ok {
						o.Prefix = v
					}
				}
				return o
			}
		}
	}
	return &Owner{PID: -1, Name: "unknown (another user or a sandbox)"}
}

func wineMajor(v string) int {
	v = strings.TrimPrefix(v, "wine-")
	n, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
