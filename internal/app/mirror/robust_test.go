// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/mirror"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// The names of one document in the two Unicode forms: the tablet writes
// NFC, macOS often gives NFD.
const (
	cafeNFC = "Café"
	cafeNFD = "Café"
)

// A name is one name in either Unicode form: a file in NFD pairs with the
// document in NFC rather than going up again, a journal path that comes
// back in the other form is the same file, and a document renamed into the
// other form moves nothing.
func TestPassUnicodeForms(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: cafeNFC, Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF cafe")})
	f.pdfFile(cafeNFD+".pdf", "%PDF cafe", 1, time10)
	want(t, f.pass(port.MirrorRunOptions{}), "adopt "+cafeNFD+".pdf")
	// The file system gives the name back in the other form.
	if err := f.files.Rename(filepath.Join(dir, cafeNFD+".pdf"), filepath.Join(dir, cafeNFC+".pdf")); err != nil {
		t.Fatal(err)
	}
	want(t, f.pass(port.MirrorRunOptions{}))
	// The tablet writes the name in the other form: no rename.
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: cafeNFD, Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF cafe")})
	want(t, f.pass(port.MirrorRunOptions{}))
	if _, ok := f.local(cafeNFC + ".pdf"); !ok || len(f.cloud.Records()) != 2 {
		t.Fatalf("the file moved, or a document was made: %d records", len(f.cloud.Records()))
	}
}

// hiding is a disk whose listing leaves out one file, as a listing made
// while an application saves it, deleted and not yet written anew.
type hiding struct {
	port.MirrorFiles
	hide string
}

func (h hiding) Scan(dir string, prune func(string) bool) ([]port.LocalEntry, error) {
	all, err := h.MirrorFiles.Scan(dir, prune)
	return slices.DeleteFunc(all, func(e port.LocalEntry) bool { return e.Path == h.hide }), err
}

