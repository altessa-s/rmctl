// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

// The sync cache keeps only bytes that are their hash's, counts what it
// holds against its budget, and remembers one sync at a time: the
// prefetch tests rest on each of these.
func TestSyncCache(t *testing.T) {
	t.Parallel()
	c := porttest.NewSyncCache(1 << 20)
	c.Source = port.BudgetFromConfig
	data := []byte("a page")
	h := porttest.Hash(data)
	if c.Holds(h) || c.Usage() != 0 || c.Budget() != 1<<20 || c.BudgetSource() != port.BudgetFromConfig {
		t.Fatalf("a new cache: %v %d %d %q", c.Holds(h), c.Usage(), c.Budget(), c.BudgetSource())
	}
	c.Put(h, []byte("not the page"))
	if _, ok := c.Get(h); ok {
		t.Fatal("Put kept bytes that are not the hash's")
	}
	c.Put(h, data)
	if got, ok := c.Get(h); !ok || string(got) != "a page" {
		t.Fatalf("%q %v", got, ok)
	}
	c.Drop(h)
	if c.Holds(h) {
		t.Fatal("a dropped blob is held")
	}

	write := func(b []byte) func(io.Writer) error {
		return func(w io.Writer) error { _, err := w.Write(b); return err }
	}
	if err := c.Fill(h, write([]byte("other"))); !errors.Is(err, porttest.ErrHashMismatch) || c.Holds(h) {
		t.Fatalf("a mismatched fill: %v", err)
	}
	if err := c.Fill(h, func(io.Writer) error { return errBoom }); !errors.Is(err, errBoom) {
		t.Fatalf("a failed write: %v", err)
	}
	if err := c.Fill(h, write(data)); err != nil || !c.Holds(h) || c.Fills() != 1 {
		t.Fatalf("%v %v %d", err, c.Holds(h), c.Fills())
	}
	c.FailFill = errBoom
	if err := c.Fill(h, write(data)); !errors.Is(err, errBoom) || c.Fills() != 1 {
		t.Fatalf("FailFill: %v", err)
	}

	c.Indexes["idx"] = 100
	if n, ok := c.IndexSize("idx"); !ok || n != 100 {
		t.Fatalf("%d %v", n, ok)
	}
	if _, ok := c.IndexSize("other"); ok {
		t.Fatal("an index that is not kept has a size")
	}
	if c.Usage() != int64(len(data))+100 {
		t.Fatalf("usage %d", c.Usage())
	}

	if _, ok := c.LastSync(); ok {
		t.Fatal("a sync before any")
	}
	mark := port.SyncMark{Root: "r1", Generation: 7, Query: "/", Files: true, Documents: 3}
	c.MarkSync(mark)
	if got, ok := c.LastSync(); !ok || got != mark {
		t.Fatalf("%+v %v", got, ok)
	}
	c.DropSync()
	if _, ok := c.LastSync(); ok {
		t.Fatal("a dropped sync is still the last")
	}

	// Every removal changes the generation and forgets the mark; a hook
	// runs before a mark is kept.
	if g, ok := c.Removals(); !ok || g != "1" {
		t.Fatalf("after one drop: %q %v", g, ok)
	}
	c.BeforeMark = func() { c.DropIndex("idx") }
	c.MarkSync(mark)
	if g, _ := c.Removals(); g != "2" {
		t.Fatalf("after the hook: %q", g)
	}
	if _, ok := c.LastSync(); !ok {
		t.Fatal("the mark after the hook was not kept")
	}
	c.UnknownRemovals = true
	if _, ok := c.Removals(); ok {
		t.Fatal("an unknown generation was told")
	}
}

func TestVault(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	seed := map[string]string{port.DeviceToken: "d"}
	v := porttest.NewVault(port.SecretsKeychain, seed)
	seed[port.DeviceToken] = "changed"
	if v.Backend() != port.SecretsKeychain || v.Location() != "in the keychain" || !slices.Contains(v.Names(), port.DeviceToken) {
		t.Fatalf("%s %q %v", v.Backend(), v.Location(), v.Names())
	}
	if got, ok, err := v.Get(ctx, port.DeviceToken); err != nil || !ok || got != "d" {
		t.Fatalf("the vault shares its seed: %q %v %v", got, ok, err)
	}
	if err := v.Put(ctx, "userToken", "u"); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete(ctx, port.DeviceToken); err != nil {
		t.Fatal(err)
	}
	if got := v.Values(); len(got) != 1 || got["userToken"] != "u" {
		t.Fatalf("%v", got)
	}
	for _, method := range []string{"Get", "Put", "Delete"} {
		v.Fail[method] = errBoom
	}
	_, _, gerr := v.Get(ctx, "userToken")
	if !errors.Is(gerr, errBoom) || !errors.Is(v.Put(ctx, "a", "b"), errBoom) || !errors.Is(v.Delete(ctx, "userToken"), errBoom) {
		t.Fatal("Fail did not fail every method")
	}
	if porttest.NewVault(port.SecretsFile, nil).Values() == nil {
		t.Fatal("an empty vault holds nil")
	}
}

