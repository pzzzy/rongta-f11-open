// Package setupstate persists the wizard's non-secret progress checkpoints.
package setupstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	CurrentVersion = 1
	MaxStateBytes  = 64 << 10
)

var ErrCorrupt = errors.New("setup state is corrupt")

type Checkpoint string

const (
	CheckpointWelcome           Checkpoint = "welcome"
	CheckpointNetwork           Checkpoint = "network"
	CheckpointPrinter           Checkpoint = "printer"
	CheckpointTwitch            Checkpoint = "twitch"
	CheckpointEventSub          Checkpoint = "eventsub"
	CheckpointPreview           Checkpoint = "preview"
	CheckpointPhysicalAttempted Checkpoint = "physical_attempted"
	CheckpointComplete          Checkpoint = "complete"
)

var validCheckpoints = map[Checkpoint]bool{
	CheckpointWelcome: true, CheckpointNetwork: true, CheckpointPrinter: true,
	CheckpointTwitch: true, CheckpointEventSub: true, CheckpointPreview: true,
	CheckpointPhysicalAttempted: true, CheckpointComplete: true,
}

// State deliberately contains only non-secret checkpoint evidence.
type State struct {
	Version     int                      `json:"version"`
	Checkpoints map[Checkpoint]time.Time `json:"checkpoints"`
	UpdatedAt   time.Time                `json:"updated_at,omitempty"`
}

type Store struct{ path string }

func NewStore(path string) *Store { return &Store{path: path} }

func New() State {
	return State{Version: CurrentVersion, Checkpoints: make(map[Checkpoint]time.Time)}
}

func (s State) Completed(checkpoint Checkpoint) bool {
	_, ok := s.Checkpoints[checkpoint]
	return ok
}

func (s *State) Complete(checkpoint Checkpoint, at time.Time) error {
	if !validCheckpoints[checkpoint] {
		return fmt.Errorf("unknown checkpoint %q", checkpoint)
	}
	if at.IsZero() {
		return errors.New("checkpoint time must not be zero")
	}
	if s.Checkpoints == nil {
		s.Checkpoints = make(map[Checkpoint]time.Time)
	}
	s.Checkpoints[checkpoint] = at.UTC()
	s.UpdatedAt = at.UTC()
	return nil
}

func (s *Store) Load() (State, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return New(), err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxStateBytes+1))
	if err != nil || len(data) > MaxStateBytes {
		return New(), fmt.Errorf("%w: unreadable or oversized", ErrCorrupt)
	}

	var version struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &version); err != nil {
		return New(), fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if version.Version == 0 {
		return decodeLegacy(data)
	}
	if version.Version != CurrentVersion {
		return New(), fmt.Errorf("%w: unsupported version %d", ErrCorrupt, version.Version)
	}

	var state State
	if err := decodeStrict(data, &state); err != nil {
		return New(), fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if err := validate(state); err != nil {
		return New(), fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return state, nil
}

func decodeLegacy(data []byte) (State, error) {
	var legacy struct {
		Version   int          `json:"version"`
		Completed []Checkpoint `json:"completed"`
	}
	if err := decodeStrict(data, &legacy); err != nil {
		return New(), fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	state := New()
	// Legacy checkpoints did not contain evidence timestamps. Unix epoch is an
	// explicit migration marker, not a claim about when a check passed.
	for _, checkpoint := range legacy.Completed {
		if !validCheckpoints[checkpoint] {
			return New(), fmt.Errorf("%w: unknown checkpoint %q", ErrCorrupt, checkpoint)
		}
		state.Checkpoints[checkpoint] = time.Unix(0, 0).UTC()
	}
	return state, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func validate(state State) error {
	if state.Version != CurrentVersion || state.Checkpoints == nil {
		return errors.New("invalid schema")
	}
	for checkpoint, at := range state.Checkpoints {
		if !validCheckpoints[checkpoint] || at.IsZero() {
			return fmt.Errorf("invalid checkpoint %q", checkpoint)
		}
	}
	return nil
}

func (s *Store) Save(state State) error {
	state.Version = CurrentVersion
	if err := validate(state); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > MaxStateBytes {
		return errors.New("setup state exceeds size limit")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".setup-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
