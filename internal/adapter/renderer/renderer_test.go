//go:build (darwin || linux || windows) && cgo && !norender

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package renderer

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
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
			out, report, err := draw(t.Context(), r, notes(t), "", port.RenderOptions{Mode: m, Paper: p})
			if err != nil || !bytes.HasPrefix(out, []byte("%PDF-")) || report.Pages != 3 || report.SkippedBlocks != 0 {
				t.Fatalf("%s %s: %+v %v", m, p, report, err)
			}
		}
	}
}

// Either grain draws the whole document; how differently is the backend's.
func TestRenderPDFGrain(t *testing.T) {
	t.Parallel()
	r := New()
	for _, g := range []port.PDFGrain{port.PDFGrainMask, port.PDFGrainGroup} {
		out, report, err := draw(t.Context(), r, notes(t), "", port.RenderOptions{Grain: g, Paper: port.PaperA4})
		if err != nil || !bytes.HasPrefix(out, []byte("%PDF-")) || report.Pages != 3 {
			t.Fatalf("%s: %+v %v", g, report, err)
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
		out, report, err := draw(t.Context(), r, notes(t), "a1b2", port.RenderOptions{Page: 2, Scale: c.scale, Paper: port.PaperA4})
		if err != nil || report.Pages != 1 {
			t.Fatalf("%+v %v", report, err)
		}
		config, err := png.DecodeConfig(bytes.NewReader(out))
		if err != nil || config.Width != c.width {
			t.Fatalf("scale %g: %+v %v", c.scale, config, err)
		}
	}
}

func TestRenderJPEG(t *testing.T) {
	t.Parallel()
	r := New()
	for _, c := range []struct {
		scale   float64
		quality int
		width   int
	}{{1, 0, 1404}, {0.5, 90, 702}} {
		opts := port.RenderOptions{Page: 2, Format: port.FormatJPEG, Scale: c.scale, Quality: c.quality, Paper: port.PaperA4}
		out, report, err := draw(t.Context(), r, notes(t), "", opts)
		if err != nil || report.Pages != 1 {
			t.Fatalf("%+v %v", report, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil || img.Bounds().Dx() != c.width {
			t.Fatalf("scale %g: %v", c.scale, err)
		}
	}
}

func TestRenderSVG(t *testing.T) {
	t.Parallel()
	out, report, err := draw(t.Context(), New(), notes(t), "", port.RenderOptions{Page: 3, Format: port.FormatSVG, Paper: port.PaperA4})
	if err != nil || report.Pages != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	// The page is one well-formed document whose root is <svg>.
	d := xml.NewDecoder(bytes.NewReader(out))
	var root string
	for {
		tok, terr := d.Token()
		if errors.Is(terr, io.EOF) {
			break
		}
		if terr != nil {
			t.Fatalf("%v: %.200s", terr, out)
		}
		if start, ok := tok.(xml.StartElement); ok && root == "" {
			root = start.Name.Local
		}
	}
	if root != "svg" {
		t.Fatalf("root %q", root)
	}
}

// Every page of the notebook has a lined template, which --no-template
// leaves out of each format.
func TestRenderNoTemplates(t *testing.T) {
	t.Parallel()
	r := New()
	for _, opts := range []port.RenderOptions{
		{Paper: port.PaperA4},
		{Page: 1, Format: port.FormatPNG, Scale: 0.5, Paper: port.PaperA4},
		{Page: 1, Format: port.FormatJPEG, Scale: 0.5, Paper: port.PaperA4},
		{Page: 1, Format: port.FormatSVG, Paper: port.PaperA4},
	} {
		with, _, err := draw(t.Context(), r, notes(t), "", opts)
		if err != nil {
			t.Fatalf("%+v: %v", opts, err)
		}
		opts.NoTemplates = true
		without, _, err := draw(t.Context(), r, notes(t), "", opts)
		if err != nil {
			t.Fatalf("%+v: %v", opts, err)
		}
		if bytes.Equal(with, without) {
			t.Fatalf("%+v: the same bytes without the template", opts)
		}
		// The template's lines are marks on the page, so the page without
		// it carries less: less ink, fewer SVG paths. A PDF only
		// has to differ; its size depends on the backend's compression.
		var a, b int
		switch opts.Format {
		case port.FormatPNG, port.FormatJPEG:
			a, b = inked(t, with), inked(t, without)
		case port.FormatSVG:
			a, b = bytes.Count(with, []byte("<path")), bytes.Count(without, []byte("<path"))
		default:
			continue
		}
		if b >= a {
			t.Fatalf("%+v: %d ink with the template, %d without", opts, a, b)
		}
	}
}

func TestRenderFails(t *testing.T) {
	t.Parallel()
	r := New()
	if _, _, err := draw(t.Context(), r, notes(t), "", port.RenderOptions{Page: 4, Scale: 1}); !errors.Is(err, domain.ErrNoSuchPage) {
		t.Fatalf("page 4 of 3: %v", err)
	}
	if _, _, err := draw(t.Context(), r, []byte("not an archive"), "", port.RenderOptions{}); !errors.Is(err, rmrender.ErrInvalidArgument) {
		t.Fatalf("not an archive: %v", err)
	}
	// A canceled context stops the drawing and says why.
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("stop")
	cancel(cause)
	for _, opts := range []port.RenderOptions{
		{}, {Page: 1, Scale: 1}, {Page: 1, Format: port.FormatJPEG, Scale: 1}, {Page: 1, Format: port.FormatSVG},
	} {
		if _, _, err := draw(ctx, r, notes(t), "", opts); !errors.Is(err, cause) {
			t.Fatalf("%+v: %v", opts, err)
		}
	}
}

// draw is r.Render into memory.
func draw(ctx context.Context, r *Renderer, archive []byte, id domain.ItemID, opts port.RenderOptions) ([]byte, port.RenderReport, error) {
	var b bytes.Buffer
	report, err := r.Render(ctx, archive, id, opts, &b)
	return b.Bytes(), report, err
}

// writes counts what it is written, failing every write with fail once
// failAt writes (from 1) have been made.
type writes struct {
	bytes.Buffer
	count, failAt int
	fail          error
}

func (w *writes) Write(p []byte) (int, error) {
	w.count++
	if w.failAt != 0 && w.count >= w.failAt {
		return 0, w.fail
	}
	return w.Buffer.Write(p)
}

// A PDF goes to the writer as it is drawn, up to MaxPDFBytes rather than
// rmrender-go's in-memory default, and the writer's failure is the
// drawing's.
func TestRenderPDFWrites(t *testing.T) {
	t.Parallel()
	if MaxPDFBytes <= rmrender.DefaultLimits().MaximumPDFBytes {
		t.Fatalf("MaxPDFBytes %d", MaxPDFBytes)
	}
	r := New()
	var w writes
	report, err := r.Render(t.Context(), notes(t), "", port.RenderOptions{Paper: port.PaperA4}, &w)
	if err != nil || report.Pages != 3 || w.count == 0 || !bytes.HasPrefix(w.Bytes(), []byte("%PDF-")) ||
		!bytes.HasSuffix(w.Bytes(), []byte("%%EOF\n")) {
		t.Fatalf("%+v %v: %d writes", report, err, w.count)
	}
	full := errors.New("disk full")
	for _, opts := range []port.RenderOptions{{Paper: port.PaperA4}, {Page: 1, Scale: 0.25, Paper: port.PaperA4}} {
		failing := writes{failAt: 1, fail: full}
		if _, err := r.Render(t.Context(), notes(t), "", opts, &failing); !errors.Is(err, full) {
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
	if grain(port.PDFGrainGroup) != rmrender.PDFGrainGroup || grain(port.PDFGrainMask) != rmrender.PDFGrainMask || grain("") != rmrender.PDFGrainMask {
		t.Fatal("grain")
	}
}

// inked is how much darker than white a PNG or JPEG page is, summed over
// its pixels: faint template lines count as much as their shade.
func inked(t *testing.T, data []byte) int {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			n += int(3*0xffff-r-g-bl) >> 8
		}
	}
	return n
}
