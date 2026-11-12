//go:build integration && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/wiring"
)

// A command that reads the library leaves the listing completion reads;
// a move leaves the listing of the root it committed.
func TestIntegration_ListingFollowsTheLibrary(t *testing.T) {
	cloud := clouddouble.New(t)
	opts := options(t)
	opts.Client = cloud.Options()
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err = svc.Account.Register(t.Context(), clouddouble.ValidCode, false); err != nil {
		t.Fatal(err)
	}
	cloud.Seed(t,
		clouddouble.Item{ID: "f0000000-0000-4000-8000-0000000000aa", Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: "a0000000-0000-4000-8000-0000000000aa", Name: "Notes", Kind: clouddouble.Notebook},
	)
	if _, err = svc.Browser.Tree(t.Context()); err != nil {
		t.Fatal(err)
	}
	c, err := wiring.Completion(opts)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := c.Listing(t.Context())
	if err != nil || tree.Len() != 2 {
		t.Fatalf("%v", err)
	}
	if _, err = svc.Writer.Move(t.Context(), "/Notes", "/Books"); err != nil {
		t.Fatal(err)
	}
	if tree, err = c.Listing(t.Context()); err != nil || tree.Path("a0000000-0000-4000-8000-0000000000aa") != "/Books/Notes" {
		t.Fatalf("the listing does not follow the move: %v", err)
	}
}