// The profiles double answers the store, the caches and the registries
// from one state per profile, records every call, and fails a method on
// demand.
func TestProfiles(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{
		"work": {
			Dir: true, Table: true, Cache: true, CacheBytes: 42,
			Budget: porttest.Budget{Bytes: 4 << 30, Source: port.BudgetFromProfile},
		},
		"old":    {Cache: true, KeptCache: true},
		"travel": {Table: true, Settings: port.ProfileSettings{Backend: port.SecretsKeychain}},
	})
	names, err := p.Names()
	if err != nil || !slices.Equal(names, []string{"travel", "work"}) {
		t.Fatalf("store names: %v %v", names, err)
	}
	if got := p.Caches().Names(); !slices.Equal(got, []string{"old", "work"}) {
		t.Fatalf("cache names: %v", got)
	}

	s, err := p.Settings("work")
	if err != nil || s.Name != "work" || !s.DirExists || !s.Table || s.ConfigDir != "/config/work" ||
		s.ConfigFile != "/config/config.toml" || s.Backend != port.SecretsFile {
		t.Fatalf("%+v %v", s, err)
	}
	vaults, err := p.Vaults("travel")
	if err != nil || vaults[0].Backend() != port.SecretsKeychain || vaults[1].Backend() != port.SecretsFile {
		t.Fatalf("the keychain profile's vault in use comes first: %v", err)
	}
	if vaults, _ = p.Vaults("work"); vaults[0].Backend() != port.SecretsFile {
		t.Fatal("the file profile's vault in use comes first")
	}

	work, err := p.Status("work")
	if err != nil || work.Bytes != 42 || work.Budget != 4<<30 || work.BudgetSource != port.BudgetFromProfile || work.Dir != "/cache/work" {
		t.Fatalf("%+v %v", work, err)
	}
	if fresh, _ := p.Status("fresh"); fresh.Budget != porttest.CacheBudget || fresh.BudgetSource != port.BudgetFromDefault {
		t.Fatalf("a profile without a budget: %+v", fresh)
	}

	if dir, err := p.Remove("work"); err != nil || !dir.Existed || dir.Kept || p.State("work").Cache || p.State("work").CacheBytes != 0 {
		t.Fatalf("%+v %v", dir, err)
	}
	if dir, _ := p.Remove("old"); !dir.Kept || !p.State("old").Cache {
		t.Fatalf("a kept cache: %+v", dir)
	}
	if dir, err := p.RemoveDir("work"); err != nil || !dir.Existed || dir.Kept || p.State("work").Dir {
		t.Fatalf("%+v %v", dir, err)
	}
	if had, err := p.RemoveTable("work"); err != nil || !had || p.State("work").Table {
		t.Fatalf("%v %v", had, err)
	}
	if had, _ := p.RemoveTable("work"); had {
		t.Fatal("a table removed twice")
	}

	if _, err := p.Settings("Bad Name"); !errors.Is(err, porttest.ErrBadName) {
		t.Fatalf("a bad name: %v", err)
	}
	for _, name := range []string{"", "A", "a b"} {
		if porttest.ValidName(name) {
			t.Errorf("%q is valid", name)
		}
	}
	p.Fail["Names"] = errBoom
	if _, err := p.Names(); !errors.Is(err, errBoom) {
		t.Fatalf("Fail: %v", err)
	}
	if calls := p.Calls(); calls[0] != "Names " || !slices.Contains(calls, "RemoveCache work") || !slices.Contains(calls, "Settings Bad Name") {
		t.Fatalf("%v", calls)
	}
}

// A profile's registry keeps its vault in use in step with its cloud.
func TestProfilesRegistry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{
		"travel": {Settings: port.ProfileSettings{Backend: port.SecretsKeychain}},
	})
	reg, closeReg, err := p.Open("travel")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.IsRegistered(ctx); err != nil || ok {
		t.Fatalf("before registering: %v %v", ok, err)
	}
	if err := reg.Register(ctx, "wrong"); err == nil {
		t.Fatal("a wrong code registered")
	}
	if err := reg.Register(ctx, "abcdefgh"); err != nil {
		t.Fatal(err)
	}
	st := p.State("travel")
	if _, ok := st.Keychain.Values()[port.DeviceToken]; !ok || len(st.File.Values()) != 0 {
		t.Fatal("the token is not in the vault in use alone")
	}
	if ok, err := reg.IsRegistered(ctx); err != nil || !ok {
		t.Fatalf("after registering: %v %v", ok, err)
	}
	if err := reg.Forget(ctx); err != nil || len(st.Keychain.Values()) != 0 {
		t.Fatalf("forget: %v %v", err, st.Keychain.Values())
	}
	if err := closeReg(); err != nil || p.Closed != 1 {
		t.Fatalf("%v %d", err, p.Closed)
	}
	if p.Cloud("travel") != p.Cloud("travel") {
		t.Fatal("a profile has two clouds")
	}
	p.Fail["Open"] = errBoom
	if _, _, err := p.Open("travel"); !errors.Is(err, errBoom) {
		t.Fatalf("Fail: %v", err)
	}
}

