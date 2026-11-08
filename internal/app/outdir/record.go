// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package outdir

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"maps"
	"path/filepath"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// recordVersion is the layout of a record; another is no record.
const recordVersion = 1

// maxRecord bounds the record read: far above any library.
const maxRecord = 64 << 20

// record is what a directory written keeps of each file in it, by its
// path relative to the directory, with slashes.
type record struct {
	Version   int              `json:"v"`
	Documents map[string]Entry `json:"documents"`
}

// Entry is one file of a directory written: the document it was written
// from, the document's modification time then, and the options it was
// written with.
type Entry struct {
	ID       domain.ItemID `json:"id"`
	Modified time.Time     `json:"modified"`
	Options  string        `json:"options"`
	// Empty records a document that gave nothing to write: no file of it
	// stands at the path.
	Empty bool `json:"empty,omitzero"`
}

// EntryOf is what a record keeps of a file written from it with opts.
func EntryOf(it domain.Item, opts string) Entry {
	return Entry{ID: it.ID, Modified: it.Modified, Options: opts}
}

// Matches reports whether e records a file written from it as it is, with
// opts.
func (e Entry) Matches(it domain.Item, opts string) bool {
	return e.ID == it.ID && e.Modified.Equal(it.Modified) && e.Options == opts
}

// Key is the record's name of the file at path under dir.
func Key(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// Read is the record named name in dir: empty when there is none, or it
// cannot be read or decoded, so every document is written.
func Read(files port.LocalFiles, dir, name string) map[string]Entry {
	path := filepath.Join(dir, name)
	if _, isDir, exists, err := files.Stat(path); err != nil || !exists || isDir {
		return map[string]Entry{}
	}
	r, err := files.Open(path)
	if err != nil {
		return map[string]Entry{}
	}
	defer func() { _ = r.Close() }()
	var buf bytes.Buffer
	if _, err = io.Copy(&buf, io.LimitReader(r, maxRecord)); err != nil {
		return map[string]Entry{}
	}
	var m record
	if err = json.Unmarshal(buf.Bytes(), &m); err != nil || m.Version != recordVersion || m.Documents == nil {
		return map[string]Entry{}
	}
	return m.Documents
}

// Write writes the record named name in dir whole, or leaves the one
// there. It is written even when ctx is done: it records files already
// written.
func Write(ctx context.Context, files port.LocalFiles, dir, name string, docs map[string]Entry) error {
	return files.Produce(context.WithoutCancel(ctx), filepath.Join(dir, name), func(w io.Writer) error {
		return json.MarshalWrite(w, record{Version: recordVersion, Documents: docs}, json.Deterministic(true))
	})
}

// Pruned is docs without the entries of plan that do not record its
// document as it is now with opts, and whether it dropped any. Written
// before any file is, it keeps a run that stops halfway honest: a file
// written again under other options is never recorded under the old ones.
func Pruned(docs map[string]Entry, dir string, plan []Planned, opts string) (map[string]Entry, bool) {
	out := maps.Clone(docs)
	dropped := false
	for _, p := range plan {
		key := Key(dir, p.Path)
		if e, ok := out[key]; ok && !e.Matches(p.Document, opts) {
			delete(out, key)
			dropped = true
		}
	}
	return out, dropped
}

// SameTime compares a file's modification time with a document's to the
// second: the coarsest a file system the files go to keeps.
func SameTime(file, document time.Time) bool {
	return file.Truncate(time.Second).Equal(document.Truncate(time.Second))
}
