// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package indexcache_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// source is a cloud of documents whose indexes are text files addressed by
// their sha256, counting every request.
type source struct {
	mu         sync.Mutex
	generation int64
	docs       map[domain.ItemID][]domain.File
	calls      map[string]int
	// nodeHashed makes every index come without its bytes, as a node-hashed
	// one does.
	nodeHashed bool
	fail       error
}

func newSource(n int) *source {
	s := &source{docs: map[domain.ItemID][]domain.File{}, calls: map[string]int{}, generation: 1}
	for i := range n {
		s.setDoc(domain.ItemID("d"+strconv.Itoa(i)), "v1")
	}
	return s
}

func (s *source) setDoc(id domain.ItemID, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[id] = []domain.File{{Name: string(id) + ".metadata", Hash: hashOf([]byte(string(id) + version)), Size: 10}}
	s.generation++
}

func encodeFiles(files []domain.File) []byte {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s %s %d\n", f.Name, f.Hash, f.Size)
	}
	return []byte(b.String())
}

func decodeFiles(raw []byte, hash string) ([]domain.File, error) {
	if hashOf(raw) != hash {
		return nil, errors.New("hash mismatch")
	}
	var out []domain.File
	for line := range strings.Lines(string(raw)) {
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, errors.New("malformed")
		}
		size, _ := strconv.ParseInt(f[2], 10, 64)
		out = append(out, domain.File{Name: f[0], Hash: f[1], Size: size})
	}
	return out, nil
}

// records are the root's records, by id order.
func (s *source) records() []domain.Record {
	var out []domain.Record
	for i := range len(s.docs) {
		id := domain.ItemID("d" + strconv.Itoa(i))
		out = append(out, domain.Record{ID: id, Hash: hashOf(encodeFiles(s.docs[id])), Subfiles: 1})
	}
	return out
}

func encodeRoot(records []domain.Record) []byte {
	var b strings.Builder
	for _, r := range records {
		fmt.Fprintf(&b, "%s %s %d\n", r.ID, r.Hash, r.Subfiles)
	}
	return []byte(b.String())
}

func (s *source) count(name string) {
	s.calls[name]++
}

func (s *source) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, v := range s.calls {
		n += v
	}
	return n
}

func (s *source) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = map[string]int{}
}

func (s *source) Pointer(context.Context) (string, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count("Pointer")
	if s.fail != nil {
		return "", 0, s.fail
	}
	if len(s.docs) == 0 {
		return "", 0, nil
	}
	return hashOf(encodeRoot(s.records())), s.generation, nil
}

func (s *source) RootIndex(_ context.Context, hash string) ([]byte, []domain.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count("RootIndex")
	raw := encodeRoot(s.records())
	if hashOf(raw) != hash {
		return nil, nil, errors.New("no such root")
	}
	if s.nodeHashed {
		return nil, s.records(), nil
	}
	return raw, s.records(), nil
}

func (s *source) Index(_ context.Context, rec domain.Record) ([]byte, []domain.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count("Index")
	raw := encodeFiles(s.docs[rec.ID])
	if hashOf(raw) != rec.Hash {
		return nil, nil, errors.New("no such index")
	}
	if s.nodeHashed {
		return nil, s.docs[rec.ID], nil
	}
	return raw, s.docs[rec.ID], nil
}

func (s *source) DecodeRoot(raw []byte, hash string) ([]domain.Record, error) {
	if hashOf(raw) != hash {
		return nil, errors.New("hash mismatch")
	}
	var out []domain.Record
	for line := range strings.Lines(string(raw)) {
		f := strings.Fields(line)
		if len(f) != 3 {
			return nil, errors.New("malformed")
		}
		n, _ := strconv.Atoi(f[2])
		out = append(out, domain.Record{ID: domain.ItemID(f[0]), Hash: f[1], Subfiles: n})
	}
	return out, nil
}

func (s *source) DecodeFiles(raw []byte, hash string) ([]domain.File, error) {
	return decodeFiles(raw, hash)
}

