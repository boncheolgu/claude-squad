package daemon

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// RunDaemon runs the daemon process which iterates over all sessions in a repository and runs AutoYes mode on them.
// It's expected that the main process kills the daemon when the main process starts.
func RunDaemon(cfg *config.Config, repoPath string) error {
	log.InfoLog.Printf("starting daemon for repo: %s", repoPath)
	state := config.LoadState(repoPath)
	storage, err := session.NewStorage(state)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	instances, err := storage.LoadInstances()
	if err != nil {
		return fmt.Errorf("failed to load instacnes: %w", err)
	}
	for _, instance := range instances {
		// Assume AutoYes is true if the daemon is running.
		instance.AutoYes = true
	}

	pollInterval := time.Duration(cfg.DaemonPollInterval) * time.Millisecond

	// If we get an error for a session, it's likely that we'll keep getting the error. Log every 30 seconds.
	everyN := log.NewEvery(60 * time.Second)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	stopCh := make(chan struct{})
	go func() {
		defer wg.Done()
		ticker := time.NewTimer(pollInterval)
		for {
			for _, instance := range instances {
				// We only store started instances, but check anyway.
				if instance.Started() && !instance.Paused() {
					if _, hasPrompt := instance.HasUpdated(); hasPrompt {
						instance.TapEnter()
						if err := instance.UpdateDiffStats(); err != nil {
							if everyN.ShouldLog() {
								log.WarningLog.Printf("could not update diff stats for %s: %v", instance.Title, err)
							}
						}
					}
				}
			}

			// Handle stop before ticker.
			select {
			case <-stopCh:
				return
			default:
			}

			<-ticker.C
			ticker.Reset(pollInterval)
		}
	}()

	// Notify on SIGINT (Ctrl+C) and SIGTERM. Save instances before
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	log.InfoLog.Printf("received signal %s", sig.String())

	// Stop the goroutine so we don't race.
	close(stopCh)
	wg.Wait()

	if err := storage.SaveInstances(instances); err != nil {
		log.ErrorLog.Printf("failed to save instances when terminating daemon: %v", err)
	}
	return nil
}

// LaunchDaemon launches the daemon process for a specific repository.
func LaunchDaemon(repoPath string) error {
	// Find the claude squad binary.
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Pass repo path to daemon so it knows which repo to monitor
	cmd := exec.Command(execPath, "--daemon", "--repo-path", repoPath)

	// Detach the process from the parent
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Set process group to prevent signals from propagating
	cmd.SysProcAttr = getSysProcAttr()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start child process: %w", err)
	}

	log.InfoLog.Printf("started daemon child process with PID: %d for repo %s", cmd.Process.Pid, repoPath)

	// Save PID to per-repo state directory
	stateDir, err := config.GetStateDir(repoPath)
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}

	pidFile := filepath.Join(stateDir, "daemon.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Don't wait for the child to exit, it's detached
	return nil
}

// StopDaemon attempts to stop a running daemon process for a specific repository.
// Returns no error if the daemon is not found (assumes the daemon does not exist).
func StopDaemon(repoPath string) error {
	stateDir, err := config.GetStateDir(repoPath)
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}

	pidFile := filepath.Join(stateDir, "daemon.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return fmt.Errorf("invalid PID file format: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find daemon process: %w", err)
	}

	if err := proc.Kill(); err != nil {
		return fmt.Errorf("failed to stop daemon process: %w", err)
	}

	// Clean up PID file
	if err := os.Remove(pidFile); err != nil {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}

	log.InfoLog.Printf("daemon process (PID: %d) stopped successfully", pid)
	return nil
}
