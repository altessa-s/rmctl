// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ErrOtherProfile means a backup directory holds the backup of another
// profile.
var ErrOtherProfile = errors.New("the directory holds a backup of another profile; pass --force to back up into it anyway")

// ErrOtherPath means a backup directory holds the backup of another path
// of the library.
var ErrOtherPath = errors.New("back up into another directory, or pass --rescope to bind this one to the new path " +
	"(the archives outside it are moved to removed/, never deleted)")

// ErrNotADirectory means a backup directory is a file.
var ErrNotADirectory = errors.New("not a directory")

// BackupPlan is the documents a backup of the folder or document query
// names takes, in path order, and the path it covers. "" or "/" is the
// whole library. Documents in the trash stay out unless all.
func BackupPlan(tree *domain.Tree, query string, all bool) ([]domain.Item, string, error) {
	folder := domain.RootID
	if query != "" && query != "/" {
		it, found, err := tree.Resolve(query, all)
		switch {
		case err != nil:
			return nil, "", fmt.Errorf("%s: %w", query, err)
		case found && !it.Kind.IsFolder():
			return []domain.Item{it}, tree.Path(it.ID), nil
		case found:
			folder = it.ID
		}
	}
	var docs []domain.Item
	for _, it := range below(tree, folder) {
		if !it.Kind.IsFolder() && (all || !tree.IsTrashed(it.ID)) {
			docs = append(docs, it)
		}
	}
	slices.SortFunc(docs, func(a, b domain.Item) int {
		return cmp.Or(strings.Compare(tree.Path(a.ID), tree.Path(b.ID)), strings.Compare(string(a.ID), string(b.ID)))
	})
	return docs, tree.Path(folder), nil
}

// BackupFile is the archive name of a document in a backup directory.
func BackupFile(id domain.ItemID) string { return SafeName(string(id)) + ".rmdoc" }

// Backup implements port.Fetcher. Every document is packed the way export
// packs one, into an archive of its own that appears only when whole; the
// manifest is written last, so an interrupted backup leaves the previous
// manifest, and the next run writes again what it did not record.
func (s *Service) Backup(ctx context.Context, dir, query string, opts port.BackupOptions) (port.BackupReport, error) {
	report := port.BackupReport{Dir: dir}
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return report, err
	}
	docs, covered, err := BackupPlan(tree, query, opts.All)
	if err != nil {
		return report, err
	}
	old, err := s.manifest(dir, covered, opts)
	if err != nil {
		return report, err
	}
	names, err := s.backupNames(docs)
	if err != nil {
		return report, err
	}
	if err = s.files.MkdirAll(dir); err != nil {
		return report, err
	}
	entries := make([]domain.BackupEntry, len(docs))
	var written, skipped atomic.Int64
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	indexes := make([]int, len(docs))
	for i := range indexes {
		indexes[i] = i
	}
	err = concurrency.Process(ctx, indexes, func(ctx context.Context, i int) error {
		e, wrote, berr := s.backupOne(ctx, tree, docs[i], filepath.Join(dir, names[i]), names[i], old)
		if berr != nil {
			return fmt.Errorf("%s: %w", tree.Path(docs[i].ID), berr)
		}
		entries[i] = e
		if wrote {
			written.Add(1)
		} else {
			skipped.Add(1)
		}
		return nil
	}, concurrency.WithConcurrency[int](jobs), concurrency.WithStopOnError[int]())
	report.Written, report.Skipped = int(written.Load()), int(skipped.Load())
	if err != nil {
		return report, err
	}
	// A re-scope deletes nothing: what the old path held is no longer
	// this backup's to judge.
	rescoped := old.Path != "" && old.Path != covered
	if err = s.setAside(dir, tree, old, entries, opts.Prune && !rescoped, opts.All, &report); err != nil {
		return report, err
	}
	m := domain.BackupManifest{
		Format: domain.BackupFormat, Profile: opts.Profile, Created: s.clock.Now().UTC(), Path: covered, Documents: entries,
	}
	err = s.files.Produce(ctx, filepath.Join(dir, domain.BackupManifestName), func(w io.Writer) error {
		_, werr := w.Write(m.Bytes())
		return werr
	})
	if err != nil {
		return report, err
	}
	report.Documents = len(entries)
	return report, nil
}

