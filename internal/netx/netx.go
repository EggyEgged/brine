// Package netx holds small HTTP helpers: JSON fetches with timeouts, and
// resumable-by-retry, checksummed downloads with a terminal progress line.
package netx

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Timeouts. Metadata requests are small and must not hang `brine run`;
// downloads are large, so they get a stall watchdog instead of a deadline.
const (
	APITimeout   = 30 * time.Second
	StallTimeout = 60 * time.Second
)

// Client is shared by all requests. It has no overall timeout; callers use
// contexts.
var Client = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 30 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConnsPerHost:   8,
	ForceAttemptHTTP2:     true,
}}

const userAgent = "brine/1 (+linux launcher)"

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func GetJSON(url string, v any) error {
	b, err := GetBytes(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// GetJSONTimeout is GetJSON with a custom deadline for the whole request.
func GetJSONTimeout(url string, d time.Duration, v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	resp, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func GetBytes(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), APITimeout)
	defer cancel()
	resp, err := get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// Verify lists the checksums a download must match. Empty fields are not checked.
type Verify struct {
	MD5    string // hex
	SHA256 string // hex, or base64 as Microsoft's APIs return it
}

func (v Verify) empty() bool { return v.MD5 == "" && v.SHA256 == "" }

// NormalizeSHA256 accepts a SHA-256 as hex (optionally "sha256:" prefixed)
// or base64 and returns lowercase hex, or "" if it is neither.
func NormalizeSHA256(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "sha256:")
	if len(s) == 64 {
		if _, err := hex.DecodeString(s); err == nil {
			return strings.ToLower(s)
		}
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return hex.EncodeToString(b)
	}
	return ""
}

// Progress is called with bytes done and total (total may be -1).
type Progress func(done, total int64)

// Download fetches url to dest atomically, verifying v. A file already at
// dest that matches v is reused. Transient failures are retried.
func Download(url, dest string, v Verify, p Progress) error {
	if !v.empty() {
		if err := verifyFile(dest, v); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		lastErr = download(url, dest, v, p)
		if lastErr == nil {
			return nil
		}
		var cs *ChecksumError
		if errors.As(lastErr, &cs) && attempt >= 1 {
			break // retrying a bad file won't help
		}
	}
	return lastErr
}

// ChecksumError reports a download whose content did not match.
type ChecksumError struct {
	File, Algo, Got, Want string
}

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("%s: %s mismatch (got %s, want %s)", e.File, e.Algo, e.Got, e.Want)
}

func download(url, dest string, v Verify, p Progress) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Abort a transfer that makes no progress for StallTimeout.
	watchdog := time.AfterFunc(StallTimeout, cancel)
	defer watchdog.Stop()

	resp, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var hashes []hash.Hash
	w := io.Writer(f)
	var md, sh hash.Hash
	if v.MD5 != "" {
		md = md5.New()
		hashes = append(hashes, md)
	}
	if v.SHA256 != "" {
		sh = sha256.New()
		hashes = append(hashes, sh)
	}
	for _, h := range hashes {
		w = io.MultiWriter(w, h)
	}
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			watchdog.Reset(StallTimeout)
			if _, err := w.Write(buf[:n]); err != nil {
				f.Close()
				os.Remove(tmp)
				return err
			}
			done += int64(n)
			if p != nil {
				p(done, resp.ContentLength)
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			if ctx.Err() != nil {
				return fmt.Errorf("download stalled for %s: %w", StallTimeout, rerr)
			}
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if resp.ContentLength >= 0 && done != resp.ContentLength {
		os.Remove(tmp)
		return fmt.Errorf("%s: short download (%d of %d bytes)", filepath.Base(dest), done, resp.ContentLength)
	}
	if md != nil {
		if got := hex.EncodeToString(md.Sum(nil)); got != strings.ToLower(v.MD5) {
			os.Remove(tmp)
			return &ChecksumError{filepath.Base(dest), "MD5", got, v.MD5}
		}
	}
	if sh != nil {
		want := NormalizeSHA256(v.SHA256)
		if got := hex.EncodeToString(sh.Sum(nil)); got != want {
			os.Remove(tmp)
			return &ChecksumError{filepath.Base(dest), "SHA-256", got, want}
		}
	}
	return os.Rename(tmp, dest)
}

func verifyFile(path string, v Verify) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	md, sh := md5.New(), sha256.New()
	if _, err := io.Copy(io.MultiWriter(md, sh), f); err != nil {
		return err
	}
	if v.MD5 != "" && hex.EncodeToString(md.Sum(nil)) != strings.ToLower(v.MD5) {
		return errors.New("md5 differs")
	}
	if v.SHA256 != "" && hex.EncodeToString(sh.Sum(nil)) != NormalizeSHA256(v.SHA256) {
		return errors.New("sha256 differs")
	}
	return nil
}

// Bar returns a Progress that redraws a single stderr line, at most ~10x/s.
// Calls may come from several goroutines' callers; the caller serialises them.
func Bar(label string) Progress {
	var last time.Time
	return func(done, total int64) {
		if time.Since(last) < 100*time.Millisecond && done != total {
			return
		}
		last = time.Now()
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r  %s %5.1f%% (%s / %s)   ", label,
				float64(done)*100/float64(total), Size(done), Size(total))
		} else {
			fmt.Fprintf(os.Stderr, "\r  %s %s   ", label, Size(done))
		}
		if done == total {
			fmt.Fprintln(os.Stderr)
		}
	}
}

func Size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
