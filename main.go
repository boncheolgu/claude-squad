package main

import (
	"claude-squad/app"
	cmd2 "claude-squad/cmd"
	"claude-squad/config"
	"claude-squad/daemon"
	"claude-squad/lock"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var (
	version         = "1.0.13"
	programFlag     string
	autoYesFlag     bool
	daemonFlag      bool
	repoPathFlag    string
	cleanupKillAll  bool
	rootCmd      = &cobra.Command{
		Use:   "claude-squad",
		Short: "Claude Squad - Manage multiple AI agents like Claude Code, Aider, Codex, and Amp.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			log.Initialize(daemonFlag)
			defer log.Close()

			if daemonFlag {
				// Daemon mode: use provided repo path
				if repoPathFlag == "" {
					return fmt.Errorf("--repo-path is required in daemon mode")
				}
				cfg := config.LoadConfig()
				err := daemon.RunDaemon(cfg, repoPathFlag)
				log.ErrorLog.Printf("failed to start daemon %v", err)
				return err
			}

			// Check if we're in a git repository
			currentDir, err := filepath.Abs(".")
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			if !git.IsGitRepo(currentDir) {
				return fmt.Errorf("error: claude-squad must be run from within a git repository")
			}

			// Get canonical repo path (resolves symlinks)
			repoPath, err := config.GetCanonicalRepoPath(currentDir)
			if err != nil {
				return fmt.Errorf("failed to get canonical repo path: %w", err)
			}

			// Acquire exclusive lock for this repository
			lock, err := lock.AcquireLock(repoPath)
			if err != nil {
				return err
			}
			defer func() {
				if err := lock.Release(); err != nil {
					log.ErrorLog.Printf("failed to release lock: %v", err)
				}
			}()

			cfg := config.LoadConfig()

			// Program flag overrides config
			program := cfg.DefaultProgram
			if programFlag != "" {
				program = programFlag
			}
			// AutoYes flag overrides config
			autoYes := cfg.AutoYes
			if autoYesFlag {
				autoYes = true
			}
			if autoYes {
				defer func() {
					if err := daemon.LaunchDaemon(repoPath); err != nil {
						log.ErrorLog.Printf("failed to launch daemon: %v", err)
					}
				}()
			}
			// Kill any daemon that's running for this repo
			if err := daemon.StopDaemon(repoPath); err != nil {
				log.ErrorLog.Printf("failed to stop daemon: %v", err)
			}

			return app.Run(ctx, program, autoYes, repoPath)
		},
	}

	resetCmd = &cobra.Command{
		Use:   "reset",
		Short: "Reset all stored instances for the current repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			// Get current directory and repo path
			currentDir, err := filepath.Abs(".")
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			if !git.IsGitRepo(currentDir) {
				return fmt.Errorf("error: must be run from within a git repository")
			}

			repoPath, err := config.GetCanonicalRepoPath(currentDir)
			if err != nil {
				return fmt.Errorf("failed to get canonical repo path: %w", err)
			}

			// Load and reset state for this repo
			state := config.LoadState(repoPath)
			storage, err := session.NewStorage(state)
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}
			if err := storage.DeleteAllInstances(); err != nil {
				return fmt.Errorf("failed to reset storage: %w", err)
			}
			fmt.Println("Storage has been reset successfully")

			// Get repo hash for cleanup
			repoHash, err := config.GetRepoHash(repoPath)
			if err != nil {
				return fmt.Errorf("failed to get repo hash: %w", err)
			}

			// Cleanup tmux sessions for this repo only
			if err := tmux.CleanupSessionsByPrefix(cmd2.MakeExecutor(), tmux.TmuxPrefix+repoHash); err != nil {
				return fmt.Errorf("failed to cleanup tmux sessions: %w", err)
			}
			fmt.Println("Tmux sessions have been cleaned up")

			// Cleanup worktrees for this repo
			if err := git.CleanupWorktrees(repoPath); err != nil {
				return fmt.Errorf("failed to cleanup worktrees: %w", err)
			}
			fmt.Println("Worktrees have been cleaned up")

			// Kill daemon for this repo
			if err := daemon.StopDaemon(repoPath); err != nil {
				return err
			}
			fmt.Println("daemon has been stopped")

			return nil
		},
	}

	debugCmd = &cobra.Command{
		Use:   "debug",
		Short: "Print debug information like config paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			cfg := config.LoadConfig()

			configDir, err := config.GetConfigDir()
			if err != nil {
				return fmt.Errorf("failed to get config directory: %w", err)
			}
			configJson, _ := json.MarshalIndent(cfg, "", "  ")

			fmt.Printf("Config: %s\n%s\n", filepath.Join(configDir, config.ConfigFileName), configJson)

			return nil
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number of claude-squad",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("claude-squad version %s\n", version)
			fmt.Printf("https://github.com/smtg-ai/claude-squad/releases/tag/v%s\n", version)
		},
	}

	cleanupCmd = &cobra.Command{
		Use:   "cleanup",
		Short: "List or clean up claude-squad tmux sessions",
		Long: `List all claude-squad tmux sessions, or clean up orphaned sessions.

Usage:
  cs cleanup              List all sessions (default)
  cs cleanup --kill-all   Kill all claude-squad sessions without prompting

Orphaned sessions occur when:
- A repository is deleted but tmux sessions remain
- .claude-squad/ directory is removed manually
- Sessions are left after repository moves`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			if cleanupKillAll {
				return killAllClaudeSquadSessions()
			}

			// Default: list sessions and check for orphans
			return cleanupOrphanedSessions()
		},
	}
)

