package config

import (
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	StateFileName     = "state.json"
	InstancesFileName = "instances.json"
	StateDirName      = ".claude-squad"
)

// InstanceStorage handles instance-related operations
type InstanceStorage interface {
	// SaveInstances saves the raw instance data
	SaveInstances(instancesJSON json.RawMessage) error
	// GetInstances returns the raw instance data
	GetInstances() json.RawMessage
	// DeleteAllInstances removes all stored instances
	DeleteAllInstances() error
}

// AppState handles application-level state
type AppState interface {
	// GetHelpScreensSeen returns the bitmask of seen help screens
	GetHelpScreensSeen() uint32
	// SetHelpScreensSeen updates the bitmask of seen help screens
	SetHelpScreensSeen(seen uint32) error
}

// StateManager combines instance storage and app state management
type StateManager interface {
	InstanceStorage
	AppState
}

// State represents the application state that persists between sessions
type State struct {
	// HelpScreensSeen is a bitmask tracking which help screens have been shown
	HelpScreensSeen uint32 `json:"help_screens_seen"`
	// Instances stores the serialized instance data as raw JSON
	InstancesData json.RawMessage `json:"instances"`

	// repoPath is the repository path this state belongs to (not serialized)
	repoPath string `json:"-"`
}

// DefaultState returns the default state
func DefaultState() *State {
	return &State{
		HelpScreensSeen: 0,
		InstancesData:   json.RawMessage("[]"),
	}
}

// GetStateDir returns the per-repo state directory path.
// For a repository at /home/user/project, this returns /home/user/project/.claude-squad/
func GetStateDir(repoPath string) (string, error) {
	// Get canonical path to handle symlinks
	canonical, err := GetCanonicalRepoPath(repoPath)
	if err != nil {
		return "", fmt.Errorf("failed to get canonical repo path: %w", err)
	}

	stateDir := filepath.Join(canonical, StateDirName)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create state directory %s: %w (check write permissions)", stateDir, err)
	}

	// Create .gitignore to ignore all contents
	gitignorePath := filepath.Join(stateDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte("*\n"), 0644); err != nil {
			log.WarningLog.Printf("failed to create .gitignore in state directory: %v", err)
		}
	}

	return stateDir, nil
}

// LoadState loads the state from disk. If it cannot be done, we return the default state.
func LoadState(repoPath string) *State {
	stateDir, err := GetStateDir(repoPath)
	if err != nil {
		log.ErrorLog.Printf("failed to get state directory: %v", err)
		return DefaultState()
	}

	statePath := filepath.Join(stateDir, StateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create and save default state if file doesn't exist
			defaultState := DefaultState()
			if saveErr := SaveState(defaultState, repoPath); saveErr != nil {
				log.WarningLog.Printf("failed to save default state: %v", saveErr)
			}
			return defaultState
		}

		log.WarningLog.Printf("failed to read state file: %v", err)
		defaultState := DefaultState()
		defaultState.repoPath = repoPath
		return defaultState
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		// State file is corrupted - try to recover from backup
		log.ErrorLog.Printf("state file corrupted: %v", err)

		// Preserve corrupted file for inspection
		corruptedPath := fmt.Sprintf("%s.corrupted.%d", statePath, time.Now().Unix())
		if renameErr := os.Rename(statePath, corruptedPath); renameErr != nil {
			log.ErrorLog.Printf("failed to preserve corrupted state: %v", renameErr)
		} else {
			log.InfoLog.Printf("corrupted state preserved at: %s", corruptedPath)
		}

		// Try to restore from backup
		backupPath := statePath + ".bak"
		backupData, backupErr := os.ReadFile(backupPath)
		if backupErr == nil {
			var backupState State
			if json.Unmarshal(backupData, &backupState) == nil {
				log.InfoLog.Printf("successfully restored state from backup")
				backupState.repoPath = repoPath
				return &backupState
			}
			log.ErrorLog.Printf("backup file is also corrupted")
		}

		// No recovery possible, start fresh
		log.WarningLog.Printf("starting with fresh state - previous instances lost")
		defaultState := DefaultState()
		defaultState.repoPath = repoPath
		return defaultState
	}

	state.repoPath = repoPath
	return &state
}

// SaveState saves the state to disk with proactive backup
func SaveState(state *State, repoPath string) error {
	stateDir, err := GetStateDir(repoPath)
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}

	statePath := filepath.Join(stateDir, StateFileName)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Proactive backup: if state.json exists, back it up before writing new state
	backupPath := statePath + ".bak"
	if _, err := os.Stat(statePath); err == nil {
		// Existing state file - back it up
		if err := os.Rename(statePath, backupPath); err != nil {
			// If rename fails, try copying instead
			if existing, readErr := os.ReadFile(statePath); readErr == nil {
				_ = os.WriteFile(backupPath, existing, 0644)
			}
		}
	}

	// Write new state
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		// Write failed - try to restore backup
		if _, statErr := os.Stat(backupPath); statErr == nil {
			_ = os.Rename(backupPath, statePath)
		}
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// InstanceStorage interface implementation

// SaveInstances saves the raw instance data
func (s *State) SaveInstances(instancesJSON json.RawMessage) error {
	s.InstancesData = instancesJSON
	return SaveState(s, s.repoPath)
}

// GetInstances returns the raw instance data
func (s *State) GetInstances() json.RawMessage {
	return s.InstancesData
}

// DeleteAllInstances removes all stored instances
func (s *State) DeleteAllInstances() error {
	s.InstancesData = json.RawMessage("[]")
	return SaveState(s, s.repoPath)
}

// AppState interface implementation

// GetHelpScreensSeen returns the bitmask of seen help screens
func (s *State) GetHelpScreensSeen() uint32 {
	return s.HelpScreensSeen
}

// SetHelpScreensSeen updates the bitmask of seen help screens
func (s *State) SetHelpScreensSeen(seen uint32) error {
	s.HelpScreensSeen = seen
	return SaveState(s, s.repoPath)
}
