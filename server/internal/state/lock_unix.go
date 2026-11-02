//go:build unix

package state

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// lockDir takes an exclusive, non-blocking lock on dir/lumen.lock. The OS
// drops it when the process exits, so a crash never leaves a stale lock.
func lockDir(dir string) (func(), error) {
	f, err := os.OpenFile(lockPath(dir), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w: %s", ErrLocked, dir)
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
