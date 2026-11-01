// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package browse

import (
	"context"
	"fmt"

	"github.com/altessa-s/rmctl/internal/domain"
)

// folder is the folder a query names: "" and "/" are the top level, and a
// document is refused.
func folder(tree *domain.Tree, query string, all bool) (domain.ItemID, error) {
	if query == "" || query == "/" {
		return domain.RootID, nil
	}
	it, found, err := tree.Resolve(query, all)
	switch {
	case err != nil:
		return "", err
	case !found:
		return domain.RootID, nil
	case !it.Kind.IsFolder():
		return "", fmt.Errorf("%s: %w", query, domain.ErrNotAFolder)
	}
	return it.ID, nil
}

// Usage implements port.Browser. It reads only the library's indexes, so it
// answers from the cache.
func (s *Service) Usage(ctx context.Context, query string, depth int, all bool) (*domain.Tree, domain.Usage, error) {
	tree, err := s.Tree(ctx)
	if err != nil {
		return nil, domain.Usage{}, err
	}
	root, err := folder(tree, query, all)
	if err != nil {
		return nil, domain.Usage{}, err
	}
	return tree, tree.Usage(root, max(depth, 0), all), nil
}

// Duplicates implements port.Browser. It compares the originals' hashes in
// the indexes, so it answers from the cache and downloads nothing.
func (s *Service) Duplicates(ctx context.Context, query string, all bool) (*domain.Tree, []domain.DuplicateGroup, error) {
	tree, err := s.Tree(ctx)
	if err != nil {
		return nil, nil, err
	}
	root, err := folder(tree, query, all)
	if err != nil {
		return nil, nil, err
	}
	return tree, tree.Duplicates(root, all), nil
}
