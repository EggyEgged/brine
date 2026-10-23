package wine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Prefix is a Wine prefix bound to one runtime. Every process Brine starts
// in the prefix (Studio, the MCP helper, wineboot) goes through Command so
// they all share the same wine binary, WINESERVER and environment, and
// therefore attach to the same wineserver.
type Prefix struct {
	Dir       string
	RT        *Runtime
	Debug     string            // WINEDEBUG
	Env       map[string]string // extra environment (graphics, user [env])
	Overrides map[string]string // WINEDLLOVERRIDES entries, dll -> mode
}

func NewPrefix(dir string, rt *Runtime) *Prefix {
	return &Prefix{
		Dir:   dir,
		RT:    rt,
		Debug: "-all",
		Env:   map[string]string{},
		Overrides: map[string]string{
			// Keep Wine from writing its own .desktop files and MIME
			// associations for everything installed in the prefix.
			"winemenubuilder.exe": "d",
		},
	}
}

func (p *Prefix) Exists() bool {
	_, err := os.Stat(filepath.Join(p.Dir, "system.reg"))
	return err == nil
}

// Environ builds the full environment for a Wine process.
func (p *Prefix) Environ() []string {
	drop := map[string]bool{
		"WINEPREFIX": true, "WINEDEBUG": true, "WINESERVER": true, "WINELOADER": true,
		"WINEDLLOVERRIDES": true, "WINEARCH": true, "PROTONPATH": true, "GAMEID": true,
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if !drop[k] {
			env[k] = v
		}
	}
	env["WINEPREFIX"] = p.Dir
	env["WINEDEBUG"] = p.Debug
	env["WINEARCH"] = "win64"
	if p.RT.Kind == UMU {
		env["PROTONPATH"] = p.RT.ProtonPath
		env["GAMEID"] = "umu-0"
		env["UMU_LOG"] = "0"
	} else {
		env["WINESERVER"] = p.RT.Wineserver
		env["WINELOADER"] = p.RT.Wine
	}
	env["WINEDLLOVERRIDES"] = p.overrides()
	for k, v := range p.Env {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func (p *Prefix) overrides() string {
	keys := make([]string, 0, len(p.Overrides))
	for k := range p.Overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+p.Overrides[k])
	}
	return strings.Join(parts, ";")
}

// Command returns a command that runs a Windows program (or a Wine
// builtin such as "wineboot") inside the prefix.
func (p *Prefix) Command(prog string, args ...string) *exec.Cmd {
	cmd := exec.Command(p.RT.Wine, append([]string{prog}, args...)...)
	cmd.Env = p.Environ()
	cmd.Dir = p.Dir
	return cmd
}

// Server runs wineserver with the given flag (-k, -w).
func (p *Prefix) Server(args ...string) *exec.Cmd {
	ws := p.RT.Wineserver
	if ws == "" {
		ws = "wineserver"
	}
	cmd := exec.Command(ws, args...)
	cmd.Env = p.Environ()
	return cmd
}

// Init creates (or updates) the prefix with wineboot.
func (p *Prefix) Init() error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	saved := p.Overrides
	p.Overrides = map[string]string{}
	for k, v := range saved {
		p.Overrides[k] = v
	}
	// Skip the Mono and Gecko install prompts; Studio needs neither.
	p.Overrides["mscoree"] = ""
	p.Overrides["mshtml"] = ""
	defer func() { p.Overrides = saved }()

	cmd := p.Command("wineboot", "-i")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wineboot: %w\n%s", err, tail(stderr.String(), 20))
	}
	return p.Wait(2 * time.Minute)
}

// Wait waits for the wineserver to exit (all processes done), up to timeout.
func (p *Prefix) Wait(timeout time.Duration) error {
	cmd := p.Server("-w")
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		cmd.Process.Kill()
		return fmt.Errorf("wineserver still busy after %s", timeout)
	}
}

