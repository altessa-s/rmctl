// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
)

func TestTransferLine(t *testing.T) {
	t.Parallel()
	p := port.SyncProgress{Files: 120, FilesTotal: 310, Bytes: 9 << 20, BytesTotal: 20 << 20}
	if got, want := TransferLine("syncing", p, 3*time.Second),
		"syncing [=========>          ]  45%  9.0 MiB/20.0 MiB  120/310 files  3.0 MiB/s  0:04 left"; got != want {
		t.Fatalf("\n%q\n%q", got, want)
	}
	// No rate before a second, none of nothing, and no time left at the end.
	if got := TransferLine("syncing", p, 500*time.Millisecond); strings.Contains(got, "/s") {
		t.Fatal(got)
	}
	done := port.SyncProgress{Files: 2, FilesTotal: 2, Bytes: 10, BytesTotal: 10}
	if got := TransferLine("syncing", done, time.Hour); strings.Contains(got, "left") || !strings.Contains(got, "[====================] 100%") {
		t.Fatal(got)
	}
	// Nothing to fetch in bytes is all done.
	if got := TransferLine("syncing", port.SyncProgress{FilesTotal: 1}, 0); !strings.Contains(got, "100%") {
		t.Fatal(got)
	}
	if clockTime(3*time.Hour+4*time.Minute+5*time.Second) != "3:04:05" || clockTime(65*time.Second) != "1:05" {
		t.Fatal(clockTime(65 * time.Second))
	}
}

func TestTransferRedrawsAtMostEveryTenth(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	update, done := Transfer(Streams{Err: &buf, IsTerminal: true, Now: func() time.Time { return now }}, "syncing")
	p := port.SyncProgress{FilesTotal: 3, BytesTotal: 30}
	update(p)
	p.Bytes = 5
	update(p) // within the tenth: skipped
	now = now.Add(200 * time.Millisecond)
	p.Bytes = 10
	update(p)
	done()
	done()
	if n := strings.Count(buf.String(), "\r"); n != 2 || !strings.HasSuffix(buf.String(), "\n") || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("%q", buf.String())
	}
	if update, done := Transfer(Streams{Err: &buf}, "syncing"); update != nil {
		t.Fatal("a progress bar off a terminal")
	} else {
		done()
	}
}
