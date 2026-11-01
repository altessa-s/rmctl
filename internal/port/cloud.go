// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"errors"
	"io"

	"github.com/altessa-s/rmctl/internal/domain"
)

// ErrRootConflict means the root moved between reading it and writing it.
// The writer reads the root again and replays its change.
var ErrRootConflict = errors.New("the library changed while it was being written")

// ErrNotRegistered means no device token is stored: run register first.
var ErrNotRegistered = errors.New("not registered: run rmctl register <code> first")

// RootState is the root index as the cloud holds it. A library with no root
// yet is Hash "" at generation 0, with no records.
type RootState struct {
	Hash       string
	Generation int64
	Records    []domain.Record
}

// Progress reports bytes done of a known total; total is -1 when unknown.
type Progress func(done, total int64)

// RootStore reads and writes the root index.
type RootStore interface {
	Root(ctx context.Context) (RootState, error)
	// CommitRoot writes records as the new root on top of base and returns
	// the root it became. It returns ErrRootConflict when the root is no
	// longer base.
	CommitRoot(ctx context.Context, base RootState, records []domain.Record) (RootState, error)
}

// DocumentStore reads and writes the index of one item.
type DocumentStore interface {
	// Files is the index of rec, in index order.
	Files(ctx context.Context, rec domain.Record) ([]domain.File, error)
	// PutIndex stores the index of id listing files and returns its root record.
	PutIndex(ctx context.Context, id domain.ItemID, files []domain.File) (domain.Record, error)
}

// IndexProber tells which hash a root record carries.
type IndexProber interface {
	// IndexByFile is true when rec.Hash is the hash of the index file's own
	// bytes, false when it is the node hash.
	IndexByFile(ctx context.Context, rec domain.Record) (bool, error)
}

// BlobStore moves file contents. Reads verify the hash they were asked for.
type BlobStore interface {
	Read(ctx context.Context, f domain.File) ([]byte, error)
	// Download streams f into w, verifying its hash.
	Download(ctx context.Context, f domain.File, w io.Writer, progress Progress) error
	// Upload stores body under name and returns the file it became.
	Upload(ctx context.Context, name string, body []byte) (domain.File, error)
	// UploadFile stores the local file at path under name.
	UploadFile(ctx context.Context, path, name string, progress Progress) (domain.File, error)
	// ReadRaw reads a blob by hash without verifying it, for diagnostics.
	ReadRaw(ctx context.Context, hash, name string) ([]byte, error)
	// Missing reports which of hashes the cloud does not hold.
	Missing(ctx context.Context, hashes []string) ([]string, error)
}

// Plan is what the account's token says about its subscription.
type Plan struct {
	Level     string
	SyncScope string
}

// Registry is the device registration of this machine.
type Registry interface {
	IsRegistered(ctx context.Context) (bool, error)
	Register(ctx context.Context, code string) error
	// Forget drops the stored tokens; Revoke first removes the device from
	// the account at the cloud.
	Forget(ctx context.Context) error
	Revoke(ctx context.Context) error
	Plan(ctx context.Context) (Plan, error)
}

// EventKind distinguishes a socket announcement from a library event.
type EventKind int

// The kinds of a notification event.
const (
	EventConnected EventKind = iota + 1
	EventDropped
	EventLibrary
	// EventUnreadable is a frame that did not parse; Frame holds it whole.
	EventUnreadable
)

// Event is one notification.
type Event struct {
	Kind EventKind
	// Name is the cloud's event name, such as SyncComplete.
	Name string
	// SyncComplete reports a SyncComplete event: the library changed.
	SyncComplete   bool
	DocumentID     domain.ItemID
	VisibleName    string
	FromThisDevice bool
	// Frame is the socket frame as it arrived, for a library event and for
	// an unreadable frame.
	Frame []byte
	Err   error
}

// Notifier delivers notifications until the context ends.
type Notifier interface {
	Listen(ctx context.Context, on func(Event)) error
}
