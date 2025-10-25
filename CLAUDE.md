# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Claude Squad is a terminal multiplexer for AI coding assistants written in Go. It manages multiple instances of Claude Code, Aider, Codex, and Gemini in isolated git worktrees with separate tmux sessions, allowing parallel task execution.

## Development Commands

### Building and Testing

```bash
# Build the binary
go build -v -o build/claude-squad

# Run tests
go test -v ./...

# Run tests for a specific package
go test -v ./session/...
go test -v ./session/git/...
go test -v ./ui/...

# Build for specific platforms (as per CI)
GOOS=linux GOARCH=amd64 go build -v -o build/linux_amd64/claude-squad
GOOS=darwin GOARCH=arm64 go build -v -o build/darwin_arm64/claude-squad
```

### Running the Application

```bash
# Run directly
go run main.go

# Run with specific program
go run main.go -p "aider --model ollama_chat/gemma3:1b"

# Run with auto-yes mode (experimental)
go run main.go -y

# Show debug information
go run main.go debug

# Reset all stored instances
go run main.go reset
```

## Architecture

### Per-Repository Isolation Model

Claude Squad uses a **per-repository isolation model**:
- Each repository gets its own isolated state, instances, and worktrees
- Multiple repositories can run `cs` simultaneously without conflicts
- Only one `cs` instance per repository (enforced by file locking)

### Core Components

**Instance Management** (`session/instance.go`, `session/storage.go`):
- `Instance` is the central entity representing a running AI assistant session
- Each instance has: title, git worktree, tmux session, branch, status (Running/Ready/Loading/Paused)
- Instances can be paused (commits changes, removes worktree, keeps branch) and resumed
- Storage handles serialization/deserialization of instances between runs

**Repository Identification** (`config/repo.go`):
- `GetCanonicalRepoPath()`: Resolves symlinks to ensure same repo always gets same hash
- `GetRepoHash()`: SHA256 hash (8 hex chars) uniquely identifies each repository
- Used for: namespacing tmux sessions, isolating per-repo state

**Process Locking** (`lock/lockfile.go`, `lock/lockfile_*.go`):
- Atomic file locking using `flock()` (Unix) or `LockFileEx` (Windows)
- Lock file: `<repo>/.claude-squad/cs.lock`
- Prevents multiple `cs` instances in same repository
- Auto-released when process exits (kernel-enforced)

**Git Worktree Integration** (`session/git/`):
- Each instance gets an isolated git worktree in `<repo>/.claude-squad/worktrees/`
- Worktrees are stored locally within the repository (gitignored)
- Worktrees are created from the current repo with unique branches (prefix + sanitized session name)
- Operations: Setup, Cleanup, Remove, Prune, IsDirty, CommitChanges, PushChanges
- Diff tracking compares current state against base commit SHA

**Tmux Session Management** (`session/tmux/tmux.go`):
- Each instance runs in a dedicated tmux session: `claudesquad_<repo-hash>_<title>`
- Repo hash prevents session name collisions across different repositories
- Repo path stored in tmux environment variable (`CLAUDE_SQUAD_REPO`) for orphan detection
- PTY-based attachment enables resizing and input/output streaming
- StatusMonitor tracks content changes using SHA256 hashing to detect when AI is working vs. waiting
- Supports Claude, Aider, and Gemini with auto-detection of trust prompts
- Mouse scrolling and history are enabled (10000 line limit)

**UI Layer** (`app/app.go`, `ui/`):
- Built with Bubble Tea TUI framework
- Three-pane layout: List (30%) | Preview/Diff tabs (70%)
- States: stateDefault, stateNew, statePrompt, stateHelp, stateConfirm
- Key components: List, Menu, TabbedWindow (Preview + Diff), ErrBox, Overlays
- Preview pane shows live tmux output; Diff pane shows git changes

**State Storage** (`config/state.go`):
- **Per-repo state**: `<repo>/.claude-squad/state.json` (gitignored)
- **Global config**: `~/.claude-squad/config.json` (DefaultProgram, BranchPrefix, AutoYes)
- Proactive backups: `state.json.bak` created before each write
- Corruption recovery: Automatically restores from backup if state file corrupted
- Each repository's instances are isolated and independent

**Daemon Management** (`daemon/daemon.go`):
- Per-repository daemons for AutoYes mode
- Each repo gets its own daemon process: `<repo>/.claude-squad/daemon.pid`
- Daemon only monitors instances in its specific repository
- Multiple repos = multiple independent daemons

