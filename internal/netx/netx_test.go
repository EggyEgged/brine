package netx

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("x"))
	h := hex.EncodeToString(sum[:])
	b := base64.StdEncoding.EncodeToString(sum[:])
	for _, in := range []string{h, "sha256:" + h, b, " " + h + "\n"} {
		if got := NormalizeSHA256(in); got != h {
			t.Errorf("NormalizeSHA256(%q) = %q", in, got)
		}
	}
	for _, bad := range []string{"", "abc", "sha256:", "zz" + h[2:], b[:10]} {
		if got := NormalizeSHA256(bad); got != "" {
			t.Errorf("NormalizeSHA256(%q) = %q, want empty", bad, got)
		}
	}
}

func serve(t *testing.T, body string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadVerifies(t *testing.T) {
	body := "hello brine"
	m := md5.Sum([]byte(body))
	s := sha256.Sum256([]byte(body))
	srv := serve(t, body)
	dest := filepath.Join(t.TempDir(), "f")

	if err := Download(srv.URL, dest, Verify{MD5: hex.EncodeToString(m[:]), SHA256: base64.StdEncoding.EncodeToString(s[:])}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != body {
		t.Fatalf("content %q", b)
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Fatal("leftover .part file")
	}
}

func TestDownloadRejectsBadChecksum(t *testing.T) {
	srv := serve(t, "tampered")
	dest := filepath.Join(t.TempDir(), "f")
	good := sha256.Sum256([]byte("original"))
	err := Download(srv.URL, dest, Verify{SHA256: hex.EncodeToString(good[:])}, nil)
	var cs *ChecksumError
	if !errors.As(err, &cs) {
		t.Fatalf("want ChecksumError, got %v", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("bad file was kept")
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Fatal("bad .part was kept")
	}
}

func TestDownloadReusesVerifiedFile(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.Write([]byte("data")) }))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "f")
	os.WriteFile(dest, []byte("data"), 0o644)
	s := sha256.Sum256([]byte("data"))
	if err := Download(srv.URL, dest, Verify{SHA256: hex.EncodeToString(s[:])}, nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatal("re-downloaded a file that already matched")
	}
}

func TestDownloadHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	old := time.Sleep
	_ = old
	if err := download(srv.URL, filepath.Join(t.TempDir(), "f"), Verify{}, nil); err == nil {
		t.Fatal("404 accepted")
	}
}

func TestGetJSONTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	start := time.Now()
	var v map[string]any
	if err := GetJSONTimeout(srv.URL, 150*time.Millisecond, &v); err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
}
