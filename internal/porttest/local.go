// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/altessa-s/go-atlas-core/time/clock"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ErrNoFile is what MemFiles returns for a path it does not hold.
var ErrNoFile = errors.New("porttest: no such file")

// ErrIsDir is what MemFiles.Produce returns for a directory.
var ErrIsDir = errors.New("porttest: path is a directory")

// MemFiles is a file system in memory. Paths are cleaned with filepath.
// Fail makes the named method (Produce, Stat, ModTime, SetModTime, Open,
// ReadHead, ReadTail, MkdirAll, Walk, Remove, Rename, Stage) fail;
// "Unstage" makes the removal Stage returns fail, after it removed
// everything. A file written has the zero modification time until
// SetModTime sets one.
type MemFiles struct {
	mu     sync.Mutex
	files  map[string][]byte
	mtimes map[string]time.Time
	dirs   map[string]bool
	stages int
	Fail   map[string]error
}

// NewMemFiles returns an empty file system.
func NewMemFiles() *MemFiles {
	return &MemFiles{files: map[string][]byte{}, mtimes: map[string]time.Time{}, dirs: map[string]bool{}, Fail: map[string]error{}}
}

var _ port.LocalFiles = (*MemFiles)(nil)

// Write stores a file, for test setup; its modification time is zero.
func (m *MemFiles) Write(path string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[filepath.Clean(path)] = slices.Clone(data)
	delete(m.mtimes, filepath.Clean(path))
}

// File returns a stored file.
func (m *MemFiles) File(path string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[filepath.Clean(path)]
	return slices.Clone(b), ok
}

// Paths is every stored file, sorted.
func (m *MemFiles) Paths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Sorted(maps.Keys(m.files))
}

func (m *MemFiles) fail(method string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Fail[method]
}

// Produce implements port.LocalFiles: the file appears only when write succeeds.
func (m *MemFiles) Produce(_ context.Context, path string, write func(io.Writer) error) error {
	if err := m.fail("Produce"); err != nil {
		return err
	}
	path = filepath.Clean(path)
	m.mu.Lock()
	isDir := m.dirs[path]
	m.mu.Unlock()
	if isDir {
		return fmt.Errorf("%w: %s", ErrIsDir, path)
	}
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		return err
	}
	m.Write(path, buf.Bytes())
	return nil
}

// Stat implements port.LocalFiles.
func (m *MemFiles) Stat(path string) (size int64, isDir, exists bool, err error) {
	if err := m.fail("Stat"); err != nil {
		return 0, false, false, err
	}
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dirs[path] {
		return 0, true, true, nil
	}
	b, ok := m.files[path]
	return int64(len(b)), false, ok, nil
}

// ModTime implements port.LocalFiles.
func (m *MemFiles) ModTime(path string) (time.Time, bool, error) {
	if err := m.fail("ModTime"); err != nil {
		return time.Time{}, false, err
	}
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[path]; !ok {
		return time.Time{}, false, nil
	}
	return m.mtimes[path], true, nil
}

// SetModTime implements port.LocalFiles.
func (m *MemFiles) SetModTime(path string, mtime time.Time) error {
	if err := m.fail("SetModTime"); err != nil {
		return err
	}
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[path]; !ok {
		return ErrNoFile
	}
	m.mtimes[path] = mtime
	return nil
}

