// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package browse reads the library: listings, details and searches. It
// implements port.Browser.
package browse

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/altessa-s/go-atlas-core/runtime/concurrency"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// TextConcurrency is how many documents a text search reads at once.
const TextConcurrency = 8

// Service implements port.Browser.
type Service struct {
	lib   *library.Library
	pages *pages.Reader
}

var _ port.Browser = (*Service)(nil)

// New returns the browse use case; pages reads the typed text a search by
// text looks into. A nil dependency panics.
func New(lib *library.Library, pages *pages.Reader) *Service {
	switch {
	case lib == nil:
		panic("browse: Library is required")
	case pages == nil:
		panic("browse: pages.Reader is required")
	}
	return &Service{lib: lib, pages: pages}
}

// Tree implements port.Browser.
func (s *Service) Tree(ctx context.Context) (*domain.Tree, error) {
	tree, _, err := s.lib.Load(ctx)
	return tree, err
}

// List implements port.Browser. "" and "/" list the top level; a folder
// lists its children; a document lists itself.
func (s *Service) List(ctx context.Context, query string, all bool) (*domain.Tree, []domain.Item, error) {
	tree, err := s.Tree(ctx)
	if err != nil {
		return nil, nil, err
	}
	if query == "" || query == "/" {
		return tree, tree.Children(domain.RootID, all), nil
	}
	it, found, err := tree.Resolve(query, all)
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return tree, tree.Children(domain.RootID, all), nil
	}
	if !it.Kind.IsFolder() {
		return tree, []domain.Item{it}, nil
	}
	return tree, tree.Children(it.ID, all), nil
}

// Stat implements port.Browser. Items in the trash are found too.
func (s *Service) Stat(ctx context.Context, query string) (*domain.Tree, domain.Item, error) {
	tree, err := s.Tree(ctx)
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
	return tree, it, nil
}

// Find implements port.Browser. The results are sorted by path. A search
// by text reads the pages of the documents that have pages and passed every
// other test; the rest cannot hold typed text or highlights.
func (s *Service) Find(ctx context.Context, q port.FindQuery) (*domain.Tree, []domain.Item, error) {
	var pattern *regexp.Regexp
	if q.Pattern != "" {
		var err error
		if pattern, err = regexp.Compile(q.Pattern); err != nil {
			return nil, nil, fmt.Errorf("invalid pattern: %w", err)
		}
	}
	tree, err := s.Tree(ctx)
	if err != nil {
		return nil, nil, err
	}
	under := domain.RootID
	if q.Under != "" {
		if under, err = tree.ResolveFolder(q.Under); err != nil {
			return nil, nil, err
		}
	}
	type hit struct {
		item domain.Item
		path string
	}
	var hits []hit
	for _, it := range tree.Items() {
		if !matches(tree, it, q, under, pattern) {
			continue
		}
		hits = append(hits, hit{it, tree.Path(it.ID)})
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return strings.Compare(a.path, b.path) })
	items := make([]domain.Item, len(hits))
	for i, h := range hits {
		items[i] = h.item
	}
	if q.Text == "" {
		return tree, items, nil
	}
	items, err = s.withText(ctx, items, q.Text)
	if err != nil {
		return nil, nil, err
	}
	return tree, items, nil
}

// withText keeps the items whose pages hold text, in their order.
func (s *Service) withText(ctx context.Context, items []domain.Item, text string) ([]domain.Item, error) {
	found, err := concurrency.ProcessCollect(ctx, items, func(ctx context.Context, it domain.Item) (bool, error) {
		if !pages.HasPages(it) {
			return false, nil
		}
		read, err := s.pages.Read(ctx, it, nil)
		if err != nil {
			return false, fmt.Errorf("%s: %w", it.Name, err)
		}
		return pages.Contains(read, text), nil
	}, concurrency.WithConcurrency[domain.Item](TextConcurrency), concurrency.WithStopOnError[domain.Item]())
	if err != nil {
		return nil, err
	}
	var out []domain.Item
	for i, ok := range found {
		if ok {
			out = append(out, items[i])
		}
	}
	return out, nil
}

func matches(tree *domain.Tree, it domain.Item, q port.FindQuery, under domain.ItemID, pattern *regexp.Regexp) bool {
	if !q.All && tree.IsTrashed(it.ID) {
		return false
	}
	if under != domain.RootID && (it.ID == under || !tree.IsWithin(it.ID, under)) {
		return false
	}
	if q.Starred && !it.Pinned {
		return false
	}
	for _, tag := range q.Tags {
		if !slices.Contains(it.Tags, tag) {
			return false
		}
	}
	return pattern == nil || pattern.MatchString(tree.Path(it.ID))
}
