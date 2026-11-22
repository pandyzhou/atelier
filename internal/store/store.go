// Package store keeps each feature's data as a JSON file in the app's user
// data directory (%AppData%\Atelier on Windows).
package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Store reads and writes JSON documents by name. A Store with an empty
// directory keeps nothing on disk, which tests use.
type Store struct {
	dir string
}

// Open returns a store writing into dir; "" keeps nothing.
func Open(dir string) *Store { return &Store{dir: dir} }

// Dir is where the store writes, "" when it keeps nothing.
func (s *Store) Dir() string { return s.dir }

// Load decodes the document name into v. A document that does not exist yet
// leaves v as it is and is not an error.
func (s *Store) Load(name string, v any) error {
	if s.dir == "" {
		return nil
	}
	b, err := os.ReadFile(s.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Save encodes v as the document name. It writes a temporary file first and
// renames it over the old one, so a crash never leaves half a file.
func (s *Store) Save(name string, v any) error {
	if s.dir == "" {
		return nil
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	tmp := s.path(name) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name+".json") }
