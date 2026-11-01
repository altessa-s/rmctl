// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package inspect answers protocol questions about the live library for
// rmctl debug. It implements port.Inspector and changes nothing in the
// cloud.
//
// Which hash a root record carries is asked of the cloud through
// port.IndexProber rather than worked out by re-serializing the index: a
// tablet may have written the index in schema 3 or in another order. A blob
// written with -o goes through port.LocalFiles, installed only when whole.
package inspect

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"slices"
	"unicode/utf8"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// missingBatch is how many hashes one check-files call carries.
const missingBatch = 500

// loadJobs is how many items are read at once.
const loadJobs = 8

// Service implements port.Inspector.
type Service struct {
	lib    *library.Library
	roots  port.RootStore
	prober port.IndexProber
	blobs  port.BlobStore
	files  port.LocalFiles
}

var _ port.Inspector = (*Service)(nil)

// New returns the inspect use case. A nil dependency panics.
func New(
	lib *library.Library, roots port.RootStore, prober port.IndexProber, blobs port.BlobStore, files port.LocalFiles,
) *Service {
	switch {
	case lib == nil:
		panic("inspect: Library is required")
	case roots == nil:
		panic("inspect: RootStore is required")
	case prober == nil:
		panic("inspect: IndexProber is required")
	case blobs == nil:
		panic("inspect: BlobStore is required")
	case files == nil:
		panic("inspect: LocalFiles is required")
	}
	return &Service{lib: lib, roots: roots, prober: prober, blobs: blobs, files: files}
}

// Root implements port.Inspector.
func (s *Service) Root(ctx context.Context) (port.RootState, error) { return s.roots.Root(ctx) }

// Blob implements port.Inspector: the bytes as stored, unverified.
func (s *Service) Blob(ctx context.Context, hash, name string) ([]byte, error) {
	return s.blobs.ReadRaw(ctx, hash, name)
}

// SaveBlob implements port.Inspector: the blob as Blob reads it, written to
// dest through a staging file that only a complete write installs.
func (s *Service) SaveBlob(ctx context.Context, hash, name, dest string) (string, error) {
	data, err := s.Blob(ctx, hash, name)
	if err != nil {
		return "", err
	}
	err = s.files.Produce(ctx, dest, func(w io.Writer) error {
		_, werr := w.Write(data)
		return werr
	})
	if err != nil {
		return "", err
	}
	return dest, nil
}

// Files implements port.Inspector. Text is set for files that are UTF-8:
// .metadata, .content, .pagedata and other JSON or text files.
func (s *Service) Files(ctx context.Context, query string) (domain.Item, []port.FileReport, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return domain.Item{}, nil, err
	}
	it, found, err := tree.Resolve(query, true)
	if err != nil {
		return domain.Item{}, nil, err
	}
	if !found {
		return domain.Item{}, nil, domain.ErrNotFound
	}
	reports := make([]port.FileReport, 0, len(it.Files))
	for _, f := range it.Files {
		r := port.FileReport{File: f}
		if textual(f) {
			data, err := s.lib.Read(ctx, f)
			if err != nil {
				return domain.Item{}, nil, err
			}
			if utf8.Valid(data) {
				r.Text = data
			}
		}
		reports = append(reports, r)
	}
	return it, reports, nil
}

func textual(f domain.File) bool {
	switch f.Extension() {
	case "metadata", "content", "pagedata", "json", "txt", "md":
		return true
	default:
		return false
	}
}

// Hashes implements port.Inspector. Which hash a record carries is the
// cloud's to say: the prober reads the index by the record hash, which only
// the hash of the index file's own bytes addresses.
func (s *Service) Hashes(ctx context.Context) ([]port.HashReport, error) {
	state, err := s.roots.Root(ctx)
	if err != nil {
		return nil, err
	}
	return concurrency.ProcessCollect(ctx, state.Records, func(ctx context.Context, rec domain.Record) (port.HashReport, error) {
		byFile, err := s.prober.IndexByFile(ctx, rec)
		if err != nil {
			return port.HashReport{}, err
		}
		return port.HashReport{Record: rec, ByIndexFile: byFile}, nil
	}, concurrency.WithConcurrency[domain.Record](loadJobs), concurrency.WithStopOnError[domain.Record]())
}

// CheckFiles implements port.Inspector: the hashes of the library's files
// that the cloud does not hold.
func (s *Service) CheckFiles(ctx context.Context) ([]string, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var hashes []string
	for _, it := range tree.Items() {
		for _, f := range it.Files {
			if !seen[f.Hash] {
				seen[f.Hash] = true
				hashes = append(hashes, f.Hash)
			}
		}
	}
	var missing []string
	for batch := range slices.Chunk(hashes, missingBatch) {
		got, err := s.blobs.Missing(ctx, batch)
		if err != nil {
			return nil, err
		}
		missing = append(missing, got...)
	}
	return missing, nil
}

// Sweep implements port.Inspector: for every item, the top-level fields of
// its .metadata and .content, counted as "metadata.<field>" and
// "content.<field>". A file that is not a JSON object counts as
// "<kind>.<malformed>".
func (s *Service) Sweep(ctx context.Context) (map[string]int, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, it := range tree.Items() {
		for _, f := range it.Files {
			kind := f.Extension()
			if kind != "metadata" && kind != "content" {
				continue
			}
			data, err := s.lib.Read(ctx, f)
			if err != nil {
				return nil, err
			}
			var obj map[string]jsontext.Value
			if json.Unmarshal(data, &obj) != nil || obj == nil {
				counts[kind+".<malformed>"]++
				continue
			}
			for key := range obj {
				counts[kind+"."+key]++
			}
		}
	}
	return counts, nil
}
