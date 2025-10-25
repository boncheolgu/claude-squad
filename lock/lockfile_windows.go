//go:build windows

package lock

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// acquireLockPlatform attempts to acquire an exclusive lock using LockFileEx.
// This is the Windows equivalent of flock.
func acquireLockPlatform(file *os.File) error {
	// Get the file handle
	handle := windows.Handle(file.Fd())

	// Create an overlapped structure (required for LockFileEx)
	var overlapped syscall.Overlapped

	// Flags:
	// LOCKFILE_EXCLUSIVE_LOCK = exclusive lock (not shared)
	// LOCKFILE_FAIL_IMMEDIATELY = non-blocking (fail immediately if locked)
	const (
		LOCKFILE_EXCLUSIVE_LOCK   = 0x00000002
		LOCKFILE_FAIL_IMMEDIATELY = 0x00000001
	)
	flags := uint32(LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY)

	// Lock the entire file (0 offset, max length)
	err := windows.LockFileEx(
		handle,
		flags,
		0,               // reserved, must be 0
		1,               // nNumberOfBytesToLockLow
		0,               // nNumberOfBytesToLockHigh
		&overlapped,
	)

	if err != nil {
		return fmt.Errorf("lock is held by another process: %w", err)
	}

	return nil
}

// Note: The lock is automatically released when the file handle is closed
// or when the process terminates, similar to Unix flock behavior.