// manifest is the manifest dir holds, empty when there is none, checked
// against the profile and the path covered: a directory is bound to the
// path its manifest records.
func (s *Service) manifest(dir, covered string, opts port.BackupOptions) (domain.BackupManifest, error) {
	_, isDir, exists, err := s.files.Stat(dir)
	switch {
	case err != nil:
		return domain.BackupManifest{}, err
	case !exists:
		return domain.BackupManifest{}, nil
	case !isDir:
		return domain.BackupManifest{}, fmt.Errorf("%s: %w", dir, ErrNotADirectory)
	}
	m, found, err := library.ReadManifest(s.files, dir)
	if err != nil || !found {
		return domain.BackupManifest{}, err
	}
	if m.Profile != opts.Profile && !opts.Force {
		return domain.BackupManifest{}, fmt.Errorf("%s holds profile %q: %w", dir, m.Profile, ErrOtherProfile)
	}
	if m.Path != "" && m.Path != covered && !opts.Rescope {
		return domain.BackupManifest{}, fmt.Errorf("%s holds the backup of %s, not %s: %w", dir, m.Path, covered, ErrOtherPath)
	}
	return m, nil
}

// backupNames are the archive names of docs, after every file of each has
// passed the archive's rules.
func (s *Service) backupNames(docs []domain.Item) ([]string, error) {
	names := make([]string, len(docs))
	seen := map[string]domain.ItemID{}
	for i, it := range docs {
		for _, f := range it.Files {
			if err := s.archives.CheckName(f.Name); err != nil {
				return nil, fmt.Errorf("%s: %w", it.Name, err)
			}
		}
		name := BackupFile(it.ID)
		if other, ok := seen[name]; ok {
			return nil, fmt.Errorf("%s and %s take one archive name %q: %w", other, it.ID, name, ErrRepeatedEntry)
		}
		seen[name], names[i] = it.ID, name
	}
	return names, nil
}

// backupOne writes the archive of one document unless the manifest records
// its record hash and the archive is there with the size it recorded.
func (s *Service) backupOne(
	ctx context.Context, tree *domain.Tree, it domain.Item, path, name string, old domain.BackupManifest,
) (domain.BackupEntry, bool, error) {
	if e, ok := old.Entry(it.ID); ok && e.Hash == it.Hash && e.File == name {
		size, isDir, exists, err := s.files.Stat(path)
		if err != nil {
			return domain.BackupEntry{}, false, err
		}
		if exists && !isDir && size == e.Size {
			return domain.NewBackupEntry(tree, it, name, size), false, nil
		}
	}
	if _, err := s.pack(ctx, []domain.Item{it}, path); err != nil {
		return domain.BackupEntry{}, false, err
	}
	size, _, _, err := s.files.Stat(path)
	if err != nil {
		return domain.BackupEntry{}, false, err
	}
	return domain.NewBackupEntry(tree, it, name, size), true, nil
}

// setAside deals with the archives of the documents old lists and entries
// do not. The archive of a document gone from the library — deleted, or in
// the trash unless all — is deleted with prune and moved into the removed
// directory otherwise; the archive of a document still there but outside
// the path backed up is always moved, never deleted. An archive already
// gone is not counted.
func (s *Service) setAside(
	dir string, tree *domain.Tree, old domain.BackupManifest, entries []domain.BackupEntry, prune, all bool, report *port.BackupReport,
) error {
	kept := make(map[domain.ItemID]bool, len(entries))
	for _, e := range entries {
		kept[e.ID] = true
	}
	removed := filepath.Join(dir, domain.BackupRemovedDir)
	for _, e := range old.Documents {
		if kept[e.ID] {
			continue
		}
		path := filepath.Join(dir, e.File)
		_, isDir, exists, err := s.files.Stat(path)
		if err != nil {
			return err
		}
		if !exists || isDir {
			continue
		}
		_, inLibrary := tree.Get(e.ID)
		gone := !inLibrary || (!all && tree.IsTrashed(e.ID))
		if gone && prune {
			if err = s.files.Remove(path); err != nil {
				return err
			}
			report.Pruned++
			continue
		}
		if err = s.files.MkdirAll(removed); err != nil {
			return err
		}
		if err = s.files.Rename(path, filepath.Join(removed, e.File)); err != nil {
			return err
		}
		report.Moved++
		if !gone {
			report.Outside++
		}
	}
	return nil
}
