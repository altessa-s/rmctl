// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud

import (
	"context"
	"errors"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/domain"
)

// The reads below split Root and Files into their requests, for the index
// cache: the root pointer alone, and an index with the bytes it came as, so
// the cache can keep what the cloud sent and check it again when it reads
// it back.

// Pointer reads the root pointer, without its index. A library with no root
// yet is "" at generation 0.
func (c *Cloud) Pointer(ctx context.Context) (hash string, generation int64, err error) {
	root, err := c.client.Root(ctx)
	if errors.Is(err, rmcloud.ErrNoRootYet) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, mapErr(err)
	}
	return root.Hash, root.Generation, nil
}

// RootIndex reads the root index hash names. raw is the index as stored when
// hash is the hash of its bytes, and nil when it is a node hash.
func (c *Cloud) RootIndex(ctx context.Context, hash string) (raw []byte, records []domain.Record, err error) {
	raw, index, err := c.index(ctx, hash, rootName)
	if err != nil {
		return nil, nil, err
	}
	return raw, recordsOf(index), nil
}

// Index reads the index of rec, as [Cloud.RootIndex] does the root's.
func (c *Cloud) Index(ctx context.Context, rec domain.Record) (raw []byte, files []domain.File, err error) {
	raw, index, err := c.index(ctx, rec.Hash, rmcloud.IndexName(string(rec.ID)))
	if err != nil {
		return nil, nil, err
	}
	return raw, filesOf(index), nil
}

// DecodeRoot parses a root index held to hash.
func (c *Cloud) DecodeRoot(raw []byte, hash string) ([]domain.Record, error) {
	index, err := rmcloud.DecodeIndex(raw, hash)
	if err != nil {
		return nil, mapErr(err)
	}
	return recordsOf(index), nil
}

// DecodeFiles parses a document index held to hash.
func (c *Cloud) DecodeFiles(raw []byte, hash string) ([]domain.File, error) {
	index, err := rmcloud.DecodeIndex(raw, hash)
	if err != nil {
		return nil, mapErr(err)
	}
	return filesOf(index), nil
}

// index reads the index hash addresses: as a blob checked against hash, so
// its bytes can be kept, or, when hash is a node hash and the blob check
// fails, through the transport's index read, which accepts either.
func (c *Cloud) index(ctx context.Context, hash, name string) ([]byte, rmcloud.Index, error) {
	raw, err := c.client.Blob(ctx, hash, name)
	if rmcloud.IsKind(err, rmcloud.KindHashMismatch) {
		index, ierr := c.client.Index(ctx, hash, name)
		return nil, index, mapErr(ierr)
	}
	if err != nil {
		return nil, rmcloud.Index{}, mapErr(err)
	}
	index, err := rmcloud.DecodeIndex(raw, hash)
	if err != nil {
		return nil, rmcloud.Index{}, mapErr(err)
	}
	return raw, index, nil
}

func recordsOf(index rmcloud.Index) []domain.Record {
	records := make([]domain.Record, 0, len(index.Entries))
	for _, e := range index.Entries {
		records = append(records, domain.Record{ID: domain.ItemID(e.DocumentID), Hash: e.Hash, Subfiles: e.Subfiles, Size: e.Size})
	}
	return records
}

func filesOf(index rmcloud.Index) []domain.File {
	files := make([]domain.File, 0, len(index.Entries))
	for _, e := range index.Entries {
		files = append(files, domain.File{Name: e.DocumentID, Hash: e.Hash, Size: e.Size})
	}
	return files
}
