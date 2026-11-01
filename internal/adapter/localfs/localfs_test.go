// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/localfs"
)

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestProduceInstallsOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.pdf")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := localfs.New().Produce(t.Context(), path, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("got %q", got)
	}
	if names := entries(t, dir); !slices.Equal(names, []string{"out.pdf"}) {
		t.Fatalf("left behind: %v", names)
	}
}

func TestProduceLeavesNothingOnFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for name, tc := range map[string]struct {
		ctx   func(t *testing.T) context.Context
		write func(io.Writer) error
		want  error
	}{
		"write fails": {
			ctx:   func(t *testing.T) context.Context { return t.Context() },
			write: func(w io.Writer) error { _, _ = io.WriteString(w, "half"); return boom },
			want:  boom,
		},
		"cancelled": {
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			write: func(w io.Writer) error { _, err := io.WriteString(w, "all"); return err },
			want:  context.Canceled,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "out.pdf")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := localfs.New().Produce(tc.ctx(t), path, tc.write)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			if got, _ := os.ReadFile(path); string(got) != "old" {
				t.Fatalf("the existing file became %q", got)
			}
			if names := entries(t, dir); !slices.Equal(names, []string{"out.pdf"}) {
				t.Fatalf("left behind: %v", names)
			}
		})
	}
}

func TestProduceRefuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	called := false
	write := func(io.Writer) error { called = true; return nil }
	if err := localfs.New().Produce(t.Context(), dir, write); !errors.Is(err, localfs.ErrIsDirectory) || called {
		t.Fatalf("a directory: %v, called %v", err, called)
	}
	if err := localfs.New().Produce(t.Context(), filepath.Join(dir, "missing", "f"), write); err == nil || called {
		t.Fatalf("a missing parent: %v, called %v", err, called)
	}
}

func TestStatReadWalkRemove(t *testing.T) {
	t.Parallel()
	fs := localfs.New()
	dir := t.TempDir()
	file := filepath.Join(dir, "a", "b.txt")
	if err := fs.MkdirAll(filepath.Dir(file)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.pdf"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := fs.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(r); err != nil || string(b) != "0123456789" || r.Close() != nil {
		t.Fatalf("open %q %v", b, err)
	}
	if r, err := fs.Open(dir); err == nil || r != nil {
		t.Fatal("a directory opened as a file")
	}
	size, isDir, exists, err := fs.Stat(file)
	if err != nil || !exists || isDir || size != 10 {
		t.Fatalf("stat %d %v %v %v", size, isDir, exists, err)
	}
	if _, isDir, exists, err := fs.Stat(dir); err != nil || !exists || !isDir {
		t.Fatalf("stat dir %v %v %v", isDir, exists, err)
	}
	if _, _, exists, err := fs.Stat(filepath.Join(dir, "none")); err != nil || exists {
		t.Fatalf("stat missing %v %v", exists, err)
	}
	if head, err := fs.ReadHead(file, 4); err != nil || string(head) != "0123" {
		t.Fatalf("head %q %v", head, err)
	}
	if head, err := fs.ReadHead(file, 64); err != nil || string(head) != "0123456789" {
		t.Fatalf("long head %q %v", head, err)
	}
	if tail, err := fs.ReadTail(file, 3); err != nil || string(tail) != "789" {
		t.Fatalf("tail %q %v", tail, err)
	}
	if tail, err := fs.ReadTail(file, 64); err != nil || string(tail) != "0123456789" {
		t.Fatalf("long tail %q %v", tail, err)
	}
	if _, err := fs.ReadHead(dir, 1); err == nil {
		t.Fatal("read a directory")
	}
	if _, err := fs.ReadTail(filepath.Join(dir, "none"), 1); err == nil {
		t.Fatal("read a missing file")
	}
	list, err := fs.Walk(dir)
	if err != nil || !slices.Equal(list, []string{filepath.Join("a", "b.txt"), "c.pdf"}) {
		t.Fatalf("walk %v %v", list, err)
	}
	if err := fs.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(file); err == nil {
		t.Fatal("removed a missing file")
	}
	if _, err := fs.Walk(filepath.Join(dir, "none")); err == nil || !strings.Contains(err.Error(), "none") {
		t.Fatalf("walk of a missing directory: %v", err)
	}
}

func BenchmarkProduce(b *testing.B) {
	fs := localfs.New()
	path := filepath.Join(b.TempDir(), "out")
	body := make([]byte, 1<<20)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		if err := fs.Produce(b.Context(), path, func(w io.Writer) error { _, err := w.Write(body); return err }); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStage(t *testing.T) {
	t.Parallel()
	fs := localfs.New()
	dir, remove, err := fs.Stage()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("%v %v", info, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if err := fs.Produce(t.Context(), filepath.Join(dir, "0"), func(w io.Writer) error {
		_, err := w.Write([]byte("staged"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the staging directory is still there: %v", err)
	}
}

// Not parallel: it points the temporary directory somewhere missing.
func TestStageWithoutATemporaryDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("TMPDIR", missing)
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)
	if _, _, err := localfs.New().Stage(); err == nil || !strings.Contains(err.Error(), "staging directory") {
		t.Fatalf("got %v", err)
	}
}

func TestStatThroughALockedDirectory(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 || filepath.Separator == '\\' {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, _, _, err := localfs.New().Stat(filepath.Join(dir, "f")); err == nil {
		t.Fatal("stat through a locked directory succeeded")
	}
}

func TestRename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	from, to := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(from, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := localfs.New()
	if err := fs.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(to); string(got) != "x" || slices.Contains(entries(t, dir), "a") {
		t.Fatalf("%q %v", got, entries(t, dir))
	}
	if err := fs.Rename(from, to); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%v", err)
	}
}
