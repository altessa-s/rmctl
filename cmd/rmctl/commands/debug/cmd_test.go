// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

func build(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{New(env)} }

func fake() *clitest.Fake {
	return &clitest.Fake{
		RootState: port.RootState{Hash: "rh", Generation: 7, Records: []domain.Record{{ID: "a"}, {ID: "b"}}},
		BlobData:  []byte("raw bytes"),
		Reports: []port.FileReport{
			{File: domain.File{Name: "a.metadata", Hash: "h1", Size: 80}, Text: []byte(`{"visibleName":"Secret.pdf","lastModified":"1730000000000","version":3}`)},
			{File: domain.File{Name: "a.pagedata", Hash: "h2", Size: 6}, Text: []byte("Blank")},
			{File: domain.File{Name: "a.pdf", Hash: "h3", Size: 2048}},
		},
		HashList: []port.HashReport{{Record: domain.Record{ID: "a", Hash: "x"}, ByIndexFile: true}, {Record: domain.Record{ID: "b", Hash: "y"}}},
		Missing:  []string{"h9"},
		Counts:   map[string]int{"metadata.visibleName": 2, "content.fileType": 1},
	}
}

func run(t *testing.T, f *clitest.Fake, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, f.Services(), clitest.Options{}, build, append([]string{"debug"}, args...)...)
}

func TestText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"root"}, "hash        rh\ngeneration  7\nrecords     2\n"},
		{[]string{"blob", "h", "--name", "a.metadata"}, "raw bytes"},
		{[]string{"hashes"}, "file  a  x\nnode  b  y\n"},
		{[]string{"checkfiles"}, "h9\n"},
		{[]string{"sweep"}, "     1  content.fileType\n     2  metadata.visibleName\n"},
	}
	for _, tc := range tests {
		r := run(t, fake(), tc.args...)
		if r.Code != 0 || r.Out != tc.want {
			t.Fatalf("%v: %d %q %v", tc.args, r.Code, r.Out, r.Error)
		}
	}
}

func TestFilesMasksNames(t *testing.T) {
	t.Parallel()
	r := run(t, fake(), "files", "a")
	if strings.Contains(r.Out, "Secret") || !strings.Contains(r.Out, `"visibleName": "<name:6>.pdf"`) ||
		!strings.Contains(r.Out, `"lastModified": "1730000000000"`) || !strings.Contains(r.Out, "Blank\n") {
		t.Fatalf("%s", r.Out)
	}
	r = run(t, fake(), "files", "a", "--raw")
	if !strings.Contains(r.Out, `"visibleName":"Secret.pdf"`) {
		t.Fatalf("%s", r.Out)
	}
}

func TestJSON(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"root", "hashes", "checkfiles", "sweep", "files"} {
		args := []string{name, "--json"}
		if name == "files" {
			args = []string{name, "a", "--json"}
		}
		r := run(t, fake(), args...)
		if r.Code != 0 {
			t.Fatal(r.Error)
		}
		clitest.Golden(t, name, r.Out)
	}
}

func TestCheckFilesNoneMissing(t *testing.T) {
	t.Parallel()
	f := fake()
	f.Missing = nil
	r := run(t, f, "checkfiles")
	if r.Out != "" || r.Err != "the cloud holds every blob\n" {
		t.Fatalf("%q %q", r.Out, r.Err)
	}
	if r = run(t, f, "checkfiles", "--json"); r.Out != "[]\n" {
		t.Fatalf("%q", r.Out)
	}
}

func TestBlobToFile(t *testing.T) {
	t.Parallel()
	f := fake()
	r := run(t, f, "blob", "h", "--name", "a.rm", "-o", "out/blob.bin")
	calls := f.Calls()
	if r.Code != 0 || r.Out != "" || calls[len(calls)-1] != "SaveBlob h a.rm out/blob.bin" {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Out, calls)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "Blob ") {
			t.Fatalf("-o read the blob into the command: %v", calls)
		}
	}
	f = fake()
	f.Err = errors.New("boom")
	if r = run(t, f, "blob", "h", "-o", "out/blob.bin"); r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "boom") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

func TestErrorsAndUsage(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	f := fake()
	f.Err = boom
	for _, args := range [][]string{{"root"}, {"blob", "h"}, {"files", "a"}, {"hashes"}, {"checkfiles"}, {"sweep"}} {
		r := run(t, f, args...)
		if !errors.Is(r.Error, boom) || !strings.HasPrefix(r.Error.Error(), "debug "+args[0]+": ") {
			t.Fatalf("%v: %v", args, r.Error)
		}
	}
	for _, args := range [][]string{{"root", "x"}, {"blob"}, {"files"}, {"hashes", "x"}, {"checkfiles", "x"}, {"sweep", "x"}, {"bogus"}} {
		if r := run(t, fake(), args...); r.Code != cliutil.CodeUsage {
			t.Fatalf("%v: %d %v", args, r.Code, r.Error)
		}
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, build, "debug", "root"); !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
	if r := run(t, fake()); r.Code != 0 || !strings.Contains(r.Out, "Available Commands:") {
		t.Fatalf("debug alone: %d %q", r.Code, r.Out)
	}
}

func TestWriteFilesFailingWriter(t *testing.T) {
	t.Parallel()
	if err := writeFiles(failWriter{}, []fileReportView{{Name: "a"}}); err == nil {
		t.Fatal("unnoticed")
	}
	if err := writeFiles(&failAfter{n: 1}, []fileReportView{{Name: "a", Text: "x"}}); err == nil {
		t.Fatal("unnoticed")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// failAfter accepts n writes and refuses the rest.
type failAfter struct{ n int }

func (f *failAfter) Write(p []byte) (int, error) {
	if f.n > 0 {
		f.n--
		return len(p), nil
	}
	return 0, errors.New("closed")
}
