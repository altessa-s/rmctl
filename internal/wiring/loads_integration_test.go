//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/wiring"
)

// loads records what the library tells of its loads.
type loads struct {
	mu    sync.Mutex
	calls [][2]int
	ended int
}

func (l *loads) LoadProgress(done, total int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, [2]int{done, total})
}

func (l *loads) LoadDone() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ended++
}

func (l *loads) seen() ([][2]int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.calls), l.ended
}

// The observer of the options follows a load from the cloud through every
// use case that shares the library, hears nothing of a second read in the
// same run, and nothing at all of an offline run over the same cache.
func TestIntegration_LoadsAreObserved(t *testing.T) {
	cloud := clouddouble.New(t)
	opts := options(t)
	opts.Client = cloud.Options()
	online := &loads{}
	opts.Loads = online
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
	want := [][2]int{{0, 2}, {1, 2}, {2, 2}}
	if calls, ended := online.seen(); !slices.Equal(calls, want) || ended != 1 {
		t.Fatalf("the first read told %v, %d ended", calls, ended)
	}
	// The fetcher shares the library: what the browser read is known.
	if _, err = svc.Fetcher.Get(t.Context(), "/Missing", t.TempDir(), nil); err == nil {
		t.Fatal("a missing document was downloaded")
	}
	if calls, ended := online.seen(); len(calls) != len(want) || ended != 1 {
		t.Fatalf("a second read told %v, %d ended", calls, ended)
	}

	on := true
	opts.Offline = &on
	offline := &loads{}
	opts.Loads = offline
	cached, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cached.Close() })
	tree, err := cached.Browser.Tree(t.Context())
	if err != nil || tree.Len() != 2 {
		t.Fatalf("an offline read of the cache: %v", err)
	}
	if calls, ended := offline.seen(); len(calls) != 0 || ended != 0 {
		t.Fatalf("an offline read told %v, %d ended", calls, ended)
	}
}
