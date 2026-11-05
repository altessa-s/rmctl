// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ErrNoManifest means a directory holds no backup manifest.
var ErrNoManifest = errors.New("no " + domain.BackupManifestName + ": not a backup directory")

// ErrNothingToRestore means a backup lists no document.
var ErrNothingToRestore = errors.New("the backup holds no document")

// restoring is one archive of a backup on its way back: the manifest entry,
// the archive's documents and the folder path they go into.
type restoring struct {
	entry  domain.BackupEntry
	docs   []archived
	folder string
}

// folderPlan is the folders a restore puts documents in: each path's
// folder, existing or new, and the new folders' records, parents first.
type folderPlan struct {
	ids     map[string]domain.ItemID
	records []domain.Record
	created []string
	// anchors are the deepest existing folders of the paths, which the
	// documents or the new folders go into; each must still be there when
	// the restore commits.
	anchors []domain.ItemID
}

// RestoreBackup implements port.Writer. The archives are read first, so a
// missing or broken one stops the restore before anything is uploaded; the
// missing folders are then created and every document is placed in one
// commit, which a moved library replays.
func (s *Service) RestoreBackup(ctx context.Context, dir string, opts port.RestoreOptions) (_ port.RestoreReport, err error) {
	m, found, err := library.ReadManifest(s.files, dir)
	if err != nil {
		return port.RestoreReport{}, err
	}
	if !found {
		return port.RestoreReport{}, fmt.Errorf("%s: %w", dir, ErrNoManifest)
	}
	entries, err := m.Select(opts.IDs)
	if err != nil {
		return port.RestoreReport{}, err
	}
	if len(entries) == 0 {
		return port.RestoreReport{}, fmt.Errorf("%s: %w", dir, ErrNothingToRestore)
	}
	report := port.RestoreReport{Profile: m.Profile, Created: m.Created}
	stage := ""
	if !opts.DryRun {
		var remove func() error
		if stage, remove, err = s.files.Stage(); err != nil {
			return port.RestoreReport{}, err
		}
		defer func() {
			if rerr := remove(); rerr != nil {
				err = errors.Join(err, fmt.Errorf("remove the staging directory %s: %w", stage, rerr))
			}
		}()
	}
	work := make([]restoring, len(entries))
	for i, e := range entries {
		sub := ""
		if stage != "" {
			sub = filepath.Join(stage, strconv.Itoa(i))
			if err = s.files.MkdirAll(sub); err != nil {
				return port.RestoreReport{}, err
			}
		}
		docs, rerr := s.readArchive(ctx, filepath.Join(dir, e.File), sub)
		if rerr != nil {
			return port.RestoreReport{}, rerr
		}
		work[i] = restoring{entry: e, docs: docs, folder: restoreFolder(opts.To, e)}
	}
	tree, state, err := s.lib.Load(ctx)
	if err != nil {
		return port.RestoreReport{}, err
	}
	plan, err := s.planFolders(ctx, tree, work, opts.DryRun)
	if err != nil {
		return port.RestoreReport{}, err
	}
	report.Folders = plan.created
	if opts.DryRun {
		report.Documents = s.plannedRestore(work, plan, state)
		return report, nil
	}
	report.Documents, err = s.commitRestore(ctx, work, plan)
	if err != nil {
		return port.RestoreReport{}, err
	}
	return report, nil
}

// restoreFolder is the folder path entry goes back into below to.
func restoreFolder(to string, e domain.BackupEntry) string {
	return path.Join("/", to, e.Folder())
}

