#!/bin/bash

echo "=== Testing Lock Behavior ==="
echo ""

# Clean up any existing state
rm -rf .claude-squad

echo "Test 1: Acquire lock successfully"
cat > /tmp/test_lock.go <<'EOF'
package main

import (
	"claude-squad/lock"
	"fmt"
	"os"
	"time"
)

func main() {
	repoPath := os.Args[1]

	fmt.Println("Acquiring lock...")
	l, err := lock.AcquireLock(repoPath)
	if err != nil {
		fmt.Printf("Failed to acquire lock: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Lock acquired successfully")

	if len(os.Args) > 2 && os.Args[2] == "--hold" {
		duration := 5
		fmt.Printf("Holding lock for %d seconds...\n", duration)
		time.Sleep(time.Duration(duration) * time.Second)
	}

	fmt.Println("Releasing lock...")
	if err := l.Release(); err != nil {
		fmt.Printf("Failed to release lock: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Lock released successfully")
}
EOF

cd /workspace/claude-squad
go run /tmp/test_lock.go "$(pwd)"

echo ""
echo "Test 2: Try to acquire lock twice (should fail)"
# Start first process in background that holds the lock
go run /tmp/test_lock.go "$(pwd)" --hold &
LOCK_PID=$!

# Give it time to acquire the lock
sleep 1

# Try to acquire lock again (should fail)
echo "Attempting to acquire lock while it's held..."
if go run /tmp/test_lock.go "$(pwd)" 2>&1 | grep -q "another cs instance is running"; then
    echo "✓ Lock correctly prevented concurrent access"
else
    echo "✗ Lock did not prevent concurrent access"
    kill $LOCK_PID 2>/dev/null
    exit 1
fi

# Wait for first process to finish
wait $LOCK_PID

echo ""
echo "Test 3: Lock can be re-acquired after release"
if go run /tmp/test_lock.go "$(pwd)" 2>&1 | grep -q "Lock acquired successfully"; then
    echo "✓ Lock can be re-acquired after release"
else
    echo "✗ Lock cannot be re-acquired"
    exit 1
fi

echo ""
echo "Test 4: Check lock file location"
if [ -f ".claude-squad/cs.lock" ]; then
    echo "✓ Lock file exists at .claude-squad/cs.lock"
    cat .claude-squad/cs.lock
else
    echo "✗ Lock file not found"
    exit 1
fi

echo ""
echo "=== All lock tests passed ==="
