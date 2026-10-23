package lock

import (
	"testing"
	"time"

	"brine/internal/dirs"
)

func TestExclusive(t *testing.T) {
	old := dirs.Data
	dirs.Data = t.TempDir()
	defer func() { dirs.Data = old }()
	// Two acquisitions in one process use separate file descriptors, which
	// flock treats as independent holders.
	a, err := Acquire(time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	noted := false
	start := time.Now()
	if _, err := Acquire(300*time.Millisecond, func(string) { noted = true }); err == nil {
		t.Fatal("second Acquire succeeded while the first was held")
	}
	if time.Since(start) < 250*time.Millisecond || !noted {
		t.Fatalf("did not wait/notify (waited %s, noted %v)", time.Since(start), noted)
	}
	a.Release()
	b, err := Acquire(time.Second, nil)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	b.Release()
	var nilLock *Lock
	nilLock.Release() // must not panic
}
