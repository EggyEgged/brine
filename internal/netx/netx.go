// Package netx holds small HTTP helpers: JSON fetches and resumable,
// checksummed downloads with a terminal progress line.
package netx

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var Client = &http.Client{Timeout: 0}

const userAgent = "brine/1 (+linux launcher)"

func Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
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
	resp, err := Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

func GetBytes(url string) ([]byte, error) {
	resp, err := Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Progress is called with bytes done and total (total may be -1).
type Progress func(done, total int64)

// Download fetches url to dest atomically. If wantMD5 is non-empty the
// file is verified and an existing file with the right sum is reused.
func Download(url, dest, wantMD5 string, p Progress) error {
	if wantMD5 != "" {
		if sum, err := FileMD5(dest); err == nil && sum == wantMD5 {
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
		lastErr = download(url, dest, wantMD5, p)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func download(url, dest, wantMD5 string, p Progress) error {
	resp, err := Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var h hash.Hash = md5.New()
	w := io.MultiWriter(f, h)
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				f.Close()
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
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if wantMD5 != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != wantMD5 {
			os.Remove(tmp)
			return fmt.Errorf("%s: checksum mismatch (got %s, want %s)", filepath.Base(dest), got, wantMD5)
		}
	}
	return os.Rename(tmp, dest)
}

func FileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Bar returns a Progress that redraws a single stderr line, at most ~10x/s.
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
