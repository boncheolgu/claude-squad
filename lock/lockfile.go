// ABOUTME: Cross-platform file locking for preventing concurrent cs instances
// ABOUTME: Uses flock on Unix and LockFileEx on Windows for atomic, kernel-enforced locks
package lock

import (
	"claude-squad/config"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Lock represents an exclusive lock on a repository
type Lock struct {
	file     *os.File
	filePath string
}

// AcquireLock attempts to acquire an exclusive lock for the given repository.
// Returns an error if another process holds the lock.
func AcquireLock(repoPath string) (*Lock, error) {
	// Get the state directory for this repo
	stateDir, err := config.GetStateDir(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get state directory: %w", err)
	}

	lockPath := filepath.Join(stateDir, "cs.lock")

	// Open or create the lock file
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try to acquire the lock (platform-specific)
	if err := acquireLockPlatform(file); err != nil {
		file.Close()

		// Try to read existing PID for better error message
		existingPID := readPIDFromLockFile(lockPath)
		if existingPID != "" {
			return nil, fmt.Errorf("another cs instance is running in this repo (PID %s)", existingPID)
		}
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Write our PID to the lock file
	pid := os.Getpid()
	if err := file.Truncate(0); err == nil {
		if _, err := file.Seek(0, 0); err == nil {
			fmt.Fprintf(file, "%d\n", pid)
			file.Sync()
		}
	}

	return &Lock{
		file:     file,
		filePath: lockPath,
	}, nil
}

// Release releases the lock and removes the lock file
func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}

	// Close the file (this releases the flock automatically on Unix)
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	// Remove the lock file
	if err := os.Remove(l.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}

	l.file = nil
	return nil
}

// readPIDFromLockFile attempts to read a PID from the lock file for error reporting
func readPIDFromLockFile(lockPath string) string {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return ""
	}

	pidStr := strings.TrimSpace(string(data))
	// Validate it's a number
	if _, err := strconv.Atoi(pidStr); err != nil {
		return ""
	}

	return pidStr
}
