// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ErrBadArchiveName is what the Archiver double answers for a name a
// device-layout archive may not hold.
var ErrBadArchiveName = errors.New("porttest: bad archive entry name")

// Archiver is the .rmdoc container in memory: Pack encodes the files as
// JSON, WriteArchive does the same with files Files holds and stores the
// result there, and OpenArchive decodes what Files holds at a path, so an
// export opens again. Fail makes CheckName, Pack, WriteArchive, OpenArchive
// or Read fail.
type Archiver struct {
	Files *MemFiles
	mu    sync.Mutex
	Fail  map[string]error
}

// NewArchiver returns an archiver over files.
func NewArchiver(files *MemFiles) *Archiver {
	return &Archiver{Files: files, Fail: map[string]error{}}
}

var (
	_ port.ArchiveReader = (*Archiver)(nil)
	_ port.ArchiveWriter = (*Archiver)(nil)
)

func (a *Archiver) fail(method string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Fail[method]
}

// CheckName implements port.ArchiveWriter: one or two components, none
// empty or hidden, no backslash, nothing absolute.
func (a *Archiver) CheckName(name string) error {
	if err := a.fail("CheckName"); err != nil {
		return err
	}
	parts := strings.Split(name, "/")
	if name == "" || len(parts) > 2 || strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("%w: %q", ErrBadArchiveName, name)
	}
	for _, p := range parts {
		if p == "" || strings.HasPrefix(p, ".") {
			return fmt.Errorf("%w: %q", ErrBadArchiveName, name)
		}
	}
	return nil
}

// WriteArchive implements port.ArchiveWriter: each source is read from
// Files, and the archive is stored there at path.
func (a *Archiver) WriteArchive(path string, sources []port.ArchiveSource) error {
	if err := a.fail("WriteArchive"); err != nil {
		return err
	}
	files := make([]domain.ArchiveFile, len(sources))
	for i, s := range sources {
		data, ok := a.Files.File(s.Path)
		if !ok {
			return fmt.Errorf("%s: %w", s.Path, fs.ErrNotExist)
		}
		files[i] = domain.ArchiveFile{Name: s.Name, Data: data}
	}
	data, err := a.Pack(files)
	if err != nil {
		return err
	}
	a.Files.Write(path, data)
	return nil
}

// Pack encodes files as an archive this double opens, for test setup.
func (a *Archiver) Pack(files []domain.ArchiveFile) ([]byte, error) {
	if err := a.fail("Pack"); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("porttest: an archive needs a file")
	}
	seen := map[string]bool{}
	for _, f := range files {
		if err := a.CheckName(f.Name); err != nil {
			return nil, err
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("%w: %q repeats", ErrBadArchiveName, f.Name)
		}
		seen[f.Name] = true
	}
	return json.Marshal(files)
}

// OpenArchive implements port.ArchiveReader.
func (a *Archiver) OpenArchive(path string) (port.Archive, error) {
	if err := a.fail("OpenArchive"); err != nil {
		return nil, err
	}
	data, ok := a.Files.File(path)
	if !ok {
		return nil, fmt.Errorf("open %s: %w", path, fs.ErrNotExist)
	}
	var files []domain.ArchiveFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("open %s: not an archive: %w", path, err)
	}
	return &memArchive{owner: a, files: files}, nil
}

type memArchive struct {
	owner *Archiver
	files []domain.ArchiveFile
}

func entryOf(f domain.ArchiveFile) domain.ArchiveEntry {
	id := f.Name
	if i := strings.IndexAny(id, "./"); i >= 0 {
		id = id[:i]
	}
	return domain.ArchiveEntry{Name: f.Name, Size: int64(len(f.Data)), DocumentID: domain.ItemID(id)}
}

func (m *memArchive) Entries() []domain.ArchiveEntry {
	out := make([]domain.ArchiveEntry, len(m.files))
	for i, f := range m.files {
		out[i] = entryOf(f)
	}
	return out
}

func (m *memArchive) Read(keep func(domain.ArchiveEntry) bool, body func(domain.ArchiveEntry, []byte) error) error {
	if err := m.owner.fail("Read"); err != nil {
		return err
	}
	for _, f := range m.files {
		e := entryOf(f)
		if keep != nil && !keep(e) {
			continue
		}
		if err := body(e, f.Data); err != nil {
			return err
		}
	}
	return nil
}

func (m *memArchive) Close() error { return nil }