func (s *source) Root(ctx context.Context) (port.RootState, error) {
	hash, gen, err := s.Pointer(ctx)
	if err != nil || hash == "" {
		return port.RootState{}, err
	}
	_, recs, err := s.RootIndex(ctx, hash)
	return port.RootState{Hash: hash, Generation: gen, Records: recs}, err
}

func (s *source) CommitRoot(_ context.Context, base port.RootState, records []domain.Record) (port.RootState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count("CommitRoot")
	if base.Generation != s.generation {
		return port.RootState{}, port.ErrRootConflict
	}
	s.generation++
	return port.RootState{Hash: hashOf(encodeRoot(records)), Generation: s.generation, Records: records}, nil
}

func (s *source) Files(ctx context.Context, rec domain.Record) ([]domain.File, error) {
	_, files, err := s.Index(ctx, rec)
	return files, err
}

func (s *source) PutIndex(_ context.Context, id domain.ItemID, files []domain.File) (domain.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count("PutIndex")
	s.docs[id] = files
	return domain.Record{ID: id, Hash: hashOf(encodeFiles(files)), Subfiles: len(files)}, nil
}

// store is a profile's cache in a temporary directory.
func store(t testing.TB) *blobcache.Cache {
	t.Helper()
	return blobcache.New(filepath.Join(t.TempDir(), "default"), 1<<20, porttest.NewClock(time.Unix(1_700_000_000, 0)), nil)
}

// read loads the whole library the way the library use case does: the
// root, then every record's index.
func read(t *testing.T, c *indexcache.Cache) (port.RootState, [][]domain.File) {
	t.Helper()
	state, err := c.Root(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var all [][]domain.File
	for _, rec := range state.Records {
		files, err := c.Files(context.Background(), rec)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, files)
	}
	return state, all
}

func newCache(src *source, st indexcache.Store, clk port.Clock, mode indexcache.Mode, ttl time.Duration, log *slog.Logger) *indexcache.Cache {
	return indexcache.New(src, st, indexcache.Options{Profile: "default", Mode: mode, TTL: ttl, Clock: clk, Log: log})
}

func TestMissThenHit(t *testing.T) {
	t.Parallel()
	for _, nodeHashed := range []bool{false, true} {
		src := newSource(5)
		src.nodeHashed = nodeHashed
		st := store(t)
		clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
		first, files := read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
		if src.calls["Pointer"] != 1 || src.calls["RootIndex"] != 1 || src.calls["Index"] != 5 {
			t.Fatalf("a cold read: %v", src.calls)
		}
		// The next run, a process of its own: the root costs one call, and
		// every index is on disk.
		src.reset()
		second, again := read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
		if src.total() != 1 || src.calls["Pointer"] != 1 {
			t.Fatalf("node-hashed %v: a warm read: %v", nodeHashed, src.calls)
		}
		if fmt.Sprint(first) != fmt.Sprint(second) || fmt.Sprint(files) != fmt.Sprint(again) {
			t.Fatalf("the cache changed the library:\n%v\n%v", first, second)
		}
	}
}

func TestChangedRootFetchesOnlyWhatIsNew(t *testing.T) {
	t.Parallel()
	src := newSource(5)
	st := store(t)
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
	src.setDoc("d3", "v2")
	src.reset()
	state, _ := read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
	if src.calls["Pointer"] != 1 || src.calls["RootIndex"] != 1 || src.calls["Index"] != 1 || src.total() != 3 {
		t.Fatalf("after one document changed: %v", src.calls)
	}
	if state.Generation != src.generation {
		t.Fatalf("generation %d, want %d", state.Generation, src.generation)
	}
}

func TestEmptyLibrary(t *testing.T) {
	t.Parallel()
	src := newSource(0)
	st := store(t)
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	if state, _ := read(t, newCache(src, st, clk, indexcache.Live, 0, nil)); state.Hash != "" || len(state.Records) != 0 {
		t.Fatalf("%+v", state)
	}
	src.reset()
	if state, _ := read(t, newCache(src, st, clk, indexcache.Offline, 0, nil)); state.Hash != "" || src.total() != 0 {
		t.Fatalf("%+v %v", state, src.calls)
	}
}

