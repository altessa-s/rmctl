// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package render_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/render"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// viewing is the open use case over the render fixture.
type viewing struct {
	fixture
	v        *render.Viewer
	previews *porttest.Previews
	opener   *porttest.Opener
}

func setupViewer(t *testing.T) viewing {
	t.Helper()
	f := setup(t)
	p, o := &porttest.Previews{}, &porttest.Opener{}
	return viewing{fixture: f, v: render.NewViewer(f.s, p, o, "1.2.3"), previews: p, opener: o}
}

// drawn is the first line of the drawing kept at path.
func (v viewing) drawn(t *testing.T, path string) string {
	t.Helper()
	data, ok := v.previews.File(path)
	if !ok {
		t.Fatalf("%s is not kept", path)
	}
	head, _, _ := strings.Cut(string(data), "\n")
	return head
}

func TestOpenDrawsOnceAndShows(t *testing.T) {
	t.Parallel()
	v := setupViewer(t)
	report, err := v.v.Open(t.Context(), "/Books/Notes", port.RenderOptions{})
	if err != nil || !report.Drawn || report.Pages != 3 || !strings.HasSuffix(report.Path, "/Notes-rendered.pdf") {
		t.Fatalf("%+v %v", report, err)
	}
	if head := v.drawn(t, report.Path); head != "PDF d4aaaaaa fresh a4" {
		t.Fatal(head)
	}
	downloads := v.cloud.Count("Download") + v.cloud.Count("Read")
	again, err := v.v.Open(t.Context(), "d4", port.RenderOptions{Paper: port.PaperA4})
	if err != nil || again.Drawn || again.Path != report.Path || again.Pages != 0 {
		t.Fatalf("%+v %v", again, err)
	}
	if n := v.cloud.Count("Download") + v.cloud.Count("Read"); n != downloads {
		t.Fatalf("a kept drawing downloaded %d files", n-downloads)
	}
	if opened := v.opener.Opened(); len(opened) != 2 || opened[1] != report.Path {
		t.Fatalf("%v", opened)
	}
	// Other options are another drawing.
	page, err := v.v.Open(t.Context(), "d4", port.RenderOptions{Page: 2})
	if err != nil || !page.Drawn || !strings.HasSuffix(page.Path, "/Notes-p2.png") || page.Path == report.Path {
		t.Fatalf("%+v %v", page, err)
	}
	if v.previews.Draws() != 2 {
		t.Fatalf("%d drawings", v.previews.Draws())
	}
}