func TestRenderer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	r := &porttest.Renderer{Report: port.RenderReport{Pages: 9}}
	var out bytes.Buffer
	rep, err := r.Render(ctx, []byte("archive"), "d1", port.RenderOptions{Mode: "pages", Grain: "group", Paper: "a4", NoTemplates: true}, &out)
	if err != nil || out.String() != "PDF d1 pages a4 grain-group no-template\narchive" || rep.Pages != 3 {
		t.Fatalf("%q %+v %v", out.String(), rep, err)
	}
	// The default policy for unreadable pages is left out; another is
	// named, and a page reports the unreadable pages the PDF would.
	out.Reset()
	r.Report.UnreadablePages = 1
	rep, err = r.Render(ctx, nil, "d1", port.RenderOptions{Page: 1, Paper: "a4", Unreadable: port.UnreadableStrict}, &out)
	if err != nil || out.String() != "PNG d1 p1 x0 a4 unreadable-strict\n" || rep.Pages != 1 || rep.UnreadablePages != 1 {
		t.Fatalf("%q %+v %v", out.String(), rep, err)
	}
	r.Report.UnreadablePages = 0
	for format, want := range map[port.Format]string{
		port.FormatPNG:  "PNG d1 p2 x1.5 a4",
		port.FormatJPEG: "JPG d1 p2 x1.5 q80 a4",
		port.FormatSVG:  "SVG d1 p2 a4",
	} {
		out.Reset()
		rep, err := r.Render(ctx, nil, "d1", port.RenderOptions{Page: 2, Format: format, Scale: 1.5, Quality: 80, Paper: "a4"}, &out)
		if err != nil || out.String() != want+"\n" || rep.Pages != 1 {
			t.Errorf("%s: %q %+v %v", format, out.String(), rep, err)
		}
	}
	if _, err := r.Render(ctx, nil, "d1", port.RenderOptions{Page: 4}, io.Discard); !errors.Is(err, domain.ErrNoSuchPage) {
		t.Fatalf("a page past the last: %v", err)
	}
	if calls := r.Calls(); len(calls) != 5 || !strings.HasPrefix(calls[0], "PDF d1") {
		t.Fatalf("%v", calls)
	}

	r.FailIDs = map[domain.ItemID]error{"bad": errBoom}
	if _, err := r.Render(ctx, nil, "bad", port.RenderOptions{}, io.Discard); !errors.Is(err, errBoom) {
		t.Fatalf("FailIDs: %v", err)
	}
	if _, err := r.Render(ctx, nil, "d1", port.RenderOptions{}, io.Discard); err != nil {
		t.Fatalf("FailIDs failed another document: %v", err)
	}
	// A drawing that fails partway has written its first line.
	partway := &porttest.Renderer{FailWritten: errBoom}
	out.Reset()
	if _, err := partway.Render(ctx, []byte("archive"), "d1", port.RenderOptions{Mode: "fresh"}, &out); !errors.Is(err, errBoom) || out.String() != "PDF d1 fresh \n" {
		t.Fatalf("FailWritten: %q %v", out.String(), err)
	}
	// The writer's failure is the drawing's.
	if _, err := (&porttest.Renderer{}).Render(ctx, nil, "d1", port.RenderOptions{}, failWriter{}); !errors.Is(err, errBoom) {
		t.Fatalf("a failing writer: %v", err)
	}
	r.Fail = errBoom
	if _, err := r.Render(ctx, nil, "d1", port.RenderOptions{}, io.Discard); !errors.Is(err, errBoom) {
		t.Fatalf("Fail: %v", err)
	}
	cancelled, cancel := context.WithCancelCause(ctx)
	cancel(errBoom)
	if _, err := (&porttest.Renderer{}).Render(cancelled, nil, "d1", port.RenderOptions{}, io.Discard); !errors.Is(err, errBoom) {
		t.Fatalf("a cancelled render: %v", err)
	}
	off := &porttest.Renderer{Unavailable: true}
	if _, err := off.Render(ctx, nil, "d1", port.RenderOptions{}, io.Discard); !errors.Is(err, domain.ErrRenderUnsupported) || off.Available() == nil {
		t.Fatalf("unavailable: %v", err)
	}
}

func TestMemFilesModTime(t *testing.T) {
	t.Parallel()
	m := porttest.NewMemFiles()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := m.SetModTime("/a", at); !errors.Is(err, porttest.ErrNoFile) {
		t.Fatalf("a missing file: %v", err)
	}
	if _, ok, err := m.ModTime("/a"); err != nil || ok {
		t.Fatalf("a missing file has a time: %v %v", ok, err)
	}
	m.Write("/a", []byte("x"))
	if err := m.SetModTime("/dir/../a", at); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := m.ModTime("/a"); err != nil || !ok || !got.Equal(at) {
		t.Fatalf("%v %v %v", got, ok, err)
	}
	m.Fail["ModTime"], m.Fail["SetModTime"] = errBoom, errBoom
	if _, _, err := m.ModTime("/a"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if err := m.SetModTime("/a", at); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}
