// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secretfile

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/altessa-s/rmcloud-go"
)

// The permissions of the token file and its directory.
const (
	fileMode fs.FileMode = 0o600
	dirMode  fs.FileMode = 0o700
)

// ErrExposed means the token file can be read by other users.
var ErrExposed = errors.New("the token file is readable by other users")

// Store keeps the protocol secrets of one profile in one JSON file that only
// its owner can read. It never logs or prints a value.
type Store struct {
	path string
	mu   sync.Mutex
}

var _ rmcloud.SecretStore = (*Store)(nil)

// New returns the store kept at path. The file and its directory are created
// on the first Save.
func New(path string) *Store {
	if path == "" {
		panic("secretfile: a path is required")
	}
	return &Store{path: path}
}

// Path is where the secrets are kept.
func (s *Store) Path() string { return s.path }

// Secret returns the value stored under which.
func (s *Store) Secret(_ context.Context, which rmcloud.SecretName) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read()
	if err != nil {
		return "", false, err
	}
	v, ok := values[string(which)]
	return v, ok, nil
}

// Save stores value under which, replacing the old value.
func (s *Store) Save(_ context.Context, which rmcloud.SecretName, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read()
	if err != nil {
		return err
	}
	values[string(which)] = value
	return s.write(values)
}

// Forget removes which; removing an absent name is success. The file is
// removed when nothing is left in it.
func (s *Store) Forget(_ context.Context, which rmcloud.SecretName) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	values, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := values[string(which)]; !ok {
		return nil
	}
	delete(values, string(which))
	if len(values) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove the token file: %w", err)
		}
		return nil
	}
	return s.write(values)
}

func (s *Store) read() (map[string]string, error) {
	info, err := os.Stat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("the token file %s is not a regular file", s.path)
	}
	if exposed(info.Mode()) {
		return nil, fmt.Errorf("%w: run chmod 600 %s", ErrExposed, s.path)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read the token file: %w", err)
	}
	values := map[string]string{}
	if err := json.Unmarshal(data, &values); err != nil {
		// The decoder's message may quote the file; it holds tokens.
		return nil, fmt.Errorf("the token file %s is not a JSON object of strings", s.path)
	}
	if values == nil {
		// A file of null decodes to no map; it holds no secrets.
		values = map[string]string{}
	}
	return values, nil
}

// write replaces the file through a sibling temporary file, so a crash
// leaves either the old file or the new one.
func (s *Store) write(values map[string]string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create the token directory: %w", err)
	}
	data, err := json.Marshal(values, json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("encode the token file: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.path)+".*.part")
	if err != nil {
		return fmt.Errorf("write the token file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	// Each step is attempted after a failed one only to collect its error;
	// the file is removed either way.
	_, werr := tmp.Write(data)
	if err := errors.Join(tmp.Chmod(fileMode), werr, tmp.Sync(), tmp.Close()); err != nil {
		return fmt.Errorf("write the token file: %w", err)
	}
	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("install the token file: %w", err)
	}
	return nil
}

// exposed reports group or other permission bits. Windows has no such bits
// in os.FileMode, so the check is skipped there.
func exposed(mode fs.FileMode) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return mode.Perm()&0o077 != 0
}
