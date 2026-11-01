// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// DefaultJobs is how many files a mirror downloads at once.
const DefaultJobs = 4

// Service implements port.Fetcher.
type Service struct {
	lib      *library.Library
	blobs    port.BlobStore
	files    port.LocalFiles
	archives port.ArchiveWriter
	clock    port.Clock
}

var _ port.Fetcher = (*Service)(nil)

// New returns the fetch use case. A nil dependency panics.
func New(lib *library.Library, blobs port.BlobStore, files port.LocalFiles, archives port.ArchiveWriter, clk port.Clock) *Service {
	switch {
	case lib == nil:
		panic("fetch: Library is required")
	case blobs == nil:
		panic("fetch: BlobStore is required")
	case files == nil:
		panic("fetch: LocalFiles is required")
	case archives == nil:
		panic("fetch: ArchiveWriter is required")
	}
	if clk == nil {
		panic("fetch: Clock is required")
	}
	return &Service{lib: lib, blobs: blobs, files: files, archives: archives, clock: clk}
}

// Get implements port.Fetcher.
func (s *Service) Get(ctx context.Context, query, dest string, progress port.Progress) (string, error) {
	it, err := s.document(ctx, query)
	if err != nil {
		return "", err
	}
	original, ok := it.Original()
	if !ok {
		return "", fmt.Errorf("%s: %w", it.Name, domain.ErrNoOriginal)
	}
	path, err := s.destination(dest, FileName(it.Name, original.Extension()))
	if err != nil {
		return "", err
	}
	err = s.files.Produce(ctx, path, func(w io.Writer) error {
		return s.blobs.Download(ctx, original, w, progress)
	})
	return path, err
}

// Export implements port.Fetcher: the documents the queries name go into
// one archive the way the tablet writes one, each document's files under
// their record names, in index order. Nothing is fetched until every name
// has passed the archive's rules. Each file is downloaded to a staging
// directory and the archive is written from there, so no file and no
// archive is held in memory whole; the archive reaches dest only when it is
// complete.
func (s *Service) Export(ctx context.Context, queries []string, dest string) (port.ExportReport, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return port.ExportReport{}, err
	}
	docs, name, err := ExportPlan(tree, queries)
	if err != nil {
		return port.ExportReport{}, err
	}
	seen := map[string]bool{}
	for _, it := range docs {
		for _, f := range it.Files {
			if cerr := s.archives.CheckName(f.Name); cerr != nil {
				return port.ExportReport{}, fmt.Errorf("%s: %w", it.Name, cerr)
			}
			if seen[f.Name] {
				return port.ExportReport{}, fmt.Errorf("%s: %q is in the archive twice: %w", it.Name, f.Name, ErrRepeatedEntry)
			}
			seen[f.Name] = true
		}
	}
	path, err := s.destination(dest, SafeName(name)+".rmdoc")
	if err != nil {
		return port.ExportReport{}, err
	}
	// An archive in place is reported even when its staging directory
	// could not be removed.
	files, err := s.pack(ctx, docs, path)
	if err != nil && files == 0 {
		return port.ExportReport{}, err
	}
	return port.ExportReport{Path: path, Documents: len(docs), Files: files}, err
}

// pack writes the files of docs into the archive at path and returns how
// many it holds, also when only the removal of the staging directory
// failed. Each file is downloaded to a staging directory and the
// archive is written from there, so no file and no archive is held in
// memory whole; the archive reaches path only when it is complete.
func (s *Service) pack(ctx context.Context, docs []domain.Item, path string) (n int, err error) {
	stage, remove, err := s.files.Stage()
	if err != nil {
		return 0, err
	}
	defer func() {
		if rerr := remove(); rerr != nil {
			err = errors.Join(err, fmt.Errorf("remove the staging directory %s: %w", stage, rerr))
		}
	}()
	var sources []port.ArchiveSource
	for _, it := range docs {
		for _, f := range it.Files {
			// The record name may hold a slash: the file is named by its count.
			local := filepath.Join(stage, strconv.Itoa(len(sources)))
			derr := s.files.Produce(ctx, local, func(w io.Writer) error { return s.blobs.Download(ctx, f, w, nil) })
			if derr != nil {
				return 0, fmt.Errorf("download %s: %w", f.Name, derr)
			}
			sources = append(sources, port.ArchiveSource{Name: f.Name, Path: local})
		}
	}
	archive := filepath.Join(stage, "archive.rmdoc")
	if err = s.archives.WriteArchive(archive, sources); err != nil {
		return 0, err
	}
	err = s.files.Produce(ctx, path, func(w io.Writer) error {
		r, oerr := s.files.Open(archive)
		if oerr != nil {
			return oerr
		}
		defer func() { _ = r.Close() }()
		_, cerr := io.Copy(w, r)
		return cerr
	})
	if err != nil {
		return 0, err
	}
	return len(sources), nil
}

