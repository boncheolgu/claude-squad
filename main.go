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
	version      = "1.0.13"
	programFlag  string
	autoYesFlag  bool
	daemonFlag   bool
	repoPathFlag string
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
		Short: "Clean up orphaned tmux sessions",
		Long: `Clean up tmux sessions that no longer have an associated repository.

This happens when:
- A repository is deleted but tmux sessions remain
- .claude-squad/ directory is removed manually
- Sessions are left after repository moves`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

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

	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(cleanupCmd)
}

// cleanupOrphanedSessions finds and removes tmux sessions that no longer have an associated repository
func cleanupOrphanedSessions() error {
	// List all tmux sessions
	cmd := exec.Command("tmux", "ls")
	output, err := cmd2.MakeExecutor().Output(cmd)
	if err != nil {
		// Exit code 1 means no sessions exist
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			fmt.Println("No tmux sessions found")
			return nil
		}
		return fmt.Errorf("failed to list tmux sessions: %w", err)
	}

	// Parse session names matching claudesquad_<hash>_*
	// Format: claudesquad_fe08346a_mytask: ...
	re := regexp.MustCompile(`claudesquad_([a-f0-9]{8})_[^:]+:`)
	matches := re.FindAllStringSubmatch(string(output), -1)

	if len(matches) == 0 {
		fmt.Println("No claude-squad tmux sessions found")
		return nil
	}

	// Collect unique repo hashes
	repoHashes := make(map[string][]string) // hash -> list of session names
	for _, match := range matches {
		if len(match) >= 2 {
			hash := match[1]
			sessionName := strings.TrimSuffix(match[0], ":")
			repoHashes[hash] = append(repoHashes[hash], sessionName)
		}
	}

	fmt.Printf("Found %d unique repository hashes in tmux sessions\n", len(repoHashes))

	// For each hash, check if .claude-squad directory exists
	orphanedSessions := []string{}
	for hash, sessions := range repoHashes {
		// Check if worktree directory exists for this hash
		configDir, err := config.GetConfigDir()
		if err != nil {
			return fmt.Errorf("failed to get config directory: %w", err)
		}

		worktreeDir := filepath.Join(configDir, "worktrees", hash)

		// If worktree directory doesn't exist, these sessions are orphaned
		if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
			fmt.Printf("Repo hash %s: No worktree directory found (orphaned)\n", hash)
			orphanedSessions = append(orphanedSessions, sessions...)
		} else {
			fmt.Printf("Repo hash %s: Active (%d sessions)\n", hash, len(sessions))
		}
	}

	if len(orphanedSessions) == 0 {
		fmt.Println("\nNo orphaned sessions found - all clean!")
		return nil
	}

	// Kill orphaned sessions
	fmt.Printf("\nCleaning up %d orphaned session(s)...\n", len(orphanedSessions))
	for _, sessionName := range orphanedSessions {
		fmt.Printf("  Killing: %s\n", sessionName)
		killCmd := exec.Command("tmux", "kill-session", "-t", sessionName)
		if err := cmd2.MakeExecutor().Run(killCmd); err != nil {
			log.WarningLog.Printf("failed to kill session %s: %v", sessionName, err)
			fmt.Printf("  Warning: Failed to kill %s\n", sessionName)
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
