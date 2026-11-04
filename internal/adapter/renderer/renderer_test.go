//go:build (darwin || linux) && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"testing"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"

	rmrender "github.com/altessa-s/rmrender-go"
)

// notes is a three-page notebook: plain text, styled text and a highlight.
func notes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/Notes.rmdoc")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRenderPDF(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.Available(); err != nil {
		t.Fatal(err)
	}
	for _, m := range []port.PDFMode{port.PDFFresh, port.PDFPreserving, port.PDFRecomposed} {
		for _, p := range []port.Paper{port.PaperA4, port.PaperLetter, port.PaperTablet} {
			out, report, err := r.Render(t.Context(), notes(t), "", port.RenderOptions{Mode: m, Paper: p})
			if err != nil || !bytes.HasPrefix(out, []byte("%PDF-")) || report.Pages != 3 || report.SkippedBlocks != 0 {
				t.Fatalf("%s %s: %+v %v", m, p, report, err)
			}
		}
	}
}

func TestRenderPNG(t *testing.T) {
	t.Parallel()
	r := New()
	for _, c := range []struct {
		scale float64
		width int
	}{{1, 1404}, {0.5, 702}} {
		out, report, err := r.Render(t.Context(), notes(t), "a1b2", port.RenderOptions{Page: 2, Scale: c.scale, Paper: port.PaperA4})
		if err != nil || report.Pages != 1 {
			t.Fatalf("%+v %v", report, err)
		}
		config, err := png.DecodeConfig(bytes.NewReader(out))
		if err != nil || config.Width != c.width {
			t.Fatalf("scale %g: %+v %v", c.scale, config, err)
		}
	}
}

func TestRenderFails(t *testing.T) {
	t.Parallel()
	r := New()
	if _, _, err := r.Render(t.Context(), notes(t), "", port.RenderOptions{Page: 4, Scale: 1}); !errors.Is(err, domain.ErrNoSuchPage) {
		t.Fatalf("page 4 of 3: %v", err)
	}
	if _, _, err := r.Render(t.Context(), []byte("not an archive"), "", port.RenderOptions{}); !errors.Is(err, rmrender.ErrInvalidArgument) {
		t.Fatalf("not an archive: %v", err)
	}
	// A canceled context stops the drawing and says why.
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("stop")
	cancel(cause)
	for _, opts := range []port.RenderOptions{{}, {Page: 1, Scale: 1}} {
		if _, _, err := r.Render(ctx, notes(t), "", opts); !errors.Is(err, cause) {
			t.Fatalf("%+v: %v", opts, err)
		}
	}
}

func TestOptionMapping(t *testing.T) {
	t.Parallel()
	if paper(port.PaperLetter) != rmrender.PaperLetter || paper(port.PaperTablet) != rmrender.PaperTablet || paper("") != rmrender.PaperA4 {
		t.Fatal("paper")
	}
	if mode(port.PDFPreserving) != rmrender.PDFPreserving || mode(port.PDFRecomposed) != rmrender.PDFRecomposed || mode("") != rmrender.PDFFresh {
		t.Fatal("mode")
	}
}
