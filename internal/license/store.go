package license

import (
	"encoding/json"
	"os"
	"sync"
)

// Store persists the signed license State to a JSON file.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a Store backed by the given file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Load reads the persisted State. A missing file returns a zero State without an
// error so first-run is seamless.
func (store *Store) Load() (State, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	data, err := os.ReadFile(store.path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

// Save persists the State atomically (temp file + rename).
func (store *Store) Save(state State) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := store.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, store.path)
}

// Clear removes the persisted State (deactivation).
func (store *Store) Clear() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	err := os.Remove(store.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
