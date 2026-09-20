package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAddAssignsIncrementalIDs(t *testing.T) {
	store := newTestStore(t)
	added, err := store.Add(
		Rule{Kind: KindUpVideo, Label: "a", Target: Target{UID: 1}},
		Rule{Kind: KindUpLive, Label: "b", Target: Target{UID: 1, RoomID: "2"}},
	)
	if err != nil || len(added) != 2 || added[0].ID != 1 || added[1].ID != 2 || !added[0].Enabled {
		t.Fatalf("Add() = %#v, %v", added, err)
	}
	file, err := store.Load()
	if err != nil || file.NextID != 3 || len(file.Rules) != 2 {
		t.Fatalf("Load() = %#v, %v", file, err)
	}
}

func TestStoreRemoveAndDisable(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Add(Rule{Kind: KindSearchKeyword, Target: Target{Query: "go"}}); err != nil {
		t.Fatal(err)
	}
	rule, err := store.SetEnabled(1, false)
	if err != nil || rule.Enabled {
		t.Fatalf("SetEnabled() = %#v, %v", rule, err)
	}
	removed, err := store.Remove(1)
	if err != nil || removed.ID != 1 {
		t.Fatalf("Remove() = %#v, %v", removed, err)
	}
	file, err := store.Load()
	if err != nil || len(file.Rules) != 0 {
		t.Fatalf("Load() after remove = %#v, %v", file, err)
	}
}

func TestMergeSeenCapsAndNewIDs(t *testing.T) {
	previous := []string{"a", "b"}
	current := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		current = append(current, fmt.Sprintf("id-%02d", i))
	}
	merged := mergeSeen(previous, current)
	if len(merged) != maxSeenIDs {
		t.Fatalf("mergeSeen len = %d, want %d", len(merged), maxSeenIDs)
	}
	added := newIDs([]string{"old", "keep"}, []string{"new", "keep", "older"})
	if len(added) != 2 || added[0] != "older" || added[1] != "new" {
		t.Fatalf("newIDs() = %#v", added)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return &Store{
		Dir:  dir,
		File: filepath.Join(dir, "watch.json"),
		Now:  func() time.Time { return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC) },
	}
}

func TestStoreLoadMissingFile(t *testing.T) {
	store := newTestStore(t)
	file, err := store.Load()
	if err != nil || file.Version != CurrentVersion || file.NextID != 1 {
		t.Fatalf("Load missing = %#v, %v", file, err)
	}
	if _, err := os.Stat(store.File); !os.IsNotExist(err) {
		t.Fatalf("missing load created file: %v", err)
	}
}
