// Package attachfs stores problem and ticket attachments under the data
// directory. The database row is the record; the file name on disk is random and
// unrelated to what the uploader called it, so a crafted name never becomes a path.
package attachfs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store struct{ root string }

func New(dataDir string) *Store { return &Store{root: filepath.Join(dataDir, "attachments")} }

func (s *Store) Root() string { return s.root }

func ownerDir(kind string, ownerID int64) string {
	return filepath.Join(kind, fmt.Sprintf("%d", ownerID))
}

// Save writes body under <kind>/<ownerID>/<random> and returns the path relative
// to the store root. kind is one of "problem"/"ticket" (the caller validates it).
func (s *Store) Save(kind string, ownerID int64, body []byte) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	rel := ownerDir(kind, ownerID) + "/" + hex.EncodeToString(raw[:])
	abs, err := s.abs(rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, body, 0o640); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func (s *Store) Open(rel string) (*os.File, int64, error) {
	abs, err := s.abs(rel)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, 0, fmt.Errorf("attachment %s is not a regular file", rel)
	}
	return f, info.Size(), nil
}

func (s *Store) Remove(rel string) error {
	abs, err := s.abs(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RemoveOwner drops the whole directory of one problem or ticket. It is called
// when the owner itself is deleted; rows are removed by the caller.
func (s *Store) RemoveOwner(kind string, ownerID int64) error {
	return os.RemoveAll(filepath.Join(s.root, filepath.FromSlash(ownerDir(kind, ownerID))))
}

func (s *Store) abs(rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(rel, "/")))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe attachment path %q", rel)
	}
	return filepath.Join(s.root, clean), nil
}
