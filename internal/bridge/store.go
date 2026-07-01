package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Store struct {
	path     string
	mu       sync.Mutex
	Sessions map[string]SessionState `json:"sessions"`
}

type SessionState struct {
	Key       string    `json:"key"`
	ThreadID  string    `json:"thread_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path:     filepath.Join(dataDir, "sessions.json"),
		Sessions: map[string]SessionState{},
	}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(b, s); err != nil {
			return nil, fmt.Errorf("decode %s: %w", s.path, err)
		}
	}
	if s.Sessions == nil {
		s.Sessions = map[string]SessionState{}
	}
	return s, nil
}

func (s *Store) Get(key string) SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Sessions[key]
}

func (s *Store) SetThread(key, threadID string) error {
	if key == "" || threadID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions[key] = SessionState{Key: key, ThreadID: threadID, UpdatedAt: time.Now()}
	return s.saveLocked()
}

func (s *Store) Reset(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Sessions, key)
	return s.saveLocked()
}

func (s *Store) List() []SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionState, 0, len(s.Sessions))
	for _, state := range s.Sessions {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *Store) saveLocked() error {
	tmp := s.path + ".tmp"
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
