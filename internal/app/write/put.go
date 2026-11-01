// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package write

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

const (
	// MaxUpload is the largest file the cloud accepts.
	MaxUpload = 500 << 20
	// epubTail is how much of an EPUB's end is searched for its DRM marker.
	epubTail = 128 << 10
	// DefaultJobs is how many files a recursive upload sends at once.
	DefaultJobs = 4
)

// extPDF is the extension of a PDF upload.
const extPDF = "pdf"

// zipMagic starts every EPUB, which is a ZIP archive.
const zipMagic = "PK\x03\x04"

// drmMarker is the file an encrypted EPUB carries.
const drmMarker = "META-INF/encryption.xml"

// upload is a validated local file.
type upload struct {
	path  string
	ext   string
	size  int64
	pages int
}

// Put implements port.Writer. When the destination folder is gone or in the
// trash by the time of the commit, the document lands at the top level.
func (s *Service) Put(ctx context.Context, local, dest string, opts port.PutOptions, progress port.Progress) (domain.Item, error) {
	up, err := s.inspect(local)
	if err != nil {
		return domain.Item{}, err
	}
	name := opts.Name
	if name == "" {
		base := filepath.Base(local)
		name = base[:len(base)-len(filepath.Ext(base))]
	}
	if strings.TrimSpace(name) == "" {
		return domain.Item{}, domain.ErrEmptyName
	}
	if err = checkPutOptions(local, up, opts); err != nil {
		return domain.Item{}, err
	}
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return domain.Item{}, err
	}
	parent, err := destination(tree, dest)
	if err != nil {
		return domain.Item{}, err
	}
	if opts.ContentOnly {
		return s.putContent(ctx, tree, parent, name, up, opts, progress)
	}
	var old domain.Item
	replacing := false
	if opts.Replace {
		if old, replacing, err = sameName(tree, parent, name); err != nil {
			return domain.Item{}, err
		}
		if replacing && opts.OnReplace != nil {
			opts.OnReplace(old)
		}
	}
	if opts.DryRun {
		kind := domain.KindPDF
		if up.ext == "epub" {
			kind = domain.KindEPUB
		}
		would := domain.Item{Name: name, Kind: kind, Parent: parent, Size: up.size, Tags: opts.Tags}
		if replacing {
			would.ID = old.ID
		}
		return would, nil
	}
	if replacing {
		return s.replace(ctx, old, up, opts, progress)
	}
	now := s.clock.Now()
	id := s.ids.NewID()
	original, err := s.blobs.UploadFile(ctx, up.path, string(id)+"."+up.ext, progress)
	if err != nil {
		return domain.Item{}, fmt.Errorf("upload %s: %w", local, err)
	}
	content := s.content(up, tagsOf(opts.Tags, now.UnixMilli()), opts.Cover)
	meta := opened(domain.NewDocumentMetadata(name, parent, now), opts.CurrentPage)
	list := []named{{name: string(id) + ".content", body: content}, {name: string(id) + ".metadata", body: meta.Bytes()}}
	if up.pages > 0 {
		list = append(list, named{name: string(id) + ".pagedata", body: domain.Pagedata(up.pages)})
	}
	rest, err := s.uploadAll(ctx, list)
	if err != nil {
		return domain.Item{}, err
	}
	files := append([]domain.File{original}, rest...)
	rec, err := s.docs.PutIndex(ctx, id, files)
	if err != nil {
		return domain.Item{}, err
	}
	result := domain.NewItem(rec, files, meta, opts.Tags)
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		final, finalFiles, finalMeta := rec, files, meta
		if checkDestination(tree, parent, domain.RootID) != nil {
			finalMeta = opened(domain.NewDocumentMetadata(name, domain.RootID, now), opts.CurrentPage)
			var rerr error
			if final, finalFiles, rerr = s.rewriteMetadata(ctx, domain.Item{ID: id, Files: files}, finalMeta); rerr != nil {
				return nil, rerr
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

// checkPutOptions refuses what an upload of up cannot honor: a content
// replacement or an opening page of anything but a PDF, a content
// replacement that is also a replacement, an opening page beyond the PDF.
func checkPutOptions(local string, up upload, opts port.PutOptions) error {
	pdfOnly := func(what string) error {
		return fmt.Errorf("%s: %w: %s is for a PDF", local, domain.ErrUnsupportedFile, what)
	}
	switch {
	case opts.ContentOnly && up.ext != extPDF:
		return pdfOnly("--content-only")
	case opts.CurrentPage != 0 && up.ext != extPDF:
		return pdfOnly("--current-page")
	case opts.ContentOnly && opts.Replace:
		return fmt.Errorf("%s: --content-only and --replace exclude each other: %w", local, domain.ErrUnsupportedFile)
	case opts.CurrentPage < 0, !opts.ContentOnly && opts.CurrentPage > up.pages:
		return pageOutOfRange(local, opts.CurrentPage, up.pages)
	}
	return nil
}

func pageOutOfRange(local string, page, pages int) error {
	return fmt.Errorf("page %d of %s, which has %d pages: %w", page, local, pages, domain.ErrNoSuchPage)
}

// opened is meta opening on page, from 1; 0 leaves it as it is.
func opened(meta domain.Metadata, page int) domain.Metadata {
	if page <= 0 {
		return meta
	}
	return meta.OpenedAt(page - 1)
}

// content is the .content of a fresh upload.
func (s *Service) content(up upload, tags []domain.Tag, cover port.Cover) []byte {
	coverPage := domain.CoverLastOpened
	if cover == port.CoverFirst {
		coverPage = domain.CoverFirstPage
	}
	if up.ext != extPDF {
		return domain.NewEPUBContentWithCover(up.size, tags, coverPage)
	}
	pages := make([]string, up.pages)
	for i := range pages {
		pages[i] = string(s.ids.NewID())
	}
	return domain.NewPDFContentWithCover(pages, up.size, tags, coverPage)
}

// sameName finds the document of name in folder, the one Replace replaces.
// Several of that name are refused: which one is meant is the user's call.
func sameName(tree *domain.Tree, folder domain.ItemID, name string) (domain.Item, bool, error) {
	var found []domain.Item
	for _, child := range tree.Children(folder, false) {
		if !child.Kind.IsFolder() && child.Name == name {
			found = append(found, child)
		}
	}
	switch len(found) {
	case 0:
		return domain.Item{}, false, nil
	case 1:
		return found[0], true, nil
	default:
		return domain.Item{}, false, &domain.AmbiguousError{Query: name, Candidates: found}
	}
}

// replace swaps the original of old for the uploaded file. The new index
// holds the original, a fresh .content carrying old's tags and the new
// ones, the .pagedata and the rewritten metadata: old's pages and their
// annotations do not survive, since the pages they were on are gone.
func (s *Service) replace(ctx context.Context, old domain.Item, up upload, opts port.PutOptions, progress port.Progress) (domain.Item, error) {
	original, err := s.blobs.UploadFile(ctx, up.path, string(old.ID)+"."+up.ext, progress)
	if err != nil {
		return domain.Item{}, fmt.Errorf("upload %s: %w", up.path, err)
	}
	var result domain.Item
	_, err = s.lib.Commit(ctx, func(ctx context.Context, tree *domain.Tree, state port.RootState) ([]domain.Record, error) {
		cur, ok := tree.Get(old.ID)
		if !ok {
			return nil, domain.ErrVanished
		}
		if tree.IsTrashed(cur.ID) {
			return nil, domain.ErrInTrash
		}
		// The document was chosen as the one of this name in this folder; a
		// replay after a conflict must not replace it somewhere else or
		// under another name.
		if cur.Parent != old.Parent || cur.Name != old.Name {
			return nil, domain.ErrReplaceTargetChanged
		}
		now := s.clock.Now()
		var kept []domain.Tag
		var oldContent []byte
		if f, ok := cur.File("content"); ok {
			data, rerr := s.lib.Read(ctx, f)
			if rerr != nil {
				return nil, rerr
			}
			kept = domain.ContentTagList(data)
			oldContent = data
		}
		tags := domain.MergeTags(kept, opts.Tags, now.UnixMilli())
		meta, _, merr := s.lib.Metadata(ctx, cur)
		if merr != nil {
			return nil, merr
		}
		meta = opened(meta.Rewritten(up.pages, now), opts.CurrentPage)
		content := domain.ReplacedContent(oldContent, s.content(up, tags, opts.Cover))
		list := []named{{name: string(cur.ID) + ".content", body: content}, {name: string(cur.ID) + ".metadata", body: meta.Bytes()}}
		if up.pages > 0 {
			list = append(list, named{name: string(cur.ID) + ".pagedata", body: domain.Pagedata(up.pages)})
		}
		rest, uerr := s.uploadAll(ctx, list)
		if uerr != nil {
			return nil, uerr
		}
		files := append([]domain.File{original}, rest...)
		rec, perr := s.docs.PutIndex(ctx, cur.ID, files)
		if perr != nil {
			return nil, perr
		}
		names := make([]string, len(tags))
		for i, t := range tags {
			names[i] = t.Name
		}
		result = domain.NewItem(rec, files, meta, names)
		records, _ := library.Replace(state.Records, rec)
		return records, nil
	})
	if err != nil {
		return domain.Item{}, err
	}
	return result, nil
}

// inspect validates a local file for upload: a PDF or EPUB by its
// extension, at most MaxUpload, a PDF with pages and not locked, an EPUB
// that is a ZIP without DRM.
func (s *Service) inspect(local string) (upload, error) {
	size, isDir, exists, err := s.files.Stat(local)
	if err != nil {
		return upload{}, err
	}
	if !exists {
		return upload{}, fmt.Errorf("%s: %w", local, domain.ErrNotFound)
	}
	unsupported := func(why string) error { return fmt.Errorf("%s: %w: %s", local, domain.ErrUnsupportedFile, why) }
	if isDir {
		return upload{}, unsupported("a directory")
	}
	if size > MaxUpload {
		return upload{}, unsupported("larger than 500 MiB")
	}
	if size == 0 {
		return upload{}, unsupported("an empty file")
	}
	up := upload{path: local, size: size, ext: strings.ToLower(strings.TrimPrefix(filepath.Ext(local), "."))}
	switch up.ext {
	case extPDF:
		pages, err := s.pdf.Pages(local)
		if err != nil {
			return upload{}, unsupported(err.Error())
		}
		if pages <= 0 {
			return upload{}, unsupported("a PDF without pages")
		}
		up.pages = pages
	case "epub":
		head, err := s.files.ReadHead(local, len(zipMagic))
		if err != nil {
			return upload{}, err
		}
		if string(head) != zipMagic {
			return upload{}, unsupported("not an EPUB archive")
		}
		tail, err := s.files.ReadTail(local, epubTail)
		if err != nil {
			return upload{}, err
		}
		if bytes.Contains(tail, []byte(drmMarker)) {
			return upload{}, unsupported("an EPUB protected by DRM")
		}
	default:
		return upload{}, unsupported("only PDF and EPUB files can be uploaded")
	}
	return up, nil
}

// Upload implements port.Writer: every PDF and EPUB below dir goes into
// dest, under folders that mirror dir's subdirectories. Incremental skips a
// file whose name the folder already holds.
func (s *Service) Upload(ctx context.Context, dir, dest string, opts port.UploadOptions) (port.MirrorReport, error) {
	rels, err := s.files.Walk(dir)
	if err != nil {
		return port.MirrorReport{}, err
	}
	tree, _, err := s.lib.Load(ctx)
	if err != nil {
		return port.MirrorReport{}, err
	}
	top, err := destination(tree, dest)
	if err != nil {
		return port.MirrorReport{}, err
	}
	base := ""
	if top != domain.RootID {
		base = tree.Path(top)
	}
	folders := map[string]domain.ItemID{".": top}
	var uploads []string
	for _, rel := range rels {
		ext := strings.ToLower(filepath.Ext(rel))
		if ext != ".pdf" && ext != ".epub" {
			continue
		}
		uploads = append(uploads, rel)
		sub := filepath.Dir(rel)
		if _, ok := folders[sub]; ok {
			continue
		}
		var folder domain.Item
		if folder, err = s.Mkdir(ctx, base+"/"+filepath.ToSlash(sub), true); err != nil {
			return port.MirrorReport{}, err
		}
		folders[sub] = folder.ID
	}
	if tree, _, err = s.lib.Load(ctx); err != nil {
		return port.MirrorReport{}, err
	}
	var written, skipped atomic.Int64
	jobs := opts.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	err = concurrency.Process(ctx, uploads, func(ctx context.Context, rel string) error {
		folder := folders[filepath.Dir(rel)]
		if opts.Incremental && holds(tree, folder, rel) {
			skipped.Add(1)
			return nil
		}
		if _, perr := s.Put(ctx, filepath.Join(dir, rel), string(folder), port.PutOptions{}, nil); perr != nil {
			return perr
		}
		written.Add(1)
		return nil
	}, concurrency.WithConcurrency[string](jobs), concurrency.WithStopOnError[string]())
	return port.MirrorReport{Written: int(written.Load()), Skipped: int(skipped.Load())}, err
}

// holds reports whether folder already has a document named after rel.
func holds(tree *domain.Tree, folder domain.ItemID, rel string) bool {
	base := filepath.Base(rel)
	name := base[:len(base)-len(filepath.Ext(base))]
	for _, child := range tree.Children(folder, false) {
		if !child.Kind.IsFolder() && child.Name == name {
			return true
		}
	}
	return false
}

// tagsOf stamps tag names, dropping empty ones and repeats.
func tagsOf(names []string, stamp int64) []domain.Tag {
	var tags []domain.Tag
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		tags = append(tags, domain.Tag{Name: n, Timestamp: stamp})
	}
	return tags
}
