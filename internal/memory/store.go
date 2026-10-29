// Package memory stores user-confirmed facts with their original event and time.
// It deliberately does not infer personality or facts from conversation text.
package memory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid memory input")
	ErrConflict = errors.New("memory event or timestamp conflict")
	ErrNotFound = errors.New("memory not found")
	ErrScope    = errors.New("memory does not belong to this user")
)

// Fact is an explicit, confirmed fact. ObservedAt is the event's time, not the
// upload time. A newer fact for the same key changes the current value.
type Fact struct {
	Key           string    `json:"key"`
	Value         string    `json:"value"`
	SourceEventID string    `json:"source_event_id"`
	ObservedAt    time.Time `json:"observed_at"`
	Note          string    `json:"note,omitempty"`
}

type Record struct {
	ID string `json:"id"`
	Fact
	RecordedAt time.Time `json:"recorded_at"`
	Supersedes string    `json:"supersedes,omitempty"`
	Current    bool      `json:"current"`
}

type diskState struct {
	Version int                 `json:"version"`
	Users   map[string][]Record `json:"users"`
}

// Store serializes writes within one process. Use a database before running
// multiple server processes; this JSON implementation has no interprocess lock.
type Store struct {
	mu    sync.RWMutex
	path  string
	state diskState
}

// Open loads a private JSON file, or creates an in-memory store for path == "".
// It never silently replaces an unreadable or corrupt existing file.
func Open(path string) (*Store, error) {
	s := &Store{path: path, state: diskState{Version: 1, Users: map[string][]Record{}}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read memory store: %w", err)
	}
	if err := s.loadSnapshot(data); err != nil {
		return nil, err
	}
	return s, nil
}

// ValidateSnapshot validates the same byte sequence the JSON store would load,
// without writing a private copy to disk.
func ValidateSnapshot(data []byte) error {
	return (&Store{}).loadSnapshot(data)
}

func (s *Store) loadSnapshot(data []byte) error {
	if err := json.Unmarshal(data, &s.state); err != nil {
		return fmt.Errorf("decode memory store: %w", err)
	}
	if s.state.Version != 1 || s.state.Users == nil {
		return fmt.Errorf("unsupported memory store format")
	}
	ids := map[string]bool{}
	for user, records := range s.state.Users {
		if err := validateUser(user); err != nil {
			return err
		}
		for _, r := range records {
			if err := validateFact(r.Fact); err != nil {
				return err
			}
			if r.ID == "" || ids[r.ID] || r.RecordedAt.IsZero() {
				return fmt.Errorf("invalid or duplicate stored record")
			}
			ids[r.ID] = true
		}
		s.state.Users[user] = markCurrent(records)
	}
	return nil
}

func validateUser(user string) error {
	if strings.TrimSpace(user) == "" || len(user) > 256 {
		return fmt.Errorf("%w: user is required and must be <=256 bytes", ErrInvalid)
	}
	return nil
}

func validateFact(f Fact) error {
	if strings.TrimSpace(f.Key) == "" || len(f.Key) > 128 || strings.TrimSpace(f.Value) == "" || len(f.Value) > 8192 || strings.TrimSpace(f.SourceEventID) == "" || len(f.SourceEventID) > 256 || len(f.Note) > 8192 || f.ObservedAt.IsZero() {
		return fmt.Errorf("%w: key, value, source_event_id and observed_at are required; length limits apply", ErrInvalid)
	}
	return nil
}

// Set appends a revision. Late-arriving old events remain in history and cannot
// replace current facts. Replaying an identical event is idempotent; conflicting
// uses of one event ID/key or two different facts at the same time are rejected.
func (s *Store) Set(userID string, fact Fact) (Record, error) {
	if err := validateUser(userID); err != nil {
		return Record{}, err
	}
	if err := validateFact(fact); err != nil {
		return Record{}, err
	}
	fact.ObservedAt = fact.ObservedAt.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	var current *Record
	for _, r := range s.state.Users[userID] {
		if r.Key != fact.Key {
			continue
		}
		if r.SourceEventID == fact.SourceEventID {
			if r.Value == fact.Value && r.ObservedAt.Equal(fact.ObservedAt) && r.Note == fact.Note {
				return r, nil
			}
			return Record{}, fmt.Errorf("%w: source event was already recorded for this key", ErrConflict)
		}
		if r.ObservedAt.Equal(fact.ObservedAt) && r.Value != fact.Value {
			return Record{}, fmt.Errorf("%w: two values share one event time; clarify the time", ErrConflict)
		}
		if r.Current {
			rr := r
			current = &rr
		}
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Record{}, err
	}
	record := Record{ID: hex.EncodeToString(idBytes), Fact: fact, RecordedAt: time.Now().UTC()}
	if current != nil && fact.ObservedAt.After(current.ObservedAt) {
		record.Supersedes = current.ID
	}
	next := cloneState(s.state)
	next.Users[userID] = markCurrent(append(next.Users[userID], record))
	if err := s.persist(next); err != nil {
		return Record{}, err
	}
	s.state = next
	for _, r := range next.Users[userID] {
		if r.ID == record.ID {
			return r, nil
		}
	}
	panic("inserted memory missing")
}

// Current returns one current revision per key, sorted by key. Empty users yield
// an empty slice. The caller must obtain userID from authenticated server state.
func (s *Store) Current(userID string) ([]Record, error) {
	if err := validateUser(userID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for _, r := range s.state.Users[userID] {
		if r.Current {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// History returns chronological revisions for one key, or all keys if key="".
func (s *Store) History(userID, key string) ([]Record, error) {
	if err := validateUser(userID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Record{}
	for _, r := range s.state.Users[userID] {
		if key == "" || r.Key == key {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ObservedAt.Before(out[j].ObservedAt) })
	return out, nil
}

// DeleteUser removes both current and historical local facts. It does not delete
// backups, application logs, or a separate Mem0 service; handle those explicitly.
func (s *Store) DeleteUser(userID string) error {
	if err := validateUser(userID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.state)
	delete(next.Users, userID)
	if err := s.persist(next); err != nil {
		return err
	}
	s.state = next
	return nil
}

func markCurrent(records []Record) []Record {
	latest := map[string]int{}
	for i := range records {
		records[i].Current = false
		j, ok := latest[records[i].Key]
		if !ok || records[i].ObservedAt.After(records[j].ObservedAt) {
			latest[records[i].Key] = i
		}
	}
	for _, i := range latest {
		records[i].Current = true
	}
	return records
}

func cloneState(src diskState) diskState {
	dst := diskState{Version: src.Version, Users: make(map[string][]Record, len(src.Users))}
	for user, records := range src.Users {
		dst.Users[user] = append([]Record(nil), records...)
	}
	return dst
}

func (s *Store) persist(next diskState) error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create memory directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, s.path); err != nil {
		return fmt.Errorf("commit memory store: %w", err)
	}
	return nil
}
