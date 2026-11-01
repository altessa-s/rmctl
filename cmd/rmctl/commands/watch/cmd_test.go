// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package watch

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

func build(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{New(env)} }

var events = []port.Event{
	{Kind: port.EventConnected},
	{Kind: port.EventLibrary, Name: "SyncComplete", SyncComplete: true, DocumentID: "d-1", VisibleName: "Secret plan.pdf",
		Frame: []byte(`{"message":{"attributes":{"event":"SyncComplete","id":"d-1","vissibleName":"Secret plan.pdf"},"messageid":"m-1"}}`)},
	{Kind: port.EventLibrary, Name: "DocAdded", DocumentID: "d-2", VisibleName: "Diary", FromThisDevice: true},
	{Kind: port.EventUnreadable, Frame: []byte("not json")},
	{Kind: port.EventDropped, Err: errors.New("eof")},
	{Kind: port.EventDropped},
	{Kind: port.EventKind(99)},
}

func TestWatchMasksNames(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Events: events, WatchErr: errors.New("socket gone")}
	r := clitest.Exec(t, f.Services(), clitest.Options{}, build, "watch")
	if r.Code != cliutil.CodeError || !strings.HasPrefix(r.Error.Error(), "watch: ") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if strings.Contains(r.Out, "Secret") || !strings.Contains(r.Out, "\tSyncComplete\td-1\t<name:11>.pdf\tfalse\t-\n") ||
		!strings.Contains(r.Out, "\tDocAdded\td-2\t<name:5>\ttrue\t-\n") ||
		!strings.Contains(r.Out, "\tunreadable frame\t-\t-\t-\tnot json\n") {
		t.Fatalf("out %q", r.Out)
	}
	if r.Err != "connected\ndropped: eof — reconnecting\ndropped — reconnecting\n" {
		t.Fatalf("err %q", r.Err)
	}
}

// --raw prints every frame whole: the wrapper and the messageid included,
// names unmasked. An event with no frame prints "-" for it.
func TestWatchRaw(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{Events: events, WatchErr: errors.New("socket gone")}
	r := clitest.Exec(t, f.Services(), clitest.Options{}, build, "watch", "--raw")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Out, `"messageid":"m-1"`) || !strings.Contains(r.Out, "Secret plan.pdf") ||
		!strings.Contains(r.Out, "\tunreadable frame\tnot json\n") || !strings.Contains(r.Out, "\tDocAdded\t-\n") {
		t.Fatalf("%d %v out %q", r.Code, r.Error, r.Out)
	}
}

func TestWatchRawJSON(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	em, err := output.Stream(&out, output.JSONRenderer{}, columns(true), output.Style{})
	if err != nil {
		t.Fatal(err)
	}
	p := printer{em: em, errOut: &bytes.Buffer{}, raw: true, now: func() time.Time { return time.Unix(0, 0) }}
	for _, e := range events {
		p.print(e)
	}
	if err := em.End(nil); err != nil {
		t.Fatal(err)
	}
	clitest.Golden(t, "watch", out.String())
}

func TestWatchEndsOnInterrupt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cliutil.ErrInterrupted)
	r := clitest.Exec(t, (&clitest.Fake{}).Services(), clitest.Options{Context: ctx}, build, "watch")
	if r.Code != cliutil.CodeInterrupted {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestWatchUsage(t *testing.T) {
	t.Parallel()
	if r := clitest.Exec(t, (&clitest.Fake{}).Services(), clitest.Options{}, build, "watch", "x"); r.Code != cliutil.CodeUsage {
		t.Fatal(r.Code)
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, build, "watch"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}