// Kill stops every process in the prefix and its wineserver. It asks
// politely with `wineserver -k`; anything still alive after a grace period
// is killed outright.
func (p *Prefix) Kill() error {
	if !p.ServerRunning() && len(p.Processes("")) == 0 {
		return nil
	}
	if p.ServerRunning() {
		_ = p.Server("-k").Run()
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && (p.ServerRunning() || len(p.Processes("")) > 0) {
		time.Sleep(200 * time.Millisecond)
	}
	if left := p.Processes(""); len(left) > 0 {
		for _, pr := range left {
			syscall.Kill(pr.PID, syscall.SIGKILL)
		}
		time.Sleep(500 * time.Millisecond)
	}
	if p.ServerRunning() {
		return fmt.Errorf("wineserver for %s did not exit", p.Dir)
	}
	return nil
}

// ServerSocketDir is where the wineserver for this prefix listens. Wine
// keys it on the prefix directory's device and inode, so any process with
// the same WINEPREFIX finds the same server.
func (p *Prefix) ServerSocketDir() (string, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(p.Dir, &st); err != nil {
		return "", err
	}
	tmp := "/tmp"
	return filepath.Join(tmp, fmt.Sprintf(".wine-%d", os.Getuid()),
		fmt.Sprintf("server-%x-%x", st.Dev, st.Ino)), nil
}

// ServerRunning reports whether a wineserver is serving this prefix. It
// checks the socket Wine creates and, because that location differs between
// Wine builds, also looks for a wineserver process with this WINEPREFIX.
func (p *Prefix) ServerRunning() bool {
	if len(p.Processes("wineserver")) > 0 || len(p.Processes("wineserver64")) > 0 {
		return true
	}
	dir, err := p.ServerSocketDir()
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "socket")); err != nil {
		return false
	}
	return serverLocked(filepath.Join(dir, "lock"))
}

func serverLocked(lock string) bool {
	f, err := os.OpenFile(lock, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	fl := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &fl); err != nil {
		return false
	}
	return fl.Type != syscall.F_UNLCK
}

// Process is a running process that belongs to this prefix.
type Process struct {
	PID  int
	Args []string
}

func (p *Process) String() string { return fmt.Sprintf("%d %s", p.PID, strings.Join(p.Args, " ")) }

// Processes lists processes whose WINEPREFIX is this prefix and whose
// program is exe (case-insensitive, e.g. "RobloxStudioBeta.exe"; "" matches
// all). Only the program itself counts: WebView2 children that mention
// Studio in their arguments are not Studio.
func (p *Prefix) Processes(exe string) []Process {
	want := "WINEPREFIX=" + p.Dir
	var out []Process
	ents, _ := os.ReadDir("/proc")
	uid := os.Getuid()
	for _, e := range ents {
		var pid int
		if _, err := fmt.Sscanf(e.Name(), "%d", &pid); err != nil {
			continue
		}
		var st syscall.Stat_t
		if syscall.Stat("/proc/"+e.Name(), &st) != nil || int(st.Uid) != uid {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(cmdline) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if exe != "" && !IsProgram(args, exe) {
			continue
		}
		environ, err := os.ReadFile("/proc/" + e.Name() + "/environ")
		if err != nil {
			continue
		}
		for _, kv := range strings.Split(string(environ), "\x00") {
			if kv == want {
				out = append(out, Process{PID: pid, Args: args})
				break
			}
		}
	}
	return out
}

// IsProgram reports whether a command line runs the Windows program exe,
// either directly (a running Wine process shows the .exe as argv[0]) or
// through the wine loader (argv[0] is wine, argv[1] the .exe).
func IsProgram(args []string, exe string) bool {
	base := func(s string) string {
		s = s[strings.LastIndexAny(s, `/\`)+1:]
		return strings.ToLower(s)
	}
	exe = strings.ToLower(exe)
	if len(args) == 0 {
		return false
	}
	if base(args[0]) == exe {
		return true
	}
	switch base(args[0]) {
	case "wine", "wine64", "wine-preloader", "wine64-preloader":
		return len(args) > 1 && base(args[1]) == exe
	}
	return false
}

// WinPath converts an absolute host path into the Z: drive path Wine maps it to.
func WinPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return "Z:" + strings.ReplaceAll(abs, "/", `\`)
}

// UserDir is the prefix's C:\users\<name> directory. Wine names it after
// the account that created the prefix, which is not always $USER (sudo,
// su, containers), so look at what is on disk first.
func (p *Prefix) UserDir() string {
	users := filepath.Join(p.Dir, "drive_c", "users")
	ents, _ := os.ReadDir(users)
	var found []string
	for _, e := range ents {
		if e.IsDir() && !strings.EqualFold(e.Name(), "Public") {
			found = append(found, e.Name())
		}
	}
	if len(found) == 1 {
		return filepath.Join(users, found[0])
	}
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil && u.Username != "" {
		for _, f := range found {
			if f == u.Username {
				name = f
			}
		}
		if name == "" {
			name = u.Username
		}
	}
	return filepath.Join(users, name)
}

// LocalAppData is %LOCALAPPDATA% inside the prefix.
func (p *Prefix) LocalAppData() string {
	return filepath.Join(p.UserDir(), "AppData", "Local")
}

// ImportReg imports a .reg document (REGEDIT4 or Windows Registry Editor 5 format).
func (p *Prefix) ImportReg(content string) error {
	f, err := os.CreateTemp("", "brine-*.reg")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	f.Close()
	cmd := p.Command("regedit", "/S", WinPath(f.Name()))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("regedit: %w\n%s", err, tail(stderr.String(), 10))
	}
	return nil
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
