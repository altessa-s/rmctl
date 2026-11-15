// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/internal/port"
)

func TestProgressHelpersPolicyAndClearing(t *testing.T) {
	t.Parallel()
	helpers := map[string]func(Streams, string) (port.Progress, func()){"count": CountProgress, "bytes": TransferBytes, "percent": Progress}
	for name, helper := range helpers {
		for _, tc := range []struct {
			name     string
			terminal bool
			args     []string
			env      map[string]string
			silent   bool
		}{
			{name: "tty", terminal: true}, {name: "pipe", silent: true},
			{name: "json", terminal: true, args: []string{"--json"}, silent: true},
			{name: "format", terminal: true, args: []string{"--format=json"}, silent: true},
			{name: "environment", terminal: true, env: map[string]string{FormatEnv: "json"}, silent: true},
			{name: "quiet", terminal: true, args: []string{"--quiet"}, silent: true},
			{name: "dumb", terminal: true, env: map[string]string{"TERM": "dumb"}},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				var buf bytes.Buffer
				env := NewEnv(Streams{Err: &buf, IsTerminal: tc.terminal, Getenv: func(k string) string { return tc.env[k] }}, nil)
				cmd := &cobra.Command{Use: "test"}
				AddPersistentFlags(cmd, env)
				if err := cmd.ParseFlags(tc.args); err != nil {
					t.Fatal(err)
				}
				update, done := helper(env.ProgressStreams(cmd), "work")
				if tc.silent {
					if update != nil {
						t.Fatal("progress enabled")
					}
					done()
					if buf.Len() != 0 {
						t.Fatalf("silent progress: %q", buf.String())
					}
					return
				}
				done() // nothing to clear before the first update
				update(1, 2)
				update(2, 2)
				done()
				before := buf.String()
				done()
				if buf.String() != before {
					t.Fatal("done is not idempotent")
				}
				if !strings.Contains(before, "work") || !strings.HasSuffix(before, "\r") && !strings.HasSuffix(before, "\r\x1b[K") {
					t.Fatalf("line not erased: %q", before)
				}
				if tc.name == "dumb" && strings.Contains(before, "\x1b") {
					t.Fatalf("escape in dumb terminal: %q", before)
				}
				Notef(&buf, "warning")
				if !strings.HasSuffix(buf.String(), "warning\n") {
					t.Fatal(buf.String())
				}
			})
		}
	}
}

func TestNewProgressHelpersConcurrent(t *testing.T) {
	t.Parallel()
	for _, helper := range []func(Streams, string) (port.Progress, func()){CountProgress, TransferBytes} {
		var buf bytes.Buffer
		update, done := helper(Streams{Err: &buf, IsTerminal: true}, "work")
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for n := range 20 {
					update(int64(n), 20)
				}
			})
		}
		wg.Wait()
		update(20, 20)
		done()
		if !strings.HasSuffix(buf.String(), "\n") {
			t.Fatal("default helper must finish its line")
		}
	}
}

func TestRenderProgressPhasesAndFinishing(t *testing.T) {
	t.Parallel()
	for _, local := range []bool{false, true} {
		var buf bytes.Buffer
		pages, download, done := RenderProgress(Streams{Err: &buf, IsTerminal: true})
		done() // no active line yet
		pages(0, 10)
		if !local {
			download(5, 10)
		}
		pages(10, 10)
		done()
		before := buf.String()
		done()
		if buf.String() != before || !strings.HasSuffix(before, "\n") {
			t.Fatalf("line not finished idempotently: %q", buf.String())
		}
		if strings.Contains(before, "downloading") == local {
			t.Fatalf("local=%v download visibility: %q", local, before)
		}
	}
	var buf bytes.Buffer
	pages, download, done := RenderProgress(Streams{Err: &buf})
	done()
	if pages != nil || download != nil || buf.Len() != 0 {
		t.Fatalf("nonterminal progress enabled: %q", buf.String())
	}
}

func TestCommentaryClearsConcurrentProgress(t *testing.T) {
	t.Parallel()
	for _, term := range []string{"xterm", "dumb"} {
		var buf bytes.Buffer
		stderr := ProgressOutput(&buf)
		if ProgressOutput(stderr) != stderr {
			t.Fatal("output wrapped twice")
		}
		streams := Streams{Err: stderr, IsTerminal: true, Getenv: func(string) string { return term }, eraseProgress: true}
		progress, done := TransferBytes(streams, "downloading")
		progress(10, 100)
		if _, err := stderr.Write([]byte("warning\n")); err != nil {
			t.Fatal(err)
		}
		eraseText := "\r\x1b[Kwarning\n"
		if term == "dumb" {
			eraseText = "\rwarning\n"
		}
		if !strings.HasSuffix(buf.String(), eraseText) {
			t.Fatalf("warning interleaved with progress: %q", buf.String())
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			for n := range 100 {
				progress(int64(n), 100)
			}
		})
		wg.Go(func() {
			for range 100 {
				if _, err := stderr.Write([]byte("log entry\n")); err != nil {
					t.Error(err)
				}
			}
		})
		wg.Wait()
		done()
	}
}

func TestProgressOutputWriteError(t *testing.T) {
	t.Parallel()
	w := ProgressOutput(failProgressWriter{})
	update, done := TransferBytes(Streams{Err: w, IsTerminal: true}, "downloading")
	update(1, 2)
	if n, err := w.Write([]byte("warning\n")); n != 0 || err == nil {
		t.Fatalf("write error: %d %v", n, err)
	}
	done()
}

type failProgressWriter struct{}

func (failProgressWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
