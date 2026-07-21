package setupstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRoundTripAndResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	state := New()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if err := state.Complete(CheckpointNetwork, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Completed(CheckpointNetwork) || got.Completed(CheckpointPrinter) {
		t.Fatalf("unexpected checkpoints: %#v", got)
	}
	if got.UpdatedAt != now {
		t.Fatalf("updated at = %v, want %v", got.UpdatedAt, now)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestSaveIsAtomicAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewStore(path)
	state := New()
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("unexpected files after save: %v", entries)
	}
}

func TestCorruptStateFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"checkpoints":{"network":"not-a-time"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(path).Load()
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("error = %v, want ErrCorrupt", err)
	}
	if len(got.Checkpoints) != 0 {
		t.Fatalf("corrupt state must not resume: %#v", got)
	}
}

func TestUnknownFieldsAndTrailingJSONFailClosed(t *testing.T) {
	for _, body := range []string{
		`{"version":1,"checkpoints":{},"wifi_psk":"secret"}`,
		`{"version":1,"checkpoints":{}} {}`,
	} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(path).Load(); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("body %q: error = %v, want ErrCorrupt", body, err)
		}
	}
}

func TestOversizedStateFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxStateBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path).Load(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("error = %v, want ErrCorrupt", err)
	}
}

func TestLegacySchemaMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	body := `{"version":0,"completed":["welcome","network"]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion || !got.Completed(CheckpointWelcome) || !got.Completed(CheckpointNetwork) {
		t.Fatalf("migration result: %#v", got)
	}
}

func TestCompleteRejectsUnknownCheckpoint(t *testing.T) {
	state := New()
	if err := state.Complete(Checkpoint("oauth-token"), time.Now()); err == nil {
		t.Fatal("expected unknown checkpoint rejection")
	}
}

func TestMissingStateStartsFresh(t *testing.T) {
	got, err := NewStore(filepath.Join(t.TempDir(), "missing.json")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != CurrentVersion || len(got.Checkpoints) != 0 {
		t.Fatalf("fresh state = %#v", got)
	}
}