func init() {
	rootCmd.Flags().StringVarP(&programFlag, "program", "p", "",
		"Program to run in new instances (e.g. 'aider --model ollama_chat/gemma3:1b')")
	rootCmd.Flags().BoolVarP(&autoYesFlag, "autoyes", "y", false,
		"[experimental] If enabled, all instances will automatically accept prompts")
	rootCmd.Flags().BoolVar(&daemonFlag, "daemon", false, "Run a program that loads all sessions"+
		" and runs autoyes mode on them.")
	rootCmd.Flags().StringVar(&repoPathFlag, "repo-path", "", "Repository path for daemon mode")

	// Hide the daemon flags as they're only for internal use
	err := rootCmd.Flags().MarkHidden("daemon")
	if err != nil {
		panic(err)
	}
	err = rootCmd.Flags().MarkHidden("repo-path")
	if err != nil {
		panic(err)
	}

	// Cleanup command flags
	cleanupCmd.Flags().BoolVar(&cleanupKillAll, "kill-all", false, "Kill all claude-squad sessions without prompting")

	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(cleanupCmd)
}

// findClaudeSquadSessions returns a list of all claude-squad tmux sessions
func findClaudeSquadSessions() ([]string, error) {
	cmd := exec.Command("tmux", "ls")
	output, err := cmd2.MakeExecutor().Output(cmd)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list tmux sessions: %w", err)
	}

	re := regexp.MustCompile(`claudesquad_[a-f0-9]{8}_[^:]+`)
	matches := re.FindAllString(string(output), -1)
	return matches, nil
}

// groupSessionsByHash groups session names by their repo hash
func groupSessionsByHash(sessions []string) map[string][]string {
	grouped := make(map[string][]string)
	re := regexp.MustCompile(`claudesquad_([a-f0-9]{8})_`)

	for _, sess := range sessions {
		if match := re.FindStringSubmatch(sess); len(match) >= 2 {
			hash := match[1]
			grouped[hash] = append(grouped[hash], sess)
		}
	}

	return grouped
}

// getSessionRepoPath queries tmux for the repo path stored in the session environment
func getSessionRepoPath(sessionName string) (string, error) {
	cmd := exec.Command("tmux", "show-environment", "-t", sessionName, "CLAUDE_SQUAD_REPO")
	output, err := cmd2.MakeExecutor().Output(cmd)
	if err != nil {
		return "", err
	}

	// Parse "CLAUDE_SQUAD_REPO=<path>" format
	parts := strings.SplitN(string(output), "=", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected environment variable format")
	}

	return strings.TrimSpace(parts[1]), nil
}

