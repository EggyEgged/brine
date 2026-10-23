// Package lock serialises Brine operations that modify the data directory
// (setup, install, update, rollback, uninstall) across processes.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"brine/internal/dirs"
)

// Lock is a held advisory lock.
type Lock struct{ f *os.File }

// Acquire takes the exclusive lock, waiting up to wait. While waiting it
// calls note once so the user knows why nothing is happening.
func Acquire(wait time.Duration, note func(string)) (*Lock, error) {
	if err := os.MkdirAll(dirs.Data, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dirs.Data, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	noted := false
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Lock{f}, nil
		}
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, err
		}
		if !noted && note != nil {
			note("another brine command is changing the installation; waiting for it to finish")
			noted = true
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("another brine command has been using the installation for over %s", wait)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Release drops the lock. It is safe to call on a nil Lock.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
	l.f = nil
}
