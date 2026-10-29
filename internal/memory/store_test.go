package memory

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fact(key, value, event, date string) Fact {
	t, err := time.Parse(time.RFC3339, date)
	if err != nil {
		panic(err)
	}
	return Fact{Key: key, Value: value, SourceEventID: event, ObservedAt: t}
}

func TestCorrectionAndLateHistoryDoNotReviveOldRelationship(t *testing.T) {
	s, _ := Open("")
	old, err := s.Set("alice", fact("relationship_status", "연애 중", "e1", "2026-10-01T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.Set("alice", fact("relationship_status", "헤어짐", "e3", "2026-10-03T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	late, err := s.Set("alice", fact("relationship_status", "갈등 후 대화 중", "e2", "2026-10-02T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	current, _ := s.Current("alice")
	if len(current) != 1 || current[0].Value != "헤어짐" || late.Current || updated.Supersedes != old.ID {
		t.Fatalf("old event changed current relationship: %+v", current)
	}
	history, _ := s.History("alice", "relationship_status")
	if len(history) != 3 || history[0].SourceEventID != "e1" || history[1].SourceEventID != "e2" || history[2].SourceEventID != "e3" || history[0].Current || !history[2].Current {
		t.Fatalf("bad source history: %+v", history)
	}
}

func TestUserIsolationDeletionAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "memory.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f := fact("contact_boundary", "연락하지 않기", "same-event", "2026-10-01T10:00:00Z")
	if _, err = s.Set("alice", f); err != nil {
		t.Fatal(err)
	}
	f.Value = "주말에만 연락하기"
	if _, err = s.Set("bob", f); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := reloaded.Current("alice")
	b, _ := reloaded.Current("bob")
	unknown, _ := reloaded.Current("unknown")
	if len(a) != 1 || len(b) != 1 || a[0].Value == b[0].Value || len(unknown) != 0 {
		t.Fatalf("user isolation failed")
	}
	if err = reloaded.DeleteUser("alice"); err != nil {
		t.Fatal(err)
	}
	final, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ = final.History("alice", "")
	b, _ = final.Current("bob")
	if len(a) != 0 || len(b) != 1 {
		t.Fatal("delete left history or deleted another user")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("private file permission %o", info.Mode().Perm())
	}
}

func TestReplayIdempotencyAndConflicts(t *testing.T) {
	s, _ := Open("")
	f := fact("relationship_status", "연애 중", "e1", "2026-10-01T10:00:00Z")
	first, _ := s.Set("alice", f)
	second, err := s.Set("alice", f)
	if err != nil || first.ID != second.ID {
		t.Fatal("replay not idempotent")
	}
	f.Value = "헤어짐"
	if _, err = s.Set("alice", f); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected source conflict: %v", err)
	}
	f.SourceEventID = "different-event"
	if _, err = s.Set("alice", f); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected timestamp conflict: %v", err)
	}
	rows, _ := s.History("alice", "")
	if len(rows) != 1 {
		t.Fatal("conflict mutated history")
	}
}

func TestFailedPersistenceLeavesMemoryUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = s.Set("alice", fact("status", "new", "e1", "2026-10-01T10:00:00Z"))
	if err == nil {
		t.Fatal("expected failure when target path is a directory")
	}
	rows, _ := s.Current("alice")
	if len(rows) != 0 {
		t.Fatal("failed disk write leaked into in-memory state")
	}
}

func TestCorruptStoreAndUnattributedFactsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"users":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("corrupt file must not be replaced")
	}
	s, _ := Open("")
	if _, err := s.Set("alice", Fact{Key: "guess", Value: "진중한 성격"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unattributed guess accepted: %v", err)
	}
	if _, err := s.Current(""); !errors.Is(err, ErrInvalid) {
		t.Fatal("unscoped read accepted")
	}
}

func TestConcurrentUsersDoNotLoseWrites(t *testing.T) {
	s, _ := Open("")
	var wg sync.WaitGroup
	for _, user := range []string{"alice", "bob", "carol", "dave"} {
		wg.Add(1)
		go func(user string) {
			defer wg.Done()
			_, err := s.Set(user, fact("boundary", "ask first", "e1", "2026-10-01T10:00:00Z"))
			if err != nil {
				t.Error(err)
			}
		}(user)
	}
	wg.Wait()
	for _, user := range []string{"alice", "bob", "carol", "dave"} {
		rows, _ := s.Current(user)
		if len(rows) != 1 {
			t.Fatalf("lost write for %s", user)
		}
	}
}
