// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pages

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// DefaultConcurrency is how many pages are fetched and read at once.
const DefaultConcurrency = 8

// Reader reads the pages of a document in its order. It is shared by the
// use cases that need typed text; it is not an input port itself.
type Reader struct {
	lib    *library.Library
	pages  port.PageReader
	concur int
}

// NewReader returns a page reader over the library. A nil dependency panics.
func NewReader(lib *library.Library, pages port.PageReader) *Reader {
	switch {
	case lib == nil:
		panic("pages: Library is required")
	case pages == nil:
		panic("pages: PageReader is required")
	}
	return &Reader{lib: lib, pages: pages, concur: DefaultConcurrency}
}

// HasPages reports whether an item holds .rm pages worth reading.
func HasPages(it domain.Item) bool {
	return slices.ContainsFunc(it.Files, func(f domain.File) bool { return f.Extension() == "rm" })
}

// slot is a page of the document's order and the file that holds it.
type slot struct {
	id     string
	number int
	file   domain.File
	found  bool
}

// Fetch reads one file of a document: from the cloud through the library,
// or from an archive on disk.
type Fetch func(ctx context.Context, f domain.File) ([]byte, error)

// slots lists the pages of it, numbered from 1: the order of its .content,
// or, when the .content names none, its .rm files in index order. selected
// keeps the pages of those numbers or identifiers; empty keeps all.
func slots(ctx context.Context, it domain.Item, selected []string, fetch Fetch) ([]slot, error) {
	ids, err := order(ctx, it, fetch)
	if err != nil {
		return nil, err
	}
	byName := map[string]domain.File{}
	for _, f := range it.Files {
		byName[f.Name] = f
	}
	slots := make([]slot, len(ids))
	for i, id := range ids {
		f, ok := byName[domain.PageFileName(it.ID, id)]
		slots[i] = slot{id: id, number: i + 1, file: f, found: ok}
	}
	return pick(slots, selected)
}

func order(ctx context.Context, it domain.Item, fetch Fetch) ([]string, error) {
	if f, ok := it.File("content"); ok {
		data, err := fetch(ctx, f)
		if err != nil {
			return nil, err
		}
		ids, err := domain.PageOrder(data)
		if err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			return ids, nil
		}
	}
	var ids []string
	prefix := string(it.ID) + "/"
	for _, f := range it.Files {
		if rest, ok := strings.CutPrefix(f.Name, prefix); ok && f.Extension() == "rm" && !strings.Contains(rest, "/") {
			ids = append(ids, strings.TrimSuffix(rest, ".rm"))
		}
	}
	return ids, nil
}

// pick keeps the slots selected by number or by identifier, in the
// document's order. A selection that names no page is refused.
func pick(slots []slot, selected []string) ([]slot, error) {
	if len(selected) == 0 {
		return slots, nil
	}
	keep := make([]bool, len(slots))
	for _, s := range selected {
		found := false
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(slots) {
			keep[n-1], found = true, true
		}
		for i, sl := range slots {
			if sl.id == s {
				keep[i], found = true, true
			}
		}
		if !found {
			return nil, fmt.Errorf("page %s: %w", s, domain.ErrNotFound)
		}
	}
	var out []slot
	for i, sl := range slots {
		if keep[i] {
			out = append(out, sl)
		}
	}
	return out, nil
}

// Read parses the selected pages of it, in order. A page whose file the
// index lacks is a page with nothing on it.
func (r *Reader) Read(ctx context.Context, it domain.Item, selected []string) ([]domain.Page, error) {
	return ReadPages(ctx, r.pages, it, selected, r.lib.Read, r.concur)
}

// ReadPages parses the selected pages of it in the document's order,
// reading its files with fetch, concur at a time: the one reading of pages
// the cloud and a local archive share. A page whose file the index lacks
// is a page with nothing on it.
func ReadPages(ctx context.Context, pr port.PageReader, it domain.Item, selected []string, fetch Fetch, concur int) ([]domain.Page, error) {
	picked, err := slots(ctx, it, selected, fetch)
	if err != nil {
		return nil, err
	}
	return concurrency.ProcessCollect(ctx, picked, func(ctx context.Context, sl slot) (domain.Page, error) {
		if !sl.found {
			return domain.Page{ID: sl.id, Number: sl.number}, nil
		}
		data, err := fetch(ctx, sl.file)
		if err != nil {
			return domain.Page{}, err
		}
		p, err := pr.ReadPage(data)
		if err != nil {
			return domain.Page{}, fmt.Errorf("page %d: %w", sl.number, err)
		}
		p.ID, p.Number = sl.id, sl.number
		return p, nil
	}, concurrency.WithConcurrency[slot](max(concur, 1)), concurrency.WithStopOnError[slot]())
}

// Contains reports whether any typed paragraph or highlighted passage of
// pages holds text, ignoring case.
func Contains(pages []domain.Page, text string) bool {
	needle := strings.ToLower(text)
	for _, p := range pages {
		for _, para := range p.Paragraphs {
			if strings.Contains(strings.ToLower(para.Text), needle) {
				return true
			}
		}
		for _, h := range p.Highlights {
			if h.HasText && strings.Contains(strings.ToLower(h.Text), needle) {
				return true
			}
		}
	}
	return false
}
