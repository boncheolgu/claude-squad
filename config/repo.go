package config

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// GetCanonicalRepoPath resolves symlinks and returns the absolute canonical path
// to a repository. This ensures that the same repository accessed through different
// paths (e.g., symlinks) always gets the same hash.
func GetCanonicalRepoPath(path string) (string, error) {
	// Resolve any symlinks in the path
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	// Get the absolute path
	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, nil
}

// GetRepoHash returns a short hash (first 8 hex chars) of the canonical repository path.
// This hash is used to:
// - Organize worktrees by repository
// - Namespace tmux sessions to prevent collisions
// - Identify per-repo state and lock files
func GetRepoHash(repoPath string) (string, error) {
	// Get canonical path to handle symlinks
	canonical, err := GetCanonicalRepoPath(repoPath)
	if err != nil {
		return "", err
	}

	// Hash the canonical path
	hash := sha256.Sum256([]byte(canonical))

	// Return first 8 hex characters (32 bits of entropy)
	// Collision probability is negligible for reasonable number of repos
	return fmt.Sprintf("%x", hash[:4]), nil
}
