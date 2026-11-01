// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package watch_test

import (
	"context"
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/watch"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestWatchDeliversUntilCanceled(t *testing.T) {
	t.Parallel()
	c := porttest.NewCloud()
	c.Events = []port.Event{{Kind: port.EventConnected}, {Kind: port.EventLibrary, Name: "SyncComplete", SyncComplete: true}}
	ctx, cancel := context.WithCancel(t.Context())
	var got []port.Event
	err := watch.New(c).Watch(ctx, func(e port.Event) {
		got = append(got, e)
		if len(got) == 2 {
			cancel()
		}
	})
	if err != nil || len(got) != 2 || !got[1].SyncComplete {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestWatchFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	c := porttest.NewCloud()
	c.Fail["Listen"] = boom
	if err := watch.New(c).Watch(t.Context(), nil); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestNilCallbackAndPanics(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := porttest.NewCloud()
	c.Events = []port.Event{{Kind: port.EventConnected}}
	if err := watch.New(c).Watch(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	watch.New(nil)
}
