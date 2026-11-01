//go:build integration

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/cloud"
	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// TestIntegration_RegisterWriteReadNotify runs the adapter end to end on a
// fresh account: register, write a folder, read it back, meet a conflict,
// and hear the write on the notification socket.
func TestIntegration_RegisterWriteReadNotify(t *testing.T) {
	t.Parallel()
	double := clouddouble.New(t)
	c := cloud.New(double.Client(t, clouddouble.NewStore()))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	if err := c.Register(ctx, clouddouble.ValidCode); err != nil {
		t.Fatal(err)
	}
	events := make(chan port.Event, 16)
	listening := make(chan error, 1)
	go func() { listening <- c.Listen(ctx, func(e port.Event) { events <- e }) }()
	double.WaitSocket(t)

	base, err := c.Root(ctx)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := c.Upload(ctx, "f-1.metadata", []byte(`{"type":"CollectionType","visibleName":"Inbox"}`))
	if err != nil {
		t.Fatal(err)
	}
	content, err := c.Upload(ctx, "f-1.content", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := c.PutIndex(ctx, "f-1", []domain.File{meta, content})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitRoot(ctx, base, []domain.Record{rec}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitRoot(ctx, base, []domain.Record{rec}); !errors.Is(err, port.ErrRootConflict) {
		t.Fatalf("a second write on the old generation: %v", err)
	}

	state, err := c.Root(ctx)
	if err != nil || len(state.Records) != 1 || state.Records[0].ID != "f-1" {
		t.Fatalf("%+v %v", state, err)
	}
	files, err := c.Files(ctx, state.Records[0])
	if err != nil || len(files) != 2 {
		t.Fatalf("%+v %v", files, err)
	}
	got, err := c.Read(ctx, files[0])
	if err != nil || len(got) == 0 {
		t.Fatalf("%q %v", got, err)
	}

	deadline := time.After(clouddouble.Slack)
	for library := false; !library; {
		select {
		case e := <-events:
			library = e.Kind == port.EventLibrary && e.SyncComplete
		case <-deadline:
			t.Fatal("the write was not heard on the notification socket")
		}
	}
	cancel()
	if err := <-listening; err != nil {
		t.Fatal(err)
	}
}
