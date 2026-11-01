// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"context"
	"fmt"
	"strings"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// relaid is a document laid out for a new PDF: its pages before, the
// relayout, the .content it had, and the plan that describes them.
type relaid struct {
	before  []domain.PageSlot
	layout  domain.Relayout
	content []byte
	plan    port.ContentPlan
}

// putContent replaces the PDF of the PDF document name in parent with up,
// keeping its pages' annotations: see port.PutOptions.ContentOnly.
func (s *Service) putContent(ctx context.Context, tree *domain.Tree, parent domain.ItemID, name string, up upload,
	opts port.PutOptions, progress port.Progress,
) (domain.Item, error) {
	old, found, err := sameName(tree, parent, name)
	if err != nil {
		return domain.Item{}, err
	}
	if !found {
		return domain.Item{}, fmt.Errorf("%s: %w", name, domain.ErrNoContentTarget)
	}
	if old.Kind != domain.KindPDF && old.Kind != domain.KindAnnotatedPDF {
		return domain.Item{}, fmt.Errorf("%s is %s: %w", name, old.Kind, domain.ErrNotPDFDocument)
	}
	r, err := s.relay(ctx, old, up)
	if err != nil {
		return domain.Item{}, err
	}
	if opts.OnContentPlan != nil {
		opts.OnContentPlan(r.plan)
	}
	if err = checkRelay(up, r, opts); err != nil {
		return domain.Item{}, err
	}
	if opts.DryRun {
		would := old
		would.Size = up.size
		return would, nil
	}
	original, err := s.blobs.UploadFile(ctx, up.path, string(old.ID)+".pdf", progress)
	if err != nil {
		return domain.Item{}, fmt.Errorf("upload %s: %w", up.path, err)
	}
	var result domain.Item
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		cur, ok := tree.Get(old.ID)
		switch {
		case !ok:
			return nil, domain.ErrVanished
		case tree.IsTrashed(cur.ID):
			return nil, domain.ErrInTrash
		case cur.Parent != old.Parent || cur.Name != old.Name:
			return nil, domain.ErrReplaceTargetChanged
		}
		// The pages are laid out again on a replay: the document may have
		// changed on the tablet meanwhile.
		again, rerr := s.relay(ctx, cur, up)
		if rerr == nil {
			rerr = checkRelay(up, again, opts)
		}
		if rerr != nil {
			return nil, rerr
		}
		rec, files, meta, ferr := s.relaidFiles(ctx, cur, original, up, again, opts.CurrentPage)
		if ferr != nil {
			return nil, ferr
		}
		result = domain.NewItem(rec, files, meta, cur.Tags)
		records, _ := library.Replace(state.Records, rec)
		return records, nil
	})
	if err != nil {
		return domain.Item{}, err
	}
	return result, nil
}

// relay lays doc's pages out for up and describes the plan.
func (s *Service) relay(ctx context.Context, doc domain.Item, up upload) (relaid, error) {
	f, ok := doc.File("content")
	if !ok {
		return relaid{}, domain.MalformedFile("content", "missing from "+string(doc.ID))
	}
	content, err := s.lib.Read(ctx, f)
	if err != nil {
		return relaid{}, err
	}
	before, err := domain.ContentPages(content)
	if err != nil {
		return relaid{}, err
	}
	layout := domain.PlanRelayout(before, up.pages, func() string { return string(s.ids.NewID()) })
	plan := port.ContentPlan{
		Document: doc, OldPages: len(before), NewPages: len(layout.Pages()),
		OldOriginal: domain.ContentOriginalPages(content), NewOriginal: up.pages,
		Kept: len(layout.Kept), Appended: len(layout.Appended), Dropped: len(layout.Dropped),
	}
	number := map[string]int{}
	for i, p := range before {
		number[p.ID] = i + 1
	}
	for _, p := range layout.Kept {
		if _, notes := domain.PageFiles(doc.ID, p.ID, doc.Files); len(notes) > 0 {
			plan.Annotated++
		}
	}
	for _, p := range layout.Dropped {
		if _, notes := domain.PageFiles(doc.ID, p.ID, doc.Files); len(notes) > 0 {
			plan.Lost = append(plan.Lost, port.AnnotatedPage{Number: number[p.ID], ID: p.ID, Files: len(notes)})
		}
	}
	return relaid{before: before, layout: layout, content: content, plan: plan}, nil
}

// checkRelay refuses a relayout that drops annotations unless asked to, and
// an opening page beyond the document's new pages.
func checkRelay(up upload, r relaid, opts port.PutOptions) error {
	if len(r.plan.Lost) > 0 && !opts.DropAnnotations {
		pages := make([]string, len(r.plan.Lost))
		for i, p := range r.plan.Lost {
			pages[i] = fmt.Sprintf("page %d (%s, %d annotation files)", p.Number, p.ID, p.Files)
		}
		return fmt.Errorf("%w: %s", domain.ErrAnnotationsDropped, strings.Join(pages, ", "))
	}
	if opts.CurrentPage > r.plan.NewPages {
		return pageOutOfRange(up.path, opts.CurrentPage, r.plan.NewPages)
	}
	return nil
}

// relaidFiles uploads cur's new .content, .metadata and .pagedata and its
// index: the new original in place of the old, every file of a kept page
// as it was, the files of dropped pages and the thumbnails gone.
func (s *Service) relaidFiles(ctx context.Context, cur domain.Item, original domain.File, up upload, r relaid, current int) (
	domain.Record, []domain.File, domain.Metadata, error,
) {
	now := s.clock.Now()
	pages := r.layout.Pages()
	opening := ""
	if current > 0 {
		opening = pages[current-1].ID
	}
	content, err := domain.RelaidContent(r.content, r.layout, up.size, opening)
	if err != nil {
		return domain.Record{}, nil, domain.Metadata{}, err
	}
	meta, _, err := s.lib.Metadata(ctx, cur)
	if err != nil {
		return domain.Record{}, nil, domain.Metadata{}, err
	}
	meta = opened(meta.Rewritten(len(pages), now), current)
	var oldPagedata []byte
	if f, ok := cur.File("pagedata"); ok {
		if oldPagedata, err = s.lib.Read(ctx, f); err != nil {
			return domain.Record{}, nil, domain.Metadata{}, err
		}
	}
	id := string(cur.ID)
	rest, err := s.uploadAll(ctx, []named{
		{name: id + ".content", body: content},
		{name: id + ".metadata", body: meta.Bytes()},
		{name: id + ".pagedata", body: domain.RelaidPagedata(oldPagedata, r.before, r.layout)},
	})
	if err != nil {
		return domain.Record{}, nil, domain.Metadata{}, err
	}
	gone := map[string]bool{}
	for _, p := range r.layout.Dropped {
		all, _ := domain.PageFiles(cur.ID, p.ID, cur.Files)
		for _, f := range all {
			gone[f.Name] = true
		}
	}
	files := []domain.File{original}
	for _, f := range cur.Files {
		switch {
		case gone[f.Name], domain.IsThumbnail(cur.ID, f):
		case f.Name == id+".pdf", f.Name == id+".content", f.Name == id+".metadata", f.Name == id+".pagedata":
		default:
			files = append(files, f)
		}
	}
	files = append(files, rest...)
	rec, err := s.docs.PutIndex(ctx, cur.ID, files)
	if err != nil {
		return domain.Record{}, nil, domain.Metadata{}, err
	}
	return rec, files, meta, nil
}
