# Changelog

All notable changes to claude-squad will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased] - Per-Repository Isolation

### ⚠️ BREAKING CHANGES

This release introduces **per-repository isolation**, fundamentally changing how claude-squad manages state and instances.

**What Changed:**
- State moved from global (`~/.claude-squad/state.json`) to per-repo (`.claude-squad/state.json` in each repo)
- Each repository now has isolated instances that don't appear in other repos
- Multiple repositories can run `cs` simultaneously without conflicts
- Only one `cs` instance allowed per repository (enforced by file locking)

**Migration Required:**
Before upgrading, run `cs reset` in each repository where you have active instances. This will:
- Clear old global state
- Clean up tmux sessions
- Remove worktrees

After upgrading, your instances will be gone and you'll start fresh with per-repo isolation.

### Added

**Per-Repository Isolation:**
- Each git repository gets independent `.claude-squad/` directory with its own state
- State files automatically gitignored
- Canonical path resolution handles symlinks correctly (same repo = same state)

**Process Locking:**
- Atomic file locking prevents concurrent `cs` instances in same repository
- Uses `flock()` on Unix, `LockFileEx` on Windows
- Lock automatically released on process exit (even crashes)
- Clear error messages when lock is held

**Improved Reliability:**
- Proactive state backups: `.claude-squad/state.json.bak` created before each write
- Automatic corruption recovery: Restores from backup if state file is corrupted
- Corrupted files preserved as `state.json.corrupted.<timestamp>` for inspection

**Better Organization:**
- Worktrees organized by repository: `~/.claude-squad/worktrees/<repo-hash>/`
- Tmux sessions namespaced: `claudesquad_<repo-hash>_<title>`
- Prevents name collisions when multiple repos have instances with same titles

**Per-Repository Daemons:**
- AutoYes mode now uses per-repo daemons (`.claude-squad/daemon.pid`)
- Each repository's daemon only monitors its own instances
- Multiple repos with AutoYes = multiple independent daemons

### Changed

- **State location**: `~/.claude-squad/state.json` → `<repo>/.claude-squad/state.json`
- **Daemon PID**: `~/.claude-squad/daemon.pid` → `<repo>/.claude-squad/daemon.pid`
- **Worktree paths**: `~/.claude-squad/worktrees/` → `~/.claude-squad/worktrees/<repo-hash>/`
- **Tmux naming**: `claudesquad_<title>` → `claudesquad_<repo-hash>_<title>`
- **Reset command**: Now scopes to current repository only

### Technical Details

**Repository Hashing:**
- Each repo identified by SHA256 hash (8 hex chars) of its canonical path
- Symlinks resolved before hashing to ensure consistency
- Example: `/workspace/project` → hash `fe08346a`

**Lock File Location:**
- Lock: `.claude-squad/cs.lock`
- Contains PID of holding process
- Kernel-enforced, survives crashes

**Backward Compatibility:**
- **None** - this is a breaking change requiring migration
- Old global state in `~/.claude-squad/state.json` will be ignored
- Run `cs reset` before upgrading to clean up

## [1.0.13] - Previous Release

See git history for changes prior to per-repository isolation.