// planFolders finds the folder of each path, existing or not. Unless
// dryRun, the missing ones are stored, each once, parents first; a dry run
// leaves their identifiers empty.
func (s *Service) planFolders(ctx context.Context, tree *domain.Tree, work []restoring, dryRun bool) (folderPlan, error) {
	paths := make([]string, 0, len(work))
	for _, w := range work {
		paths = append(paths, w.folder)
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	plan := folderPlan{ids: map[string]domain.ItemID{"/": domain.RootID}}
	anchored := map[domain.ItemID]bool{}
	for _, p := range paths {
		if _, ok := plan.ids[p]; ok {
			continue
		}
		parts := components(p)
		parent, _, missing, err := walk(tree, parts)
		if err != nil {
			return folderPlan{}, fmt.Errorf("%s: %w", p, err)
		}
		if !anchored[parent] {
			anchored[parent] = true
			plan.anchors = append(plan.anchors, parent)
		}
		// The folders walk found are there; of the rest, an earlier path of
		// this restore may have planned some already.
		for i := len(parts) - len(missing); i < len(parts); i++ {
			key := "/" + strings.Join(parts[:i+1], "/")
			if id, ok := plan.ids[key]; ok {
				parent = id
				continue
			}
			var id domain.ItemID
			if !dryRun {
				records, last, err := s.newFolders(ctx, parts[i:i+1], parent)
				if err != nil {
					return folderPlan{}, err
				}
				plan.records = append(plan.records, records...)
				id = last.ID
			}
			plan.ids[key], parent = id, id
			plan.created = append(plan.created, key)
		}
		plan.ids[p] = parent
	}
	return plan, nil
}

// plannedRestore is what a restore would add against state: the
// identifiers Import would give, with nothing uploaded.
func (s *Service) plannedRestore(work []restoring, plan folderPlan, state port.RootState) []port.RestoredDocument {
	taken := make(map[domain.ItemID]bool, len(state.Records))
	for _, rec := range state.Records {
		taken[rec.ID] = true
	}
	chosen := map[domain.ItemID]domain.ItemID{}
	var out []port.RestoredDocument
	for _, w := range work {
		for _, d := range w.docs {
			id := s.identifier(d.id, chosen, taken)
			files := make([]domain.File, len(d.files))
			for i, f := range d.files {
				name, _ := domain.RenameRecord(f.name, d.id, id)
				files[i] = domain.File{Name: name, Size: d.sizes[i]}
			}
			meta := d.meta.ImportedUnder(plan.ids[w.folder])
			out = append(out, port.RestoredDocument{
				Item: domain.NewItem(domain.Record{ID: id}, files, meta, d.tags), From: d.id, File: w.entry.File, Folder: w.folder,
			})
		}
	}
	return out
}

// commitRestore places every document in its folder in one commit with the
// new folders. An existing folder they go into that is gone or in the trash
// by then stops the restore.
func (s *Service) commitRestore(ctx context.Context, work []restoring, plan folderPlan) ([]port.RestoredDocument, error) {
	uploaded := map[placement]placed{}
	chosen := map[domain.ItemID]domain.ItemID{}
	var result []port.RestoredDocument
	_, err := s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		for _, a := range plan.anchors {
			if cerr := checkDestination(tree, a, domain.RootID); cerr != nil {
				return nil, cerr
			}
		}
		taken := make(map[domain.ItemID]bool, len(state.Records)+len(plan.records))
		for _, rec := range state.Records {
			taken[rec.ID] = true
		}
		for _, rec := range plan.records {
			taken[rec.ID] = true
		}
		records := slices.Concat(state.Records, plan.records)
		result = result[:0]
		for _, w := range work {
			target := plan.ids[w.folder]
			for _, d := range w.docs {
				id := s.identifier(d.id, chosen, taken)
				key := placement{from: d.id, id: id, parent: target}
				p, ok := uploaded[key]
				if !ok {
					var perr error
					if p, perr = s.place(ctx, d, id, target); perr != nil {
						return nil, perr
					}
					uploaded[key] = p
				}
				records = append(records, p.rec)
				result = append(result, port.RestoredDocument{
					Item: domain.NewItem(p.rec, p.files, p.meta, d.tags), From: d.id, File: w.entry.File, Folder: w.folder,
				})
			}
		}
		return records, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
