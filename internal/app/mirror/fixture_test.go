// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package mirror_test

import (
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/mirror"
	"github.com/altessa-s/rmctl/internal/app/render"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

// dir is the mirrored directory of every fixture.
const dir = "/m"

// now is the time of a fixture's passes: files written before it are
// settled.
var now = porttest.Epoch.Add(time.Hour) //nolint:gochecknoglobals // a fixed test instant

// fixture is a mirror engine over an in-memory cloud and file system.
type fixture struct {
	t        *testing.T
	cloud    *porttest.Cloud
	files    *porttest.MemFiles
	disk     *porttest.MirrorDisk
	pdf      porttest.PDF
	renderer *porttest.Renderer
	clock    interface {
		Now() time.Time
	}
	engine *mirror.Engine
	spec   port.MirrorSpec
}

// setup is a fixture whose library holds the folder /Papers (f0) and docs,
// mirrored from /m; renders false is rmctl-lite.
func setup(t *testing.T, renders bool, docs ...porttest.Doc) *fixture {
	t.Helper()
	c := porttest.NewCloud()
	c.Add(append([]porttest.Doc{{ID: "f0aaaaaa", Name: "Papers", Folder: true}}, docs...)...)
	files := porttest.NewMemFiles()
	if err := files.MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	c.ReadLocal = func(path string) ([]byte, error) {
		b, ok := files.File(path)
		if !ok {
			return nil, porttest.ErrNoFile
		}
		return b, nil
	}
	f := &fixture{
		t: t, cloud: c, files: files, disk: porttest.NewMirrorDisk(files),
		pdf: porttest.PDF{Counts: map[string]int{}}, renderer: &porttest.Renderer{},
		spec: port.MirrorSpec{Name: "papers", Dir: dir, Folder: "/Papers"},
	}
	f.engine = f.build(renders, porttest.NewClock(now))
	return f
}

// build is an engine of the fixture's doubles on timer.
func (f *fixture) build(renders bool, timer port.Timer) *mirror.Engine {
	f.clock = timer
	return mirror.NewEngine(f.deps(renders, timer))
}

// deps are the engine's dependencies over the fixture's doubles.
func (f *fixture) deps(renders bool, timer port.Timer) mirror.Deps {
	lib := library.New(f.cloud, f.cloud, f.cloud)
	archiver := porttest.NewArchiver(f.files)
	clk := porttest.NewClock(now)
	return mirror.Deps{
		Library:   lib,
		Writer:    write.New(lib, f.cloud, f.cloud, f.files, f.pdf, archiver, porttest.NewIDs("n"), clk),
		Rendering: render.New(lib, f.cloud, f.files, archiver, archiver, f.renderer),
		Fetcher:   fetch.New(lib, f.cloud, f.files, archiver, clk),
		Files:     f.files, Disk: f.disk, Notifier: f.cloud, Dirs: porttest.NewDirWatcher(false), Timer: timer, Renders: renders,
	}
}

// pdfFile writes a local PDF of pages pages at rel, written at when.
func (f *fixture) pdfFile(rel, body string, pages int, when time.Time) {
	f.t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := f.files.MkdirAll(filepath.Dir(path)); err != nil {
		f.t.Fatal(err)
	}
	f.files.Write(path, []byte(body))
	f.pdf.Counts[path] = pages
	if !when.IsZero() {
		if err := f.files.SetModTime(path, when); err != nil {
			f.t.Fatal(err)
		}
	}
}

// epubFile writes a local EPUB at rel.
func (f *fixture) epubFile(rel, body string) {
	f.t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := f.files.MkdirAll(filepath.Dir(path)); err != nil {
		f.t.Fatal(err)
	}
	f.files.Write(path, []byte("PK\x03\x04"+body))
}

// pass makes a pass with opts and fails the test on a pass-level error.
func (f *fixture) pass(opts port.MirrorRunOptions) port.MirrorPass {
	f.t.Helper()
	p, err := f.engine.Pass(f.t.Context(), f.spec, opts)
	if err != nil {
		f.t.Fatalf("pass: %v\n%s", err, actions(p))
	}
	return p
}

// actions lists a pass's actions as "kind path [to]".
func actions(p port.MirrorPass) string {
	var out []string
	for _, a := range p.Actions {
		s := string(a.Kind) + " " + a.Path
		if a.To != "" {
			s += " -> " + a.To
		}
		if a.Err != nil {
			s += " !" + a.Err.Error()
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}

// want fails unless the pass did exactly the actions want.
func want(t *testing.T, p port.MirrorPass, lines ...string) {
	t.Helper()
	if got := actions(p); got != strings.Join(lines, "\n") {
		t.Fatalf("actions:\n%s\nwant:\n%s", got, strings.Join(lines, "\n"))
	}
}

// local is the content of the local file at rel, and whether it is there.
func (f *fixture) local(rel string) (string, bool) {
	b, ok := f.files.File(filepath.Join(dir, filepath.FromSlash(rel)))
	return string(b), ok
}

// doc is the document of name below /Papers, at any depth.
func (f *fixture) doc(name string) (domain.Item, bool) {
	f.t.Helper()
	tree, _, err := library.New(f.cloud, f.cloud, f.cloud).Load(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	for _, it := range tree.Items() {
		if it.Name == name && !it.Kind.IsFolder() {
			return it, true
		}
	}
	return domain.Item{}, false
}

// path is the library path of the item id, and whether it is in the trash.
func (f *fixture) path(id domain.ItemID) (string, bool) {
	f.t.Helper()
	tree, _, err := library.New(f.cloud, f.cloud, f.cloud).Load(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return tree.Path(id), tree.IsTrashed(id)
}

// replace swaps the document d in for the record of its identifier, as a
// change on the tablet does.
func (f *fixture) replace(d porttest.Doc) {
	f.t.Helper()
	records := f.cloud.Records()
	i := slices.IndexFunc(records, func(r domain.Record) bool { return r.ID == d.ID })
	if i < 0 {
		f.t.Fatalf("no record %s", d.ID)
	}
	records[i] = f.cloud.Record(d)
	f.cloud.SetRoot(records)
}

// drop removes the record id from the root, as a deletion on the tablet.
func (f *fixture) drop(id domain.ItemID) {
	f.cloud.SetRoot(slices.DeleteFunc(f.cloud.Records(), func(r domain.Record) bool { return r.ID == id }))
}

// journal is the directory's journal, decoded loosely.
func (f *fixture) journal() map[string]any {
	f.t.Helper()
	b, ok := f.files.File(filepath.Join(dir, domain.MirrorJournalName))
	if !ok {
		return nil
	}
	var j map[string]any
	if err := json.Unmarshal(b, &j); err != nil {
		f.t.Fatal(err)
	}
	return j
}

// items are the journal's entries.
func (f *fixture) items() map[string]any {
	j := f.journal()
	if j == nil {
		return nil
	}
	items, _ := j["items"].(map[string]any)
	return items
}
