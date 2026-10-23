// Package logx writes Brine's logs under ~/.local/state/brine and
// redacts credentials before anything reaches disk.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"brine/internal/dirs"
)

var secrets = []*regexp.Regexp{
	// .ROBLOSECURITY cookie values.
	regexp.MustCompile(`_\|WARNING:-DO-NOT-SHARE-THIS[^\s"';]*`),
	// Auth URIs carry one-time codes and tickets.
	regexp.MustCompile(`(?i)(roblox-studio-auth:)[^\s"']*`),
	regexp.MustCompile(`(?i)((?:code|ticket|token|state|access_token|refresh_token|authorization)=)[^&\s"']+`),
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)("(?:token|authorization|password|cookie)"\s*:\s*")[^"]*`),
}

// Redact removes tokens, cookies and auth codes from s.
func Redact(s string) string {
	for i, re := range secrets {
		switch i {
		case 0:
			s = re.ReplaceAllString(s, "[REDACTED]")
		default:
			s = re.ReplaceAllString(s, "${1}[REDACTED]")
		}
	}
	return s
}

// File opens (appending) a log file named name in the state directory.
func File(name string) (*os.File, error) {
	if err := os.MkdirAll(dirs.Logs(), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dirs.Logs(), name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// RedactWriter redacts each Write before passing it on. Writes are assumed
// to be whole lines or at least not to split a secret, which holds for the
// line-oriented logs Brine produces.
type RedactWriter struct {
	mu sync.Mutex
	W  io.Writer
}

func (r *RedactWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := io.WriteString(r.W, Redact(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Logger is Brine's process-wide logger. It writes to stderr until Setup is called.
var Logger = log.New(&RedactWriter{W: os.Stderr}, "brine: ", 0)

// Setup sends the logger to both stderr (if verbose) and the named log file.
func Setup(name string, verbose bool) (io.Writer, func()) {
	f, err := File(name)
	if err != nil {
		Logger.Printf("cannot open log file: %v", err)
		return io.Discard, func() {}
	}
	var w io.Writer = f
	if verbose {
		w = io.MultiWriter(f, os.Stderr)
	}
	rw := &RedactWriter{W: w}
	Logger = log.New(rw, "", log.LstdFlags)
	return rw, func() { f.Close() }
}

func Printf(format string, args ...any) { Logger.Output(2, fmt.Sprintf(format, args...)) }
