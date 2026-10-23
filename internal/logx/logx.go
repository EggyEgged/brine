// Package logx writes Brine's logs under ~/.local/state/brine and
// redacts credentials before anything reaches disk.
package logx

import (
	"bytes"
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

// MaxLogSize is the size above which a log is rotated (once, keeping one
// previous generation as name.1) when it is opened.
const MaxLogSize = 5 << 20

// File opens (appending) a log file named name in the state directory,
// rotating it first if it has grown past MaxLogSize.
func File(name string) (*os.File, error) {
	if err := os.MkdirAll(dirs.Logs(), 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dirs.Logs(), name)
	rotate(path, MaxLogSize)
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func rotate(path string, max int64) {
	if st, err := os.Stat(path); err == nil && st.Size() > max {
		os.Rename(path, path+".1")
	}
}

// maxPending bounds how much of an unterminated line RedactWriter holds.
const maxPending = 64 << 10

// RedactWriter redacts what is written to it before passing it on. It works
// on whole lines, buffering a partial line until its newline arrives, so a
// token split across two Writes is still caught. Call Flush when done to
// emit a final unterminated line.
type RedactWriter struct {
	mu  sync.Mutex
	W   io.Writer
	buf []byte
}

func (r *RedactWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		line := r.buf[:i+1]
		if _, err := io.WriteString(r.W, Redact(string(line))); err != nil {
			return 0, err
		}
		r.buf = r.buf[i+1:]
	}
	if len(r.buf) > maxPending {
		// A line this long is not text we understand; redact it in one piece.
		if _, err := io.WriteString(r.W, Redact(string(r.buf))+"\n"); err != nil {
			return 0, err
		}
		r.buf = nil
	}
	return len(p), nil
}

// Flush writes any buffered partial line.
func (r *RedactWriter) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) == 0 {
		return nil
	}
	_, err := io.WriteString(r.W, Redact(string(r.buf))+"\n")
	r.buf = nil
	return err
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
	return rw, func() { rw.Flush(); f.Close() }
}

func Printf(format string, args ...any) { Logger.Output(2, fmt.Sprintf(format, args...)) }
