// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache

import (
	"context"
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

// Blobs keeps what a write uploads in the blob cache, so the read after a
// write finds the .metadata and .content it just sent instead of fetching
// them back.
type Blobs struct {
	port.BlobStore
	cache port.BlobCache
}

var _ port.BlobStore = (*Blobs)(nil)

// NewBlobs decorates blobs with cache.
func NewBlobs(blobs port.BlobStore, cache port.BlobCache) *Blobs {
	if blobs == nil || cache == nil {
		panic("indexcache: a blob store and a cache are required")
	}
	return &Blobs{BlobStore: blobs, cache: cache}
}

// Upload implements port.BlobStore.
func (b *Blobs) Upload(ctx context.Context, name string, body []byte) (domain.File, error) {
	f, err := b.BlobStore.Upload(ctx, name, body)
	if err == nil {
		b.cache.Put(f.Hash, body)
	}
	return f, err
}
