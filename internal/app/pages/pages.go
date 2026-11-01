// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// MaxText is the largest text file a notebook is made from.
const MaxText = 16 << 20

// Service implements port.Pages.
type Service struct {
	lib    *library.Library
	reader *Reader
	editor port.PageEditor
	writer port.PageWriter
	docs   port.DocumentStore
	blobs  port.BlobStore
	files  port.LocalFiles
	ids    port.IDs
	clock  port.Clock
}

var _ port.Pages = (*Service)(nil)

// New returns the pages use case. A nil dependency panics.
func New(
	lib *library.Library, reader *Reader, editor port.PageEditor, writer port.PageWriter,
	docs port.DocumentStore, blobs port.BlobStore, files port.LocalFiles, ids port.IDs, clk port.Clock,
) *Service {
	switch {
	case lib == nil:
		panic("pages: Library is required")
	case reader == nil:
		panic("pages: Reader is required")
	case editor == nil:
		panic("pages: PageEditor is required")
	case writer == nil:
		panic("pages: PageWriter is required")
	case docs == nil:
		panic("pages: DocumentStore is required")
	case blobs == nil:
		panic("pages: BlobStore is required")
	case files == nil:
		panic("pages: LocalFiles is required")
	case ids == nil:
		panic("pages: IDs is required")
	case clk == nil:
		panic("pages: Clock is required")
	}
	return &Service{lib: lib, reader: reader, editor: editor, writer: writer, docs: docs, blobs: blobs, files: files, ids: ids, clock: clk}
}

// document resolves a query to a document; items in the trash are found too.
func (s *Service) document(ctx context.Context, query string) (*domain.Tree, domain.Item, error) {
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return nil, domain.Item{}, err
	}
	it, found, err := tree.Resolve(query, true)
	if err != nil {
		return nil, domain.Item{}, err
	}
	if !found {
		return nil, domain.Item{}, domain.ErrNotFound
	}
	if it.Kind.IsFolder() {
		return nil, domain.Item{}, fmt.Errorf("%s: a folder has no pages: %w", it.Name, domain.ErrUnsupportedFile)
	}
	return tree, it, nil
}

// Pages implements port.Pages.
func (s *Service) Pages(ctx context.Context, query string, selected []string) (port.DocumentText, error) {
	_, it, err := s.document(ctx, query)
	if err != nil {
		return port.DocumentText{}, err
	}
	pages, err := s.reader.Read(ctx, it, selected)
	if err != nil {
		return port.DocumentText{}, err
	}
	return port.DocumentText{Item: it, Pages: pages}, nil
}

// Unmark implements port.Pages: every highlight of the selected pages is
// erased the way the tablet's eraser does, the edited pages replace the old
// ones in the document's index, and the change is one commit. A document in
// the trash is refused; one with nothing marked is left as it is.
func (s *Service) Unmark(ctx context.Context, query string, selected []string) (port.UnmarkReport, error) {
	tree, it, err := s.document(ctx, query)
	if err != nil {
		return port.UnmarkReport{}, err
	}
	if tree.IsTrashed(it.ID) {
		return port.UnmarkReport{}, domain.ErrInTrash
	}
	report := port.UnmarkReport{Item: it}
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		cur, ok := tree.Get(it.ID)
		if !ok {
			return nil, domain.ErrVanished
		}
		r, records, uerr := s.unmark(ctx, cur, selected, state.Records)
		report = r
		return records, uerr
	})
	return report, err
}