### Key Workflows

**Creating a New Instance**:
1. User presses `n` or `N` (with prompt)
2. Instance created in memory (not started)
3. User enters title
4. Start() creates git worktree and tmux session
5. Instance saved to storage
6. UI switches to default state, shows help screen

**Pausing/Resuming**:
- Pause (`c`): Commits changes, removes worktree, kills tmux, sets status to Paused
- Resume (`r`): Recreates worktree, restarts tmux session (or restores if still exists)
- Branch and commit history are preserved

**Attaching to Instance**:
- User presses Enter on selected instance
- Switches to raw terminal mode, streams tmux I/O
- Ctrl-Q to detach (not Ctrl-D, which kills the session)

**AutoYes Mode**:
- Daemon process monitors instances and auto-presses Enter on prompts
- Identified by detecting prompt strings (e.g., "No, and tell Claude what to do differently")

## Important Implementation Details

### Instance Lifecycle
- `Instance.Start(firstTimeSetup bool)` handles both new instances and loading from storage
- Always cleanup resources (worktree, tmux) in defer blocks with error accumulation
- `started` flag prevents operations on uninitialized instances

### Tmux PTY Management
- Each tmux session requires a PTY (`ptmx`) for sizing control
- On Attach: creates goroutines for I/O streaming and window size monitoring
- On Detach: must close PTY, restore new one, cancel goroutines, wait for cleanup
- DetachSafely vs. Detach: Safely version doesn't panic, used in Pause operation

### Git Worktree Naming and Organization
- Worktrees stored locally in repository: `<repo>/.claude-squad/worktrees/`
- Path: `<repo>/.claude-squad/worktrees/<sanitized_title>_<hex_timestamp>`
- Branch names: `<configurable_prefix><sanitized_title>`
- Always use absolute paths for reliability
- Worktrees are gitignored (`.claude-squad/` directory)

### Canonical Path Resolution
- All repo paths resolved via `filepath.EvalSymlinks()` before hashing
- Ensures symlinks to same repo get same hash and share state
- Prevents accidental state duplication from different access paths

### Testing Patterns
- Dependency injection for testability: `PtyFactory`, `cmd.Executor`
- Test constructors: `NewTmuxSessionWithDeps`, `Instance.SetTmuxSession`
- Mock git operations using test repos in temp directories

### Concurrency and Locking
- **Process-level locking**: Only one `cs` per repository (file lock)
- **Lock is atomic**: Uses kernel-enforced `flock()` / `LockFileEx`
- **Auto-release**: Lock released when process exits (even on crash)
- **Multiple repos**: Different repos can run `cs` concurrently without conflicts
- **UI updates**: Preview every 100ms, metadata every 500ms via Bubble Tea message loop

## Common Gotchas

1. **Per-Repo Isolation**: Each repository has independent state in `.claude-squad/`. Instances don't appear across repos.
2. **Lock Conflicts**: Only one `cs` instance per repo. If you see "another cs instance is running", check for:
   - Existing `cs` process in that repo
   - Stale lock (shouldn't happen - auto-released on crash, but check `.claude-squad/cs.lock`)
3. **Tmux Session Naming**: Sessions include repo hash: `claudesquad_<hash>_<title>`. Same title in different repos = different sessions.
4. **Sanitization**: Session names are sanitized (spaces removed, dots replaced with underscores) before use in tmux
5. **Exact Match**: Use `tmux has-session -t=name` (with `=`) for exact matching, not prefix matching
6. **PTY Cleanup**: Always close and restore PTY after operations; never leave `t.ptmx` as nil after Start/Restore
7. **Context Cancellation**: Attach goroutines must respect context for clean shutdown
8. **Storage Sync**: Call `storage.SaveInstances()` after state changes (new instance, delete, pause)
9. **Branch Checkout**: Cannot resume if branch is checked out elsewhere
10. **History Capture**: Use `-S - -E -` for full scrollback history in tmux
11. **Symlinks**: Symlinks to same repo get same hash - state is shared, not duplicated
12. **Migration**: Upgrading to per-repo isolation requires `cs reset` to clean old global state

## Prerequisites

- tmux
- gh (GitHub CLI)
- git (with worktree support)
- Go 1.23+

## Configuration Location

Use `cs debug` (or `go run main.go debug`) to find config paths.