// Mirror implements port.Fetcher.
func (s *Service) Mirror(ctx context.Context, query, dir string, opts port.MirrorOptions) (port.MirrorReport, error) {
	p, err := s.plan(ctx, query, dir)
	if err != nil {
		return port.MirrorReport{}, err
	}
	report := port.MirrorReport{Skipped: p.Skipped}
	for _, d := range p.Dirs {
		if err = s.files.MkdirAll(d); err != nil {
			return report, err
		}
	}
	var written, skipped atomic.Int64
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	err = concurrency.Process(ctx, p.Files, func(ctx context.Context, pf PlannedFile) error {
		wrote, ferr := s.mirrorFile(ctx, pf, opts.Incremental)
		if wrote {
			written.Add(1)
		} else if ferr == nil {
			skipped.Add(1)
		}
		return ferr
	}, concurrency.WithConcurrency[PlannedFile](jobs), concurrency.WithStopOnError[PlannedFile]())
	report.Written = int(written.Load())
	report.Skipped += int(skipped.Load())
	if err != nil {
		return report, err
	}
	if opts.Delete {
		report.Deleted, err = s.prune(dir, p)
	}
	return report, err
}

// plan is the mirror plan of the folder or document query names.
func (s *Service) plan(ctx context.Context, query, dir string) (MirrorPlan, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return MirrorPlan{}, err
	}
	if query == "" || query == "/" {
		return Plan(tree, domain.RootID, dir), nil
	}
	it, found, err := tree.Resolve(query, false)
	switch {
	case err != nil:
		return MirrorPlan{}, err
	case !found || it.Kind.IsFolder():
		return Plan(tree, it.ID, dir), nil
	default:
		return planDocument(it, dir), nil
	}
}

// mirrorFile downloads one planned file. wrote false with no error is a
// skip: incremental found the file there with the size the cloud names.
func (s *Service) mirrorFile(ctx context.Context, pf PlannedFile, incremental bool) (bool, error) {
	if incremental {
		size, isDir, exists, err := s.files.Stat(pf.Path)
		if err != nil {
			return false, err
		}
		if exists && !isDir && size == pf.File.Size {
			return false, nil
		}
	}
	err := s.files.Produce(ctx, pf.Path, func(w io.Writer) error {
		return s.blobs.Download(ctx, pf.File, w, nil)
	})
	if err != nil {
		return false, fmt.Errorf("download %s: %w", pf.Path, err)
	}
	return true, nil
}

// prune removes the files below dir that the plan does not produce.
func (s *Service) prune(dir string, p MirrorPlan) (int, error) {
	keep := make(map[string]bool, len(p.Files))
	for _, f := range p.Files {
		keep[filepath.Clean(f.Path)] = true
	}
	local, err := s.files.Walk(dir)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, rel := range local {
		path := filepath.Join(dir, rel)
		if keep[filepath.Clean(path)] {
			continue
		}
		if err := s.files.Remove(path); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// document resolves a query to a document; trashed documents count.
func (s *Service) document(ctx context.Context, query string) (domain.Item, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	it, found, err := tree.Resolve(query, true)
	if err != nil {
		return domain.Item{}, err
	}
	if !found {
		return domain.Item{}, domain.ErrNotFound
	}
	if it.Kind.IsFolder() {
		return domain.Item{}, fmt.Errorf("%s is a folder: %w", it.Name, domain.ErrNoOriginal)
	}
	return it, nil
}

// destination is dest, the name inside dest when dest is a directory, or
// the name in the working directory when dest is empty.
func (s *Service) destination(dest, name string) (string, error) {
	if dest == "" {
		return name, nil
	}
	_, isDir, exists, err := s.files.Stat(dest)
	if err != nil {
		return "", err
	}
	if (exists && isDir) || strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, string(filepath.Separator)) {
		return filepath.Join(dest, name), nil
	}
	return dest, nil
}
