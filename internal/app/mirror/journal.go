// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// journalVersion is the layout of a journal; another is refused.
const journalVersion = 1

// maxJournal bounds the journal read: far above any folder.
const maxJournal = 64 << 20

// ErrJournal means the journal of a directory cannot be read: the mirror
// does not guess what was in step.
var ErrJournal = errors.New("the mirror's journal cannot be read; move it aside to start the mirror over")

// journal is what a mirrored directory keeps of the last pass: every
// document in step, by identifier, so a document renamed or moved on the
// tablet is followed rather than taken for a new one.
type journal struct {
	Version int `json:"v"`
	// Folder is the cloud folder the directory mirrors, by identifier: a
	// mirror pointed at another folder starts over rather than taking the
	// documents it lacks for deleted.
	Folder  domain.ItemID `json:"folder"`
	Profile string        `json:"profile,omitempty"`
	// LastPass is when the last pass ended, LastError why it failed, and
	// Pending what it left for later.
	LastPass  time.Time               `json:"last_pass,omitzero"`
	LastError string                  `json:"last_error,omitempty"`
	Pending   int                     `json:"pending,omitzero"`
	Items     map[domain.ItemID]entry `json:"items"`
}

// entry is one document in step.
type entry struct {
	// Path is the local file, relative to the directory with slashes; ""
	// is a document created on the tablet, which has only its drawing.
	Path string `json:"path,omitempty"`
	// SHA256, Size and ModTime are the local file's when it was last in
	// step; a file of the same size and time is taken as unchanged.
	SHA256  string    `json:"sha256,omitempty"`
	Size    int64     `json:"size,omitzero"`
	ModTime time.Time `json:"mtime,omitzero"`
	// Name and Parent are the document's in the cloud, and Record its
	// record hash, when it was last in step.
	Name   string        `json:"name"`
	Parent domain.ItemID `json:"parent"`
	Record string        `json:"record"`
	// Content is the document's [domain.DrawnFingerprint] when it was last
	// in step: a change to it is a change of its pages, which an open, a
	// move or a rename on the tablet is not.
	Content string `json:"content,omitempty"`
	// Drawn is the fingerprint of the document last drawn, and Output the
	// drawing, relative to the directory.
	Drawn  string `json:"drawn,omitempty"`
	Output string `json:"output,omitempty"`
	// Detached marks a document deleted on one side whose deletion was not
	// carried to the other: it is followed no more.
	Detached bool `json:"detached,omitzero"`
}

// newJournal is the journal of a directory never mirrored.
func newJournal() *journal {
	return &journal{Version: journalVersion, Items: map[domain.ItemID]entry{}}
}

// journalPath is the journal of dir.
func journalPath(dir string) string { return filepath.Join(dir, domain.MirrorJournalName) }

// readJournal reads the journal of dir; a directory without one has a new
// one, and one that does not decode is ErrJournal.
func readJournal(files port.LocalFiles, dir string) (*journal, error) {
	path := journalPath(dir)
	_, isDir, exists, err := files.Stat(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return newJournal(), nil
	}
	if isDir {
		return nil, fmt.Errorf("%s: %w", path, ErrJournal)
	}
	r, err := files.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	var buf bytes.Buffer
	if _, err = io.Copy(&buf, io.LimitReader(r, maxJournal)); err != nil {
		return nil, err
	}
	j := &journal{}
	if err = json.Unmarshal(buf.Bytes(), j); err != nil || j.Version != journalVersion {
		return nil, fmt.Errorf("%s: %w", path, ErrJournal)
	}
	if j.Items == nil {
		j.Items = map[domain.ItemID]entry{}
	}
	return j, nil
}

// write writes the journal of dir whole, or leaves the one there. It is
// written even when ctx is done: it records what the pass already did.
func (j *journal) write(ctx context.Context, files port.LocalFiles, dir string) error {
	err := files.Produce(context.WithoutCancel(ctx), journalPath(dir), func(w io.Writer) error {
		return json.MarshalWrite(w, j, json.Deterministic(true))
	})
	if err != nil {
		return fmt.Errorf("record the pass in %s: %w", journalPath(dir), err)
	}
	return nil
}

// tracked counts the documents followed, and of them those with a local
// file.
func (j *journal) tracked() (all, local int) {
	for _, e := range j.Items {
		if e.Detached {
			continue
		}
		all++
		if e.Path != "" {
			local++
		}
	}
	return all, local
}
