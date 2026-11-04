// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// OfflineBlobs is the blob store of an offline run: every read the blob
// cache could not answer, and every write, is an error naming the file.
type OfflineBlobs struct{}

var _ port.BlobStore = OfflineBlobs{}

// Read implements port.BlobStore.
func (OfflineBlobs) Read(_ context.Context, f domain.File) ([]byte, error) {
	return nil, offline(f.Name)
}

// Download implements port.BlobStore.
func (OfflineBlobs) Download(_ context.Context, f domain.File, _ io.Writer, _ port.Progress) error {
	return offline(f.Name)
}

// Upload implements port.BlobStore.
func (OfflineBlobs) Upload(_ context.Context, name string, _ []byte) (domain.File, error) {
	return domain.File{}, offline(name)
}

// UploadFile implements port.BlobStore.
func (OfflineBlobs) UploadFile(_ context.Context, _, name string, _ port.Progress) (domain.File, error) {
	return domain.File{}, offline(name)
}

// ReadRaw implements port.BlobStore.
func (OfflineBlobs) ReadRaw(_ context.Context, _, name string) ([]byte, error) {
	return nil, offline(name)
}

// Missing implements port.BlobStore.
func (OfflineBlobs) Missing(context.Context, []string) ([]string, error) {
	return nil, offline("the list of blobs")
}

func offline(name string) error {
	return &offlineError{name: name}
}

type offlineError struct{ name string }

func (e *offlineError) Error() string {
	return e.name + " is not in the cache: " + port.ErrOffline.Error()
}

func (e *offlineError) Unwrap() error { return port.ErrOffline }

// BlobFiles are the blobs of the cache, whole or streamed; blobcache's
// Cache implements it.
type BlobFiles interface {
	port.BlobCache
	// Open is the blob of hash for reading, with its size; the reader fails
	// at its end when the bytes are not the hash's.
	Open(hash string) (io.ReadCloser, int64, bool)
}

// Blobs serves the reads of the blob store from the cache when it holds
// the file, and keeps what a write uploads, so the read after a write
// finds the .metadata and .content it just sent instead of fetching them
// back. A file the cache holds, as sync leaves them, is never downloaded
// again: it is addressed by its hash. A download is not kept: a file read
// once, such as an original, would push out what every read uses. Over
// [OfflineBlobs] it is the blob store of an offline run.
type Blobs struct {
	port.BlobStore
	cache BlobFiles
}

var _ port.BlobStore = (*Blobs)(nil)

// NewBlobs decorates blobs with cache.
func NewBlobs(blobs port.BlobStore, cache BlobFiles) *Blobs {
	if blobs == nil || cache == nil {
		panic("indexcache: a blob store and a cache are required")
	}
	return &Blobs{BlobStore: blobs, cache: cache}
}

// Read implements port.BlobStore.
func (b *Blobs) Read(ctx context.Context, f domain.File) ([]byte, error) {
	if data, ok := b.cache.Get(f.Hash); ok {
		return data, nil
	}
	return b.BlobStore.Read(ctx, f)
}

// copyBuffer is how much of a cached file a download copies at a time.
const copyBuffer = 256 << 10

// Download implements port.BlobStore: the cached file when there is one,
// streamed and checked against its hash, else the store's.
func (b *Blobs) Download(ctx context.Context, f domain.File, w io.Writer, progress port.Progress) error {
	r, size, ok := b.cache.Open(f.Hash)
	if !ok {
		return b.BlobStore.Download(ctx, f, w, progress)
	}
	defer func() { _ = r.Close() }()
	var done int64
	buf := make([]byte, copyBuffer)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, size)
			}
		}
		switch {
		case errors.Is(rerr, io.EOF):
			return nil
		case rerr != nil:
			return fmt.Errorf("%s in the cache: %w", f.Name, rerr)
		}
	}
}

// Upload implements port.BlobStore.
func (b *Blobs) Upload(ctx context.Context, name string, body []byte) (domain.File, error) {
	f, err := b.BlobStore.Upload(ctx, name, body)
	if err == nil {
		b.cache.Put(f.Hash, body)
	}
	return f, err
}
