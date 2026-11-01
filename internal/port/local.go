// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package port

import (
	"context"
	"io"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

// LocalFiles is the machine's file system as the use cases need it.
type LocalFiles interface {
	// Produce writes a file at path through a sibling staging file, which is
	// installed only when write succeeds and removed otherwise. It refuses a
	// path that is a directory before writing anything.
	Produce(ctx context.Context, path string, write func(w io.Writer) error) error
	// Stat reports a local file's size, or exists false.
	Stat(path string) (size int64, isDir, exists bool, err error)
	// Open opens the regular file at path for reading.
	Open(path string) (io.ReadCloser, error)
	// ReadHead reads at most n bytes from the start of path, and ReadTail at
	// most n bytes from its end.
	ReadHead(path string, n int) ([]byte, error)
	ReadTail(path string, n int) ([]byte, error)
	// MkdirAll creates a directory and its parents.
	MkdirAll(path string) error
	// Walk lists the regular files below dir, relative to it, sorted.
	Walk(dir string) ([]string, error)
	// Remove deletes a file.
	Remove(path string) error
	// Rename moves the file from to the path to, replacing a file there.
	Rename(from, to string) error
	// Stage makes a private directory for files a use case keeps on disk
	// for a while, and returns it with the function that removes it and
	// everything in it.
	Stage() (dir string, remove func() error, err error)
}

// PDFInspector reads what an upload needs to know of a PDF.
type PDFInspector interface {
	// Pages counts the pages of the PDF at path; a locked PDF is an error.
	Pages(path string) (int, error)
}

// IDs makes new identifiers: lowercase UUID v4.
type IDs interface {
	NewID() domain.ItemID
}

// Clock is the current time.
type Clock interface {
	Now() time.Time
}

// BlobCache keeps downloaded files by hash, so a second read is free.
type BlobCache interface {
	Get(hash string) ([]byte, bool)
	Put(hash string, data []byte)
}