// Open implements port.LocalFiles: the file as it is when opened.
func (m *MemFiles) Open(path string) (io.ReadCloser, error) {
	if err := m.fail("Open"); err != nil {
		return nil, err
	}
	b, ok := m.File(path)
	if !ok {
		return nil, ErrNoFile
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// ReadHead implements port.LocalFiles.
func (m *MemFiles) ReadHead(path string, n int) ([]byte, error) {
	if err := m.fail("ReadHead"); err != nil {
		return nil, err
	}
	b, ok := m.File(path)
	if !ok {
		return nil, ErrNoFile
	}
	return b[:min(n, len(b))], nil
}

// ReadTail implements port.LocalFiles.
func (m *MemFiles) ReadTail(path string, n int) ([]byte, error) {
	if err := m.fail("ReadTail"); err != nil {
		return nil, err
	}
	b, ok := m.File(path)
	if !ok {
		return nil, ErrNoFile
	}
	return b[max(len(b)-n, 0):], nil
}

// MkdirAll implements port.LocalFiles.
func (m *MemFiles) MkdirAll(path string) error {
	if err := m.fail("MkdirAll"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		m.dirs[p] = true
		if filepath.Dir(p) == p {
			return nil
		}
	}
}

// Walk implements port.LocalFiles.
func (m *MemFiles) Walk(dir string) ([]string, error) {
	if err := m.fail("Walk"); err != nil {
		return nil, err
	}
	dir = filepath.Clean(dir)
	var out []string
	for _, p := range m.Paths() {
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		out = append(out, rel)
	}
	return out, nil
}

// Remove implements port.LocalFiles.
func (m *MemFiles) Remove(path string) error {
	if err := m.fail("Remove"); err != nil {
		return err
	}
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[path]; !ok {
		return ErrNoFile
	}
	delete(m.files, path)
	delete(m.mtimes, path)
	return nil
}

// Rename implements port.LocalFiles.
func (m *MemFiles) Rename(from, to string) error {
	if err := m.fail("Rename"); err != nil {
		return err
	}
	from, to = filepath.Clean(from), filepath.Clean(to)
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[from]
	if !ok {
		return ErrNoFile
	}
	delete(m.files, from)
	m.files[to] = b
	if t, ok := m.mtimes[from]; ok {
		m.mtimes[to] = t
		delete(m.mtimes, from)
	} else {
		delete(m.mtimes, to)
	}
	return nil
}

// Stage implements port.LocalFiles: a directory stage-<n> at the top, which
// remove deletes with every file below it.
func (m *MemFiles) Stage() (string, func() error, error) {
	if err := m.fail("Stage"); err != nil {
		return "", nil, err
	}
	m.mu.Lock()
	m.stages++
	dir := fmt.Sprintf("stage-%d", m.stages)
	m.dirs[dir] = true
	m.mu.Unlock()
	return dir, func() error {
		m.mu.Lock()
		for p := range m.files {
			if strings.HasPrefix(p, dir+string(filepath.Separator)) {
				delete(m.files, p)
			}
		}
		delete(m.dirs, dir)
		m.mu.Unlock()
		return m.fail("Unstage")
	}, nil
}

// IDs hands out predictable identifiers: prefix followed by a counter,
// shaped like a lowercase UUID.
type IDs struct {
	mu     sync.Mutex
	prefix string
	n      int
}

// NewIDs returns identifiers starting with prefix (at most 8 characters).
func NewIDs(prefix string) *IDs { return &IDs{prefix: prefix} }

var _ port.IDs = (*IDs)(nil)

// NewID implements port.IDs.
func (i *IDs) NewID() domain.ItemID {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.n++
	head := (i.prefix + "00000000")[:8]
	return domain.ItemID(fmt.Sprintf("%s-0000-4000-8000-%012d", head, i.n))
}

// NewClock is a manually driven clock reading now.
func NewClock(now time.Time) *clock.Fake { return clock.NewFake(now) }

// MemCache is a BlobCache in memory that counts its hits.
type MemCache struct {
	mu   sync.Mutex
	data map[string][]byte
	Hits int
}

// NewMemCache returns an empty cache.
func NewMemCache() *MemCache { return &MemCache{data: map[string][]byte{}} }

var _ port.BlobCache = (*MemCache)(nil)

// Get implements port.BlobCache.
func (c *MemCache) Get(hash string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.data[hash]
	if ok {
		c.Hits++
	}
	return slices.Clone(b), ok
}

// Put implements port.BlobCache.
func (c *MemCache) Put(hash string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[hash] = slices.Clone(data)
}

// PDF is a PDFInspector answering from a table of page counts. A path
// missing from Counts is an error.
type PDF struct {
	Counts map[string]int
	Err    error
}

var _ port.PDFInspector = PDF{}

// ErrNotAPDF is what PDF returns for a path it has no count for.
var ErrNotAPDF = errors.New("porttest: not a PDF")

// Pages implements port.PDFInspector.
func (p PDF) Pages(path string) (int, error) {
	if p.Err != nil {
		return 0, p.Err
	}
	n, ok := p.Counts[path]
	if !ok {
		return 0, ErrNotAPDF
	}
	return n, nil
}
