# Migration Guides

This document contains migration guides for major claude-squad changes.

## Migration: Local Worktrees (Latest)

### What's Changing?

**Before:**
- Worktrees stored globally: `~/.claude-squad/worktrees/<repo-hash>/`
- Worktrees organized by repo hash in home directory

**After:**
- Worktrees stored locally: `<repo>/.claude-squad/worktrees/`
- Worktrees within repository (gitignored)
- Better for containers and backups

### Migration Steps

**Required:** Run `cs reset` in each repository before upgrading

```bash
# In each repository where you have active instances
cd /path/to/your/repo
cs reset  # Cleans up old worktrees and state

# Upgrade claude-squad
# (however you installed it)

# Start fresh with local worktrees
cs
```

### After Migration

Worktrees will now be created in `<repo>/.claude-squad/worktrees/` instead of `~/.claude-squad/worktrees/`.

Old worktrees in `~/.claude-squad/worktrees/` can be safely deleted after migration.

---

## Migration: Per-Repository Isolation (v1.0.13)

This guide helps you migrate from the old global state model to the per-repository isolation model.

## What's Changing?

**Before (Old):**
- One global state file: `~/.claude-squad/state.json`
- All instances from all repos visible in one list
- Could accidentally run multiple `cs` in same repo (race conditions)

**After (New):**
- Per-repo state: `<repo>/.claude-squad/state.json`
- Each repo has its own isolated instances
- Only one `cs` per repo (file locking prevents conflicts)

## Migration Steps

### Option 1: Clean Slate (Recommended)

If you're okay losing your current instances:

```bash
# In each repository where you use claude-squad
cd /path/to/your/repo
cs reset

# Upgrade claude-squad
# (however you installed it - brew, go install, etc.)

# Start fresh
cs
```

### Option 2: Manual Backup

If you want to preserve instance information:

```bash
# Before upgrading, in each repo:
cd /path/to/your/repo

# Note down your active instances
cs  # Take screenshots or notes

# Save the old global state
cp ~/.claude-squad/state.json ~/claude-squad-old-state-backup.json

# Clean up
cs reset

# Upgrade and recreate instances manually
```

### Option 3: No Migration (Fresh Start)

If you're a new user or don't have existing instances:

```bash
# Just upgrade and start using it
cs
```

## After Migration

### Verify It's Working

```bash
cd /path/to/your/repo

# You should see the new state directory
ls -la .claude-squad/
# Should show: cs.lock, state.json, .gitignore

# Check it's gitignored
git status
# .claude-squad/ should NOT appear in untracked files
```

### Key Differences

**State Location:**
- Old: `~/.claude-squad/state.json` (global)
- New: `<repo>/.claude-squad/state.json` (per-repo)

**Multiple Repos:**
- Old: All instances from all repos in one view
- New: Each repo shows only its instances (run `cs` in each repo separately)

**Concurrent Access:**
- Old: Multiple `cs` instances could corrupt state
- New: File lock prevents concurrent access per repo

**Tmux Sessions:**
- Old: `claudesquad_<title>`
- New: `claudesquad_<repo-hash>_<title>` (prevents collisions)

## Troubleshooting

### "Another cs instance is running"

```bash
# Check if cs is actually running
ps aux | grep claude-squad

# If not running but still locked, remove stale lock
rm .claude-squad/cs.lock
```

### Old Instances Still Visible

```bash
# Clean up old global state
rm -rf ~/.claude-squad/state.json

# Clean up old tmux sessions
tmux ls | grep claudesquad_ | cut -d: -f1 | xargs -I{} tmux kill-session -t {}
```

### Worktrees from Old Version

```bash
# Old worktrees are in: ~/.claude-squad/worktrees/
# New worktrees are in: ~/.claude-squad/worktrees/<repo-hash>/

# To clean up old worktrees:
cd ~/.claude-squad/worktrees/
# Remove directories that don't match <8-hex-chars> pattern
```

## Questions?

- Check `CLAUDE.md` for architecture details
- See `CHANGELOG.md` for complete list of changes
- Open an issue at: https://github.com/smtg-ai/claude-squad/issues
