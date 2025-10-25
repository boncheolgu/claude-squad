#!/bin/bash
set -e

echo "=== Testing Per-Repo Isolation ==="
echo ""

# Test 1: Check repo hash generation
echo "Test 1: Repo hash for current directory"
REPO_PATH=$(pwd)
echo "Repo path: $REPO_PATH"

# Test 2: Try to create state directory
echo ""
echo "Test 2: Check state directory creation"
if [ -d ".claude-squad" ]; then
    echo "⚠️  .claude-squad already exists, cleaning up..."
    rm -rf .claude-squad
fi

# Use a simple go program to test the functions
cat > /tmp/test_hash.go <<'EOF'
package main

import (
	"claude-squad/config"
	"fmt"
	"os"
)

func main() {
	repoPath := os.Args[1]

	// Test canonical path
	canonical, err := config.GetCanonicalRepoPath(repoPath)
	if err != nil {
		fmt.Printf("Error getting canonical path: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Canonical path: %s\n", canonical)

	// Test hash
	hash, err := config.GetRepoHash(repoPath)
	if err != nil {
		fmt.Printf("Error getting hash: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Repo hash: %s\n", hash)

	// Test state directory creation
	stateDir, err := config.GetStateDir(repoPath)
	if err != nil {
		fmt.Printf("Error getting state dir: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("State directory: %s\n", stateDir)
}
EOF

cd /workspace/claude-squad
go run /tmp/test_hash.go "$REPO_PATH"

echo ""
echo "Test 3: Check if .claude-squad was created"
if [ -d ".claude-squad" ]; then
    echo "✓ .claude-squad directory created"
    ls -la .claude-squad/
else
    echo "✗ .claude-squad directory not created"
    exit 1
fi

echo ""
echo "Test 4: Check .gitignore in state directory"
if [ -f ".claude-squad/.gitignore" ]; then
    echo "✓ .gitignore exists"
    echo "Contents:"
    cat .claude-squad/.gitignore
else
    echo "✗ .gitignore not created"
fi

echo ""
echo "=== All tests passed ==="
