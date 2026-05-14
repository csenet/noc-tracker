package tracker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

type Registration struct {
	MAC       string    `json:"mac"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	path string
	mu   sync.RWMutex
	data map[string]Registration // key: normalized MAC
}

func NewStore(path string) (*Store, error) {
	s := &Store{path: path, data: map[string]Registration{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
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

	var arr []Registration
	if err := json.Unmarshal(data, &arr); err != nil {
		// Don't refuse to boot just because the store file got corrupted —
		// move it aside and start empty. Registrations are easy to re-enter;
		// a server that won't start is worse than a server that lost them.
		backup := fmt.Sprintf("%s.broken-%s", s.path, time.Now().Format("20060102-150405"))
		if mvErr := os.Rename(s.path, backup); mvErr != nil {
			return fmt.Errorf("decode %s: %w (also failed to rename: %v)", s.path, err, mvErr)
		}
		log.Printf("[store] %s could not be parsed (%v); moved to %s, starting empty", s.path, err, backup)
		return nil
	}
	for _, r := range arr {
		if mac := NormalizeMAC(r.MAC); mac != "" {
			r.MAC = mac
			s.data[mac] = r
		}
	}
	return nil
}

func (s *Store) persist() error {
	arr := make([]Registration, 0, len(s.data))
	for _, r := range s.data {
		arr = append(arr, r)
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Name < arr[j].Name })

	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(arr); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Register(mac, name, owner string) (Registration, error) {
	norm := NormalizeMAC(mac)
	if norm == "" {
		return Registration{}, fmt.Errorf("invalid MAC: %q", mac)
	}
	if name == "" {
		return Registration{}, fmt.Errorf("name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	r, existed := s.data[norm]
	r.MAC = norm
	r.Name = name
	r.Owner = owner
	if !existed {
		r.CreatedAt = time.Now()
	}
	s.data[norm] = r
	if err := s.persist(); err != nil {
		return Registration{}, err
	}
	return r, nil
}

func (s *Store) Delete(mac string) error {
	norm := NormalizeMAC(mac)
	if norm == "" {
		return fmt.Errorf("invalid MAC")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, norm)
	return s.persist()
}

func (s *Store) Get(mac string) (Registration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data[NormalizeMAC(mac)]
	return r, ok
}

func (s *Store) All() []Registration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Registration, 0, len(s.data))
	for _, r := range s.data {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