// A new version of the document, or of rmctl, is drawn again.
func TestOpenDrawsAgainWhenAnythingChanged(t *testing.T) {
	t.Parallel()
	v := setupViewer(t)
	first, err := v.v.Open(t.Context(), "d4", port.RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	newer := render.NewViewer(v.s, v.previews, v.opener, "1.2.4")
	if second, err := newer.Open(t.Context(), "d4", port.RenderOptions{}); err != nil || !second.Drawn || second.Path == first.Path {
		t.Fatalf("another rmctl kept the drawing: %+v %v", second, err)
	}
	records := v.cloud.Records()
	for i, r := range records {
		if r.ID == "d4aaaaaa" {
			records[i] = v.cloud.Record(porttest.Doc{ID: "d4aaaaaa", Name: "Notes", Parent: "f1aaaaaa", Pages: [][]byte{[]byte("one"), []byte("changed")}})
		}
	}
	v.cloud.SetRoot(records)
	if third, err := v.v.Open(t.Context(), "d4", port.RenderOptions{}); err != nil || !third.Drawn || third.Path == first.Path {
		t.Fatalf("a changed document kept its drawing: %+v %v", third, err)
	}
}

func TestOpenFile(t *testing.T) {
	t.Parallel()
	v := setupViewer(t)
	archive(t, v.fixture, "one.rmdoc", map[domain.ItemID]string{"a1": "Meeting"})
	report, err := v.v.OpenFile(t.Context(), "one.rmdoc", port.RenderOptions{})
	if err != nil || !report.Drawn || !strings.HasSuffix(report.Path, "/Meeting-rendered.pdf") {
		t.Fatalf("%+v %v", report, err)
	}
	if head := v.drawn(t, report.Path); head != "PDF a1 fresh a4" {
		t.Fatal(head)
	}
	if again, _ := v.v.OpenFile(t.Context(), "one.rmdoc", port.RenderOptions{}); again.Drawn {
		t.Fatal("an unchanged archive was drawn again")
	}
	if err = v.files.SetModTime("one.rmdoc", time.Unix(1_800_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	if again, _ := v.v.OpenFile(t.Context(), "one.rmdoc", port.RenderOptions{}); !again.Drawn {
		t.Fatal("an archive written since kept its drawing")
	}
	if n := v.cloud.Count("Root"); n != 0 {
		t.Fatalf("%d calls to the cloud", n)
	}
	if _, err = v.v.OpenFile(t.Context(), "none.rmdoc", port.RenderOptions{}); err == nil {
		t.Fatal("a missing archive opened")
	}
	for _, method := range []string{"Stat", "ModTime"} {
		v.files.Fail[method] = errBoom
		if _, err = v.v.OpenFile(t.Context(), "one.rmdoc", port.RenderOptions{}); !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", method, err)
		}
		delete(v.files.Fail, method)
	}
}

// A drawing the viewer could not show is still reported, with the error.
func TestOpenWithoutAViewer(t *testing.T) {
	t.Parallel()
	v := setupViewer(t)
	v.opener.Fail = errBoom
	report, err := v.v.Open(t.Context(), "d4", port.RenderOptions{})
	if !errors.Is(err, errBoom) || report.Path == "" || !report.Drawn {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestOpenFailures(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		set   func(viewing)
		query string
		opts  port.RenderOptions
		want  error
	}{
		{"options", func(viewing) {}, "d4", port.RenderOptions{Page: -1}, render.ErrBadOptions},
		{"lite", func(v viewing) { v.renderer.Unavailable = true }, "d4", port.RenderOptions{}, domain.ErrRenderUnsupported},
		{"folder", func(viewing) {}, "/Books", port.RenderOptions{}, render.ErrFolder},
		{"name", func(v viewing) { v.archiver.Fail["CheckName"] = errBoom }, "d4", port.RenderOptions{}, errBoom},
		{"download", func(v viewing) { v.cloud.Fail["Download"] = errBoom }, "d4", port.RenderOptions{}, errBoom},
		{"draw", func(v viewing) { v.renderer.Fail = errBoom }, "d4", port.RenderOptions{}, errBoom},
		{"previews", func(v viewing) { v.previews.Fail = errBoom }, "d4", port.RenderOptions{}, errBoom},
	} {
		v := setupViewer(t)
		c.set(v)
		if report, err := v.v.Open(t.Context(), c.query, c.opts); !errors.Is(err, c.want) || report.Path != "" {
			t.Errorf("%s: %+v %v", c.name, report, err)
		}
		if len(v.opener.Opened()) != 0 {
			t.Errorf("%s: a failure was shown", c.name)
		}
	}
	v := setupViewer(t)
	v.renderer.Unavailable = true
	if _, err := v.v.OpenFile(t.Context(), "x.rmdoc", port.RenderOptions{}); !errors.Is(err, domain.ErrRenderUnsupported) {
		t.Fatal(err)
	}
}

func TestNewViewerPanics(t *testing.T) {
	t.Parallel()
	f := setup(t)
	for name, build := range map[string]func(){
		"service":  func() { render.NewViewer(nil, &porttest.Previews{}, &porttest.Opener{}, "") },
		"previews": func() { render.NewViewer(f.s, nil, &porttest.Opener{}, "") },
		"opener":   func() { render.NewViewer(f.s, &porttest.Previews{}, nil, "") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			build()
		}()
	}
}