func (s *Service) unmark(ctx context.Context, it domain.Item, selected []string, records []domain.Record) (port.UnmarkReport, []domain.Record, error) {
	report := port.UnmarkReport{Item: it}
	picked, err := slots(ctx, it, selected, s.lib.Read)
	if err != nil {
		return report, nil, err
	}
	files := it.Files
	for _, sl := range picked {
		if !sl.found {
			continue
		}
		data, rerr := s.lib.Read(ctx, sl.file)
		if rerr != nil {
			return report, nil, rerr
		}
		ids, herr := s.editor.Highlights(data)
		if herr != nil {
			return report, nil, fmt.Errorf("page %d: %w", sl.number, herr)
		}
		if len(ids) == 0 {
			continue
		}
		// The highlighter's marks are its highlights and its strokes; the
		// page's own reading says how many of each.
		page, perr := s.reader.pages.ReadPage(data)
		if perr != nil {
			return report, nil, fmt.Errorf("page %d: %w", sl.number, perr)
		}
		highlights := min(len(page.Highlights), len(ids))
		edited, eerr := s.editor.Erase(data, ids)
		if eerr != nil {
			return report, nil, fmt.Errorf("page %d: %w", sl.number, eerr)
		}
		f, uerr := s.blobs.Upload(ctx, sl.file.Name, edited)
		if uerr != nil {
			return report, nil, uerr
		}
		files = library.ReplaceFile(files, f)
		report.Erased += len(ids)
		report.Highlights += highlights
		report.Strokes += len(ids) - highlights
		report.Pages++
	}
	if report.Erased == 0 {
		return report, records, nil
	}
	meta, _, err := s.lib.Metadata(ctx, it)
	if err != nil {
		return report, nil, err
	}
	edited := meta.Edited(s.clock.Now())
	mf, err := s.blobs.Upload(ctx, string(it.ID)+".metadata", edited.Bytes())
	if err != nil {
		return report, nil, err
	}
	files = library.ReplaceFile(files, mf)
	rec, err := s.docs.PutIndex(ctx, it.ID, files)
	if err != nil {
		return report, nil, err
	}
	out, _ := library.Replace(records, rec)
	report.Item = domain.NewItem(rec, files, edited, it.Tags)
	return report, out, nil
}

// Notebook implements port.Pages: a .md, .markdown or .txt file becomes a
// notebook of typed text, laid out for opts.Device and paginated the way the
// converter does. When the destination folder is gone or in the trash by the
// time of the commit, the notebook lands at the top level.
func (s *Service) Notebook(ctx context.Context, local, dest string, opts port.NotebookOptions) (domain.Item, error) {
	plan, err := s.planNotebook(ctx, local, dest, opts)
	if err != nil {
		return domain.Item{}, err
	}
	name, parent, author := plan.name, plan.parent, plan.author
	now := s.clock.Now()
	id := s.ids.NewID()
	files := make([]domain.File, 0, len(plan.pages)+3)
	pageIDs := make([]string, 0, len(plan.pages))
	for _, data := range plan.pages {
		pid := string(s.ids.NewID())
		f, uerr := s.blobs.Upload(ctx, domain.PageFileName(id, pid), data)
		if uerr != nil {
			return domain.Item{}, uerr
		}
		files = append(files, f)
		pageIDs = append(pageIDs, pid)
	}
	tags := domain.MergeTags(nil, opts.Tags, now.UnixMilli())
	meta := domain.NewDocumentMetadata(name, parent, now)
	for _, part := range []struct {
		name string
		body []byte
	}{
		{string(id) + ".pagedata", domain.Pagedata(len(pageIDs))},
		{string(id) + ".content", domain.NewNotebookContent(pageIDs, author, tags)},
		{string(id) + ".metadata", meta.Bytes()},
	} {
		f, uerr := s.blobs.Upload(ctx, part.name, part.body)
		if uerr != nil {
			return domain.Item{}, uerr
		}
		files = append(files, f)
	}
	rec, err := s.docs.PutIndex(ctx, id, files)
	if err != nil {
		return domain.Item{}, err
	}
	result := domain.NewItem(rec, files, meta, opts.Tags)
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		final, finalFiles, finalMeta := rec, files, meta
		if gone(tree, parent) {
			finalMeta = domain.NewDocumentMetadata(name, domain.RootID, now)
			mf, uerr := s.blobs.Upload(ctx, string(id)+".metadata", finalMeta.Bytes())
			if uerr != nil {
				return nil, uerr
			}
			finalFiles = library.ReplaceFile(files, mf)
			var perr error
			if final, perr = s.docs.PutIndex(ctx, id, finalFiles); perr != nil {
				return nil, perr
			}
		}
		result = domain.NewItem(final, finalFiles, finalMeta, opts.Tags)
		return append(slices.Clone(state.Records), final), nil
	})
	if err != nil {
		return domain.Item{}, err
	}
	return result, nil
}