// A file the listing missed but that is there when its document would go
// to the trash is no deletion: the document stays, and the pass leaves it
// pending.
func TestPassTrashesNoFileThatIsBack(t *testing.T) {
	t.Parallel()
	f := setup(t, true,
		porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one"), RM: true},
		porttest.Doc{ID: "d2aaaaaa", Name: "Other", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF other")},
	)
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	f.pdfFile("Other.pdf", "%PDF other", 1, time10)
	f.pass(port.MirrorRunOptions{})
	d := f.deps(true, porttest.NewClock(now))
	d.Disk = hiding{MirrorFiles: f.disk, hide: "Paper.pdf"}
	p, err := mirror.NewEngine(d).Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if err != nil || p.Pending != 1 {
		t.Fatalf("%v %+v", err, p)
	}
	want(t, p, "skip Paper.pdf")
	if _, trashed := f.path("d1aaaaaa"); trashed {
		t.Fatal("the document of a file still there went to the trash")
	}
	want(t, f.pass(port.MirrorRunOptions{}))
}

// A move onto a file written since the listing fails, and replaces nothing.
func TestPassMoveNeverReplaces(t *testing.T) {
	t.Parallel()
	f := setup(t, true, porttest.Doc{ID: "d1aaaaaa", Name: "Paper", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one")})
	f.pdfFile("Paper.pdf", "%PDF one", 1, time10)
	f.pass(port.MirrorRunOptions{})
	f.replace(porttest.Doc{ID: "d1aaaaaa", Name: "Renamed", Parent: "f0aaaaaa", Ext: "pdf", Body: []byte("%PDF one")})
	d := f.deps(true, porttest.NewClock(now))
	d.Disk = hiding{MirrorFiles: f.disk, hide: "Renamed.pdf"}
	f.pdfFile("Renamed.pdf", "%PDF mine", 1, time10)
	p, err := mirror.NewEngine(d).Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if err == nil || len(p.Actions) != 1 || p.Actions[0].Kind != port.MirrorMoveLocal || p.Actions[0].Err == nil {
		t.Fatalf("%v\n%s", err, actions(p))
	}
	if got, _ := f.local("Renamed.pdf"); got != "%PDF mine" {
		t.Fatalf("replaced: %q", got)
	}
}

// putHook is the write use case with a hook before every Put.
type putHook struct {
	port.Writer
	before func(n int)
	puts   int
}

func (w *putHook) Put(ctx context.Context, local, dest string, opts port.PutOptions, progress port.Progress) (domain.Item, error) {
	w.puts++
	w.before(w.puts)
	return w.Writer.Put(ctx, local, dest, opts, progress)
}

// A long pass records its uploads in the journal as it goes: one cut short
// before its end finds them there, not only by their names.
func TestPassCheckpointsTheJournal(t *testing.T) {
	t.Parallel()
	f := setup(t, true)
	f.pdfFile("A.pdf", "%PDF a", 1, time10)
	f.pdfFile("B.pdf", "%PDF b", 1, time10)
	timer := porttest.NewTimer(now)
	d := f.deps(true, timer)
	hook := &putHook{Writer: d.Writer}
	hook.before = func(n int) {
		if n == 2 && len(f.items()) != 1 {
			t.Errorf("the journal before the second upload: %v", f.items())
		}
		timer.Jump(time.Minute)
	}
	d.Writer = hook
	p, err := mirror.NewEngine(d).Pass(t.Context(), f.spec, port.MirrorRunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want(t, p, "upload A.pdf", "upload B.pdf")
	if len(f.items()) != 2 {
		t.Fatalf("journal: %v", f.items())
	}
}

// A mirror config.toml defines over the directory or the folder of
// another, as a hand edit can, is not run: each would take the other's
// files for its own.
func TestRunRefusesOverlappingMirrors(t *testing.T) {
	t.Parallel()
	s := newService(t)
	s.config.set(
		port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"},
		port.MirrorSpec{Name: "inner", Dir: "/a/sub", Folder: "/B"},
		port.MirrorSpec{Name: "folder", Dir: "/c", Folder: "/A/x"},
		port.MirrorSpec{Name: "other", Dir: "/b", Folder: "/A/x", Profile: "work"},
	)
	var reported []string
	err := s.s.Run(t.Context(), port.MirrorSelection{All: true}, port.MirrorRunOptions{}, func(p port.MirrorPass) {
		if errors.Is(p.Err, port.ErrMirrorExists) {
			reported = append(reported, p.Mirror)
		}
	})
	if !errors.Is(err, port.ErrMirrorExists) || slices.Compare(reported, []string{"inner", "folder"}) != 0 {
		t.Fatal(err, reported)
	}
	if got := s.engines.open[""].passes; slices.Compare(got, []string{":a"}) != 0 {
		t.Fatal(got)
	}
	// Watched, the refused mirror is said once and not started.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.config.set(port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A"}, port.MirrorSpec{Name: "inner", Dir: "/a/sub", Folder: "/B"})
	refused := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		done <- s.s.Run(ctx, port.MirrorSelection{All: true}, port.MirrorRunOptions{Watch: true}, func(p port.MirrorPass) {
			if p.Err != nil {
				refused <- p.Mirror
			}
		})
	}()
	if got := recv(t, refused); got != "inner" {
		t.Fatal(got)
	}
	if got := recv(t, s.engines.watched); got != "a@/A" {
		t.Fatal(got)
	}
	if !s.timer.WaitSleepers(ctx, 1) {
		t.Fatal("no sleeper")
	}
	s.timer.Wake()
	if !s.timer.WaitSleepers(ctx, 1) {
		t.Fatal("no sleeper")
	}
	select {
	case got := <-refused:
		t.Fatalf("said again: %s", got)
	default:
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

// A watch whose profile's engine cannot open yet tries again, with the
// growing wait, until it opens.
func TestRunWatchRetriesTheEngine(t *testing.T) {
	t.Parallel()
	s := newService(t)
	s.config.set(port.MirrorSpec{Name: "a", Dir: "/a", Folder: "/A", Profile: "late"})
	s.engines.failOpens = 2
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failed := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		done <- s.s.Run(ctx, port.MirrorSelection{Names: []string{"a"}}, port.MirrorRunOptions{Watch: true}, func(p port.MirrorPass) {
			if p.Err != nil {
				failed <- p.Mirror
			}
		})
	}()
	for range 2 {
		recv(t, failed)
		if !s.timer.WaitSleepers(ctx, 1) {
			t.Fatal("no sleeper")
		}
		s.timer.Wake()
	}
	if got := recv(t, s.engines.watched); got != "a@/A" {
		t.Fatal(got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
