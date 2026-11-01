// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

// BackupFormat is the version of the manifest a backup writes. A manifest
// of a later version is refused rather than misread.
const BackupFormat = 1

// BackupManifestName is the manifest's file name in a backup directory, and
// BackupRemovedDir the directory that keeps the archives of documents the
// backup no longer covers.
const (
	BackupManifestName = "manifest.json"
	BackupRemovedDir   = "removed"
)

// backupExt is the extension of a backup's archives.
const backupExt = ".rmdoc"

// ErrBackupManifest means a backup manifest does not decode or breaks its
// rules.
var ErrBackupManifest = errors.New("not a backup manifest")

// BackupManifest describes a backup directory: the profile it was taken
// from, when, of which path, and one entry per document archive.
type BackupManifest struct {
	Format    int           `json:"format"`
	Profile   string        `json:"profile"`
	Created   time.Time     `json:"created"`
	Path      string        `json:"path"`
	Documents []BackupEntry `json:"documents"`
}

// BackupEntry is one document of a backup: what it was in the library when
// its archive was written, and the archive.
type BackupEntry struct {
	ID   ItemID `json:"id"`
	Name string `json:"name"`
	// Path is the document's path in the library, "/Folder/Name".
	Path string `json:"path"`
	Kind Kind   `json:"kind"`
	// Hash is the record hash: a document whose hash is unchanged needs no
	// new archive.
	Hash string `json:"hash"`
	// File is the archive's name in the backup directory, and Size its
	// size in bytes.
	File    string `json:"file"`
	Size    int64  `json:"size"`
	Trashed bool   `json:"trashed,omitempty"`
}

// NewBackupEntry is the entry of a document of tree, archived as file of
// size bytes.
func NewBackupEntry(tree *Tree, it Item, file string, size int64) BackupEntry {
	return BackupEntry{
		ID: it.ID, Name: it.Name, Path: tree.Path(it.ID), Kind: it.Kind, Hash: it.Hash,
		File: file, Size: size, Trashed: tree.IsTrashed(it.ID),
	}
}

// Folder is the path of the folder the document was in, "/" for the top
// level.
func (e BackupEntry) Folder() string {
	dir := strings.TrimSuffix(e.Path, "/"+e.Name)
	if dir == e.Path {
		// A path that does not end with the name: trust its last slash.
		dir = path.Dir(e.Path)
	}
	if dir == "" || dir == "." {
		return "/"
	}
	return dir
}

// IsBackupFile reports whether name can be an archive of a backup: one
// path component ending in .rmdoc, not hidden, so no manifest can steer a
// restore out of its directory.
func IsBackupFile(name string) bool {
	return strings.HasSuffix(name, backupExt) && len(name) > len(backupExt) &&
		!strings.HasPrefix(name, ".") && !strings.ContainsAny(name, `/\:`)
}

// ParseBackupManifest decodes a manifest and checks it: a known format,
// archive names that stay in the directory, and each document and archive
// once.
func ParseBackupManifest(data []byte) (BackupManifest, error) {
	var m BackupManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return BackupManifest{}, fmt.Errorf("%w: %w", ErrBackupManifest, err)
	}
	switch {
	case m.Format < 1:
		return BackupManifest{}, fmt.Errorf("%w: no format version", ErrBackupManifest)
	case m.Format > BackupFormat:
		return BackupManifest{}, fmt.Errorf("%w: format %d is newer than this rmctl reads (%d)", ErrBackupManifest, m.Format, BackupFormat)
	}
	ids := map[ItemID]bool{}
	files := map[string]bool{}
	for _, e := range m.Documents {
		switch {
		case e.ID == "":
			return BackupManifest{}, fmt.Errorf("%w: a document has no id", ErrBackupManifest)
		case !IsBackupFile(e.File):
			return BackupManifest{}, fmt.Errorf("%w: %s: archive name %q", ErrBackupManifest, e.ID, e.File)
		case ids[e.ID]:
			return BackupManifest{}, fmt.Errorf("%w: %s is listed twice", ErrBackupManifest, e.ID)
		case files[e.File]:
			return BackupManifest{}, fmt.Errorf("%w: %s is listed twice", ErrBackupManifest, e.File)
		}
		ids[e.ID], files[e.File] = true, true
	}
	return m, nil
}

// Bytes is the manifest as indented JSON, its documents in path order.
func (m BackupManifest) Bytes() []byte {
	m.Documents = slices.Clone(m.Documents)
	slices.SortFunc(m.Documents, func(a, b BackupEntry) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
	if m.Documents == nil {
		m.Documents = []BackupEntry{}
	}
	out, err := json.Marshal(m, jsontext.WithIndent("  "))
	if err != nil {
		// Strings, numbers and a time always encode.
		panic(err)
	}
	return append(out, '\n')
}

// Entry is the manifest's entry for id.
func (m BackupManifest) Entry(id ItemID) (BackupEntry, bool) {
	for _, e := range m.Documents {
		if e.ID == id {
			return e, true
		}
	}
	return BackupEntry{}, false
}

// Select is the entries ids name, each an identifier or a unique prefix of
// one, in manifest order; no ids is every entry.
func (m BackupManifest) Select(ids []string) ([]BackupEntry, error) {
	if len(ids) == 0 {
		return slices.Clone(m.Documents), nil
	}
	chosen := map[ItemID]bool{}
	for _, q := range ids {
		e, err := m.match(q)
		if err != nil {
			return nil, err
		}
		chosen[e.ID] = true
	}
	var out []BackupEntry
	for _, e := range m.Documents {
		if chosen[e.ID] {
			out = append(out, e)
		}
	}
	return out, nil
}

// match is the one entry q names.
func (m BackupManifest) match(q string) (BackupEntry, error) {
	if e, ok := m.Entry(ItemID(q)); ok {
		return e, nil
	}
	var found []BackupEntry
	for _, e := range m.Documents {
		if q != "" && strings.HasPrefix(string(e.ID), q) {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 0:
		return BackupEntry{}, fmt.Errorf("%s: %w in the backup", q, ErrNotFound)
	case 1:
		return found[0], nil
	default:
		items := make([]Item, len(found))
		for i, e := range found {
			items[i] = Item{ID: e.ID, Name: e.Name, Kind: e.Kind}
		}
		return BackupEntry{}, &AmbiguousError{Query: q, Candidates: items}
	}
}
