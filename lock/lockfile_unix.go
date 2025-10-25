//go:build !windows

package lock

import (
	"fmt"
	"os"
	"syscall"
)

// acquireLockPlatform attempts to acquire an exclusive lock using flock.
// This is atomic and kernel-enforced. The lock is automatically released
// when the file is closed or the process dies.
func acquireLockPlatform(file *os.File) error {
	// LOCK_EX = exclusive lock
	// LOCK_NB = non-blocking (fail immediately if locked)
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		return fmt.Errorf("lock is held by another process: %w", err)
	}
	return nil
}