// cleanupOrphanedSessions lists sessions and identifies orphaned ones using tmux env vars
func cleanupOrphanedSessions() error {
	sessions, err := findClaudeSquadSessions()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("No claude-squad tmux sessions found")
		return nil
	}

	// Categorize sessions
	type SessionInfo struct {
		name     string
		repoPath string
		status   string // "active", "orphaned", or "unknown"
	}

	var infos []SessionInfo
	for _, sess := range sessions {
		repoPath, err := getSessionRepoPath(sess)
		if err != nil {
			// Can't get repo path - old session or error
			infos = append(infos, SessionInfo{name: sess, repoPath: "(unknown)", status: "unknown"})
			continue
		}

		// Check if repo path still exists
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			infos = append(infos, SessionInfo{name: sess, repoPath: repoPath, status: "orphaned"})
		} else {
			infos = append(infos, SessionInfo{name: sess, repoPath: repoPath, status: "active"})
		}
	}

	// Group by status
	var active, orphaned, unknown []SessionInfo
	for _, info := range infos {
		switch info.status {
		case "active":
			active = append(active, info)
		case "orphaned":
			orphaned = append(orphaned, info)
		case "unknown":
			unknown = append(unknown, info)
		}
	}

	// Display results
	fmt.Printf("Found %d claude-squad session(s):\n\n", len(sessions))

	if len(active) > 0 {
		fmt.Printf("Active sessions (%d):\n", len(active))
		for _, info := range active {
			fmt.Printf("  - %s\n    repo: %s\n", info.name, info.repoPath)
		}
		fmt.Println()
	}

	if len(unknown) > 0 {
		fmt.Printf("Unknown sessions (%d) - created before repo tracking:\n", len(unknown))
		for _, info := range unknown {
			fmt.Printf("  - %s\n", info.name)
		}
		fmt.Println()
	}

	if len(orphaned) == 0 {
		fmt.Println("No orphaned sessions found - all clean!")
		fmt.Println("\nCommands:")
		fmt.Println("  cs cleanup --kill-all       Kill all sessions")
		fmt.Println("  tmux kill-session -t <name>   Kill specific session")
		return nil
	}

	// Found orphaned sessions - ask user
	fmt.Printf("Orphaned sessions (%d) - repository no longer exists:\n", len(orphaned))
	for _, info := range orphaned {
		fmt.Printf("  - %s\n    repo: %s (not found)\n", info.name, info.repoPath)
	}
	fmt.Println()

	fmt.Print("Kill orphaned sessions? [y/N]: ")
	var response string
	fmt.Scanln(&response)

	if response != "y" && response != "Y" {
		fmt.Println("Cleanup cancelled")
		return nil
	}

	// Kill orphaned sessions
	fmt.Println("\nKilling orphaned sessions...")
	for _, info := range orphaned {
		fmt.Printf("  Killing: %s\n", info.name)
		killCmd := exec.Command("tmux", "kill-session", "-t", info.name)
		if err := cmd2.MakeExecutor().Run(killCmd); err != nil {
			log.WarningLog.Printf("failed to kill session %s: %v", info.name, err)
			fmt.Printf("  Warning: Failed to kill %s\n", info.name)
		}
	}

	fmt.Println("\nCleanup complete!")
	return nil
}

// killAllClaudeSquadSessions kills all claude-squad sessions without prompting
func killAllClaudeSquadSessions() error {
	sessions, err := findClaudeSquadSessions()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions to clean up")
		return nil
	}

	fmt.Printf("Killing %d session(s)...\n", len(sessions))
	for _, sess := range sessions {
		fmt.Printf("  Killing: %s\n", sess)
		killCmd := exec.Command("tmux", "kill-session", "-t", sess)
		if err := cmd2.MakeExecutor().Run(killCmd); err != nil {
			log.WarningLog.Printf("failed to kill session %s: %v", sess, err)
			fmt.Printf("  Warning: Failed to kill %s\n", sess)
		}
	}

	fmt.Println("\nCleanup complete!")
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
	}
}
