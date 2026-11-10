// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestMirrorDisk(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	if err := files.MkdirAll("/m/sub"); err != nil {
		t.Fatal(err)
	}
	files.Write("/m/a.pdf", []byte("aa"))
	files.Write("/m/sub/b.pdf", []byte("b"))
	files.Write("/m/skip/c.pdf", []byte("c"))
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := files.SetModTime("/m/a.pdf", at); err != nil {
		t.Fatal(err)
	}
	d := porttest.NewMirrorDisk(files)
	got, err := d.Scan("/m", func(rel string) bool { return rel == "skip" })
	if err != nil || len(got) != 2 || got[0] != (port.LocalEntry{Path: "a.pdf", Size: 2, ModTime: at}) || got[1].Path != "sub/b.pdf" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ = d.Scan("/m", nil); len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if _, err = d.Scan("/nowhere", nil); !errors.Is(err, porttest.ErrNoFile) {
		t.Fatal(err)
	}
	files.Fail["Walk"] = errBoom
	if _, err = d.Scan("/m", nil); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	d.Fail["Scan"] = errBoom
	if _, err = d.Scan("/m", nil); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	release, err := d.Lock("/m/.lock")
	if err != nil || !d.Held("/m/.lock") {
		t.Fatal(err)
	}
	if _, err = d.Lock("/m/.lock"); !errors.Is(err, port.ErrMirrorBusy) {
		t.Fatal(err)
	}
	if err = release(); err != nil || d.Held("/m/.lock") {
		t.Fatal(err)
	}
	d.Fail["Lock"] = errBoom
	if _, err = d.Lock("/m/.lock"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestTimer(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	timer := porttest.NewTimer(at)
	done := make(chan error, 2)
	go func() { done <- timer.Sleep(t.Context(), time.Second) }()
	go func() { done <- timer.Sleep(t.Context(), time.Minute) }()
	if !timer.WaitSleepers(t.Context(), 2) || timer.Sleepers() != 2 {
		t.Fatal("no sleepers")
	}
	timer.Wake()
	if err := errors.Join(<-done, <-done); err != nil || !timer.Now().Equal(at.Add(time.Minute)) {
		t.Fatal(timer.Now(), err)
	}
	timer.Jump(time.Hour)
	if !timer.Now().Equal(at.Add(time.Hour + time.Minute)) {
		t.Fatal(timer.Now())
	}
	ctx, cancel := context.WithCancel(t.Context())
	go func() { done <- timer.Sleep(ctx, time.Second) }()
	if !timer.WaitSleepers(t.Context(), 1) {
		t.Fatal("no sleeper")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || timer.Sleepers() != 0 {
		t.Fatal(err)
	}
	if timer.WaitSleepers(ctx, 1) {
		t.Fatal("a sleeper that is not there")
	}
}