// gone reports a destination folder missing from the tree, or in the trash.
func gone(tree *domain.Tree, parent domain.ItemID) bool {
	if parent == domain.RootID {
		return false
	}
	it, ok := tree.Get(parent)
	return !ok || !it.Kind.IsFolder() || tree.IsTrashed(parent)
}

// paragraphs reads a text or markdown file into notebook paragraphs.
func (s *Service) paragraphs(local string) ([]domain.Paragraph, error) {
	ext := strings.ToLower(filepath.Ext(local))
	if ext != ".md" && ext != ".markdown" && ext != ".txt" {
		return nil, fmt.Errorf("%s: %w: a notebook is made from a .md or .txt file", local, domain.ErrUnsupportedFile)
	}
	size, isDir, exists, err := s.files.Stat(local)
	switch {
	case err != nil:
		return nil, err
	case !exists:
		return nil, fmt.Errorf("%s: %w", local, domain.ErrNotFound)
	case isDir:
		return nil, fmt.Errorf("%s: %w: a directory", local, domain.ErrUnsupportedFile)
	case size > MaxText:
		return nil, fmt.Errorf("%s: %w: larger than 16 MiB", local, domain.ErrUnsupportedFile)
	}
	data, err := s.files.ReadHead(local, int(size))
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: %w: not UTF-8 text", local, domain.ErrUnsupportedFile)
	}
	if ext == ".txt" {
		return domain.ParsePlainText(string(data)), nil
	}
	paragraphs, err := domain.ParseMarkdown(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", local, err)
	}
	return paragraphs, nil
}

// notebookPlan is a notebook ready to upload: its pages written, its folder
// resolved.
type notebookPlan struct {
	name   string
	parent domain.ItemID
	author string
	pages  [][]byte
}

// planNotebook reads and parses the file, lays its pages out for the device,
// writes them in memory and resolves the folder. Nothing leaves the machine.
func (s *Service) planNotebook(ctx context.Context, local, dest string, opts port.NotebookOptions) (notebookPlan, error) {
	paragraphs, err := s.paragraphs(local)
	if err != nil {
		return notebookPlan{}, err
	}
	device, err := domain.ParseDevice(string(opts.Device))
	if err != nil {
		return notebookPlan{}, err
	}
	laid, err := domain.Paginate(paragraphs, device)
	if err != nil {
		return notebookPlan{}, err
	}
	name := opts.Name
	if name == "" {
		base := filepath.Base(local)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if strings.TrimSpace(name) == "" {
		return notebookPlan{}, domain.ErrEmptyName
	}
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return notebookPlan{}, err
	}
	parent, err := tree.ResolveFolder(dest)
	if err != nil {
		return notebookPlan{}, err
	}
	author := string(s.ids.NewID())
	sheet := domain.SheetOf(device)
	pages := make([][]byte, 0, len(laid))
	for _, paras := range laid {
		data, werr := s.writer.WritePage(domain.PageDraft{
			Author: authorBytes(author), Sheet: sheet, Column: domain.ColumnFor(sheet), Paragraphs: paras,
		})
		if werr != nil {
			return notebookPlan{}, werr
		}
		pages = append(pages, data)
	}
	return notebookPlan{name: name, parent: parent, author: author, pages: pages}, nil
}

// PlanNotebook implements port.Pages.
func (s *Service) PlanNotebook(ctx context.Context, local, dest string, opts port.NotebookOptions) (domain.Item, error) {
	plan, err := s.planNotebook(ctx, local, dest, opts)
	if err != nil {
		return domain.Item{}, err
	}
	return domain.Item{Name: plan.name, Kind: domain.KindNotebook, Parent: plan.parent}, nil
}

// authorBytes is the page author of a document: the bytes of its UUID, or,
// for an identifier that is not one, the first bytes of its hash.
func authorBytes(id string) [16]byte {
	var out [16]byte
	if raw, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err == nil && len(raw) == len(out) {
		copy(out[:], raw)
		return out
	}
	sum := sha256.Sum256([]byte(id))
	copy(out[:], sum[:])
	return out
}