func TestOffline(t *testing.T) {
	t.Parallel()
	src := newSource(3)
	st := store(t)
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))

	if _, err := newCache(src, st, clk, indexcache.Offline, 0, log).Root(context.Background()); !errors.Is(err, port.ErrOffline) ||
		!strings.Contains(err.Error(), "profile default has no root") {
		t.Fatalf("offline with nothing cached: %v", err)
	}
	want, wantFiles := read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
	src.reset()
	got, files := read(t, newCache(src, st, clk, indexcache.Offline, 0, log))
	if src.total() != 0 || fmt.Sprint(got) != fmt.Sprint(want) || fmt.Sprint(files) != fmt.Sprint(wantFiles) {
		t.Fatalf("an offline read: %v\n%v", src.calls, got)
	}
	if logs.Len() != 0 {
		t.Fatalf("a fresh root warned: %s", logs.String())
	}
	// A day later the root is stale: still served, with a warning.
	clk.Advance(25 * time.Hour)
	if _, err := newCache(src, st, clk, indexcache.Offline, 0, log).Root(context.Background()); err != nil ||
		!strings.Contains(logs.String(), "last confirmed 25h0m0s ago") {
		t.Fatalf("%v %q", err, logs.String())
	}
	// An index the cache lacks is an error that names it, and the root
	// index too.
	st.Drop("indexes", want.Records[1].Hash)
	c := newCache(src, st, clk, indexcache.Offline, 0, nil)
	if _, err := c.Files(context.Background(), want.Records[1]); !errors.Is(err, port.ErrOffline) || !strings.Contains(err.Error(), "the index of d1") {
		t.Fatalf("%v", err)
	}
	st.Drop("indexes", want.Hash)
	if _, err := c.Root(context.Background()); !errors.Is(err, port.ErrOffline) || !strings.Contains(err.Error(), "the root index") {
		t.Fatalf("%v", err)
	}
	// Nothing is written offline.
	if _, err := c.CommitRoot(context.Background(), want, nil); !errors.Is(err, port.ErrOffline) {
		t.Fatal(err)
	}
	if _, err := c.PutIndex(context.Background(), "d9", nil); !errors.Is(err, port.ErrOffline) {
		t.Fatal(err)
	}
	if src.total() != 0 {
		t.Fatalf("offline asked %v", src.calls)
	}
}

func TestTTL(t *testing.T) {
	t.Parallel()
	src := newSource(3)
	st := store(t)
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	read(t, newCache(src, st, clk, indexcache.Live, 0, nil))

	src.reset()
	clk.Advance(4 * time.Minute)
	read(t, newCache(src, st, clk, indexcache.Fresh, 5*time.Minute, nil))
	if src.total() != 0 {
		t.Fatalf("within the TTL: %v", src.calls)
	}
	// Live mode, a write's, asks whatever the TTL says.
	read(t, newCache(src, st, clk, indexcache.Live, 5*time.Minute, nil))
	if src.calls["Pointer"] != 1 {
		t.Fatalf("a live read within the TTL: %v", src.calls)
	}
	// The live read confirmed the root again: the TTL counts from it.
	src.reset()
	clk.Advance(6 * time.Minute)
	read(t, newCache(src, st, clk, indexcache.Fresh, 5*time.Minute, nil))
	if src.calls["Pointer"] != 1 {
		t.Fatalf("past the TTL: %v", src.calls)
	}
	// A zero TTL never skips the root.
	src.reset()
	read(t, newCache(src, st, clk, indexcache.Fresh, 0, nil))
	if src.calls["Pointer"] != 1 {
		t.Fatalf("a zero TTL: %v", src.calls)
	}
}

