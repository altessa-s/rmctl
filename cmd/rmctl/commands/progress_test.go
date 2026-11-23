// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package commands

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestLongCommandProgressPreservesStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, second := filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf")
	for _, name := range []string{first, second} {
		if err := os.WriteFile(name, []byte("abcdef"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, label string
		args        []string
	}{
		{"get", "downloading", []string{"get", "x"}},
		{"mget", "downloading", []string{"mget", "x", "-o", "out"}},
		{"mput", "uploading", []string{"mput", "in"}},
		{"backup", "backing up", []string{"backup", "out"}},
		{"restore", "restoring", []string{"backup", "restore", "in"}},
		{"export", "exporting", []string{"export", "x", "y", "-o", "out.rmdoc"}},
		{"render", "drawing pages", []string{"render", "x", "-o", "out.pdf"}},
		{"folder", "drawing pages", []string{"render", "/", "-o", "out"}},
		{"put", "uploading", []string{"put", first, second}},
		{"mixed", "uploading files", []string{"put", first, "archive.rmdoc"}},
		{"sync", "syncing", []string{"sync"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			execute := func(terminal bool, flags []string, env map[string]string) (string, string, error) {
				f := &clitest.Fake{Progress: [2]int64{50, 100}, SyncSteps: []port.SyncProgress{{FilesTotal: 2, BytesTotal: 100}, {Files: 2, FilesTotal: 2, Bytes: 100, BytesTotal: 100}}}
				var out, errOut bytes.Buffer
				s := Streams{In: strings.NewReader(""), Out: &out, Err: &errOut, IsTerminal: terminal, Getenv: func(k string) string { return env[k] }}
				args := append(slices.Clone(tc.args), "--yes")
				args = append(args, flags...)
				err := Run(context.Background(), args, s, cliutil.Fixed(f.Services()))
				return out.String(), errOut.String(), err
			}
			baseline, _, err := execute(false, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			out, stderr, err := execute(true, nil, nil)
			if err != nil || out != baseline || !strings.Contains(stderr, tc.label) {
				t.Fatalf("stdout changed or progress missing: out %q want %q stderr %q err %v", out, baseline, stderr, err)
			}
			for _, flags := range [][]string{{"--json"}, {"--quiet"}} {
				baseline, _, err = execute(false, flags, nil)
				if err != nil {
					t.Fatal(err)
				}
				out, stderr, err = execute(true, flags, nil)
				if err != nil || out != baseline || strings.ContainsAny(stderr, "\r\x1b") {
					t.Fatalf("suppression %v: %q %q %v", flags, out, stderr, err)
				}
			}
			_, stderr, err = execute(true, nil, map[string]string{"RMCTL_FORMAT": "json"})
			if err != nil || strings.ContainsAny(stderr, "\r\x1b") {
				t.Fatalf("JSON environment progress %q %v", stderr, err)
			}
		})
	}
}

func TestRenderConcurrentPhasesClearBeforeWarnings(t *testing.T) {
	t.Parallel()
	f := &progressRendering{Fake: &clitest.Fake{}}
	svc := f.Services()
	svc.Rendering = f
	var out, errOut bytes.Buffer
	s := Streams{In: strings.NewReader(""), Out: &out, Err: &errOut, IsTerminal: true, Getenv: func(string) string { return "" }}
	err := Run(t.Context(), []string{"render", "/", "-o", "out"}, s, cliutil.Fixed(svc))
	if !errors.Is(err, errProgressRender) || !strings.Contains(errOut.String(), "\r\x1b[K") {
		t.Fatalf("%q %v", errOut.String(), err)
	}
	if !strings.Contains(errOut.String(), "\r\x1b[Kdocument:") {
		t.Fatalf("warning did not start on a cleared line: %q", errOut.String())
	}
}

var errProgressRender = errors.New("render failed")

type progressRendering struct{ *clitest.Fake }

func (*progressRendering) RenderTree(_ context.Context, _ string, _ string, o port.RenderOptions, _ port.RenderTreeOptions) (port.RenderTreeReport, error) {
	var wg sync.WaitGroup
	wg.Go(func() {
		for n := range 20 {
			o.Progress(int64(n), 20)
		}
	})
	wg.Go(func() {
		for n := range 20 {
			o.DownloadProgress(int64(n), 20)
		}
	})
	wg.Wait()
	return port.RenderTreeReport{Documents: []port.RenderedDocument{{LibraryPath: "document", Report: port.RenderReport{UnreadablePages: 1}}}}, errProgressRender
}
