package tracker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// APPosition is the placement of an AP dot on the floorplan image, in pixel
// coordinates relative to the image's top-left.
type APPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PositionStore is a JSON-file backed map keyed by AP name.
type PositionStore struct {
	path string
	mu   sync.RWMutex
	data map[string]APPosition
}

func NewPositionStore(path string) (*PositionStore, error) {
	s := &PositionStore{path: path, data: map[string]APPosition{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PositionStore) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		backup := fmt.Sprintf("%s.broken-%s", s.path, time.Now().Format("20060102-150405"))
		if mvErr := os.Rename(s.path, backup); mvErr != nil {
			return fmt.Errorf("decode %s: %w (also failed to rename: %v)", s.path, err, mvErr)
		}
		log.Printf("[positions] %s could not be parsed (%v); moved to %s, starting empty", s.path, err, backup)
		s.data = map[string]APPosition{}
	}
	return nil
}

func (s *PositionStore) All() map[string]APPosition {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]APPosition, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

// Replace overwrites the full position map. We replace rather than merge
// because the editor sends the entire set on save — a partial update would
// require client-side bookkeeping to track which APs were removed.
func (s *PositionStore) Replace(positions map[string]APPosition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]APPosition, len(positions))
	for k, v := range positions {
		s.data[k] = v
	}
	return s.persist()
}

func (s *PositionStore) persist() error {
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.data); err != nil {
		f.Close()
		return fmt.Errorf("encode positions: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