func TestDamagedEntriesAreMisses(t *testing.T) {
	t.Parallel()
	src := newSource(3)
	dir := filepath.Join(t.TempDir(), "default")
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	st := blobcache.New(dir, 1<<20, clk, nil)
	state, _ := read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
	damage := map[string][]byte{
		state.Records[0].Hash: []byte("not json"),
		state.Records[1].Hash: []byte(`{"v":1,"kind":"files","hash":"` + state.Records[1].Hash + `","raw":"dGFtcGVyZWQ="}`),
		state.Records[2].Hash: []byte(`{"v":1,"kind":"files","hash":"` + state.Hash + `"}`),
	}
	for hash, data := range damage {
		if err := os.WriteFile(filepath.Join(dir, "indexes", hash), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "state", "root"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	src.reset()
	_, files := read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
	if src.calls["Index"] != 3 || len(files) != 3 || len(files[1]) != 1 {
		t.Fatalf("damaged entries: %v %v", src.calls, files)
	}
	// They were replaced: the next read is warm again.
	src.reset()
	read(t, newCache(src, st, clk, indexcache.Live, 0, nil))
	if src.total() != 1 {
		t.Fatalf("%v", src.calls)
	}
}

func TestWritesFillTheCache(t *testing.T) {
	t.Parallel()
	src := newSource(2)
	st := store(t)
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	c := newCache(src, st, clk, indexcache.Live, 0, nil)
	state, _ := read(t, c)
	rec, err := c.PutIndex(context.Background(), "d2", []domain.File{{Name: "d2.metadata", Hash: hashOf([]byte("new")), Size: 3}})
	if err != nil {
		t.Fatal(err)
	}
	root, err := c.CommitRoot(context.Background(), state, append(state.Records, rec))
	if err != nil {
		t.Fatal(err)
	}
	// Offline sees the write without having read it back.
	src.reset()
	got, files := read(t, newCache(src, st, clk, indexcache.Offline, 0, nil))
	if src.total() != 0 || got.Hash != root.Hash || got.Generation != root.Generation || len(files) != 3 || files[2][0].Name != "d2.metadata" {
		t.Fatalf("%v %+v %v", src.calls, got, files)
	}
	// A conflict leaves the cached root alone.
	if _, err := c.CommitRoot(context.Background(), state, nil); !errors.Is(err, port.ErrRootConflict) {
		t.Fatal(err)
	}
	if again, _ := read(t, newCache(src, st, clk, indexcache.Offline, 0, nil)); again.Hash != root.Hash {
		t.Fatal("a failed commit moved the cached root")
	}
}

func TestSourceErrors(t *testing.T) {
	t.Parallel()
	src := newSource(2)
	src.fail = errors.New("offline network")
	c := newCache(src, store(t), porttest.NewClock(time.Unix(0, 0)), indexcache.Live, 0, nil)
	if _, err := c.Root(context.Background()); !errors.Is(err, src.fail) {
		t.Fatal(err)
	}
	if _, err := c.Files(context.Background(), domain.Record{ID: "x", Hash: hashOf([]byte("x"))}); err == nil {
		t.Fatal("a missing index read")
	}
}

// Processes reading and writing the same cache at once see whole entries
// or none: every read returns the library.
func TestConcurrentProcesses(t *testing.T) {
	t.Parallel()
	src := newSource(20)
	dir := filepath.Join(t.TempDir(), "default")
	clk := porttest.NewClock(time.Unix(1_700_000_000, 0))
	want, _ := read(t, newCache(src, blobcache.New(dir, 1<<20, clk, nil), clk, indexcache.Live, 0, nil))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			c := newCache(src, blobcache.New(dir, 1<<20, clk, nil), clk, indexcache.Live, 0, nil)
			state, err := c.Root(context.Background())
			if err == nil && state.Hash != want.Hash {
				err = errors.New("another root")
			}
			for _, rec := range state.Records {
				if _, ferr := c.Files(context.Background(), rec); ferr != nil {
					err = ferr
				}
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	src.reset()
	read(t, newCache(src, blobcache.New(dir, 1<<20, clk, nil), clk, indexcache.Offline, 0, nil))
	if src.total() != 0 {
		t.Fatalf("%v", src.calls)
	}
}

func TestNewNeedsItsParts(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func(){
		"source": func() { indexcache.New(nil, store(t), indexcache.Options{Clock: porttest.NewClock(time.Unix(0, 0))}) },
		"store": func() {
			indexcache.New(newSource(0), nil, indexcache.Options{Clock: porttest.NewClock(time.Unix(0, 0))})
		},
		"clock": func() { indexcache.New(newSource(0), store(t), indexcache.Options{}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("no %s did not panic", name)
				}
			}()
			build()
		}()
	}
}
