// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"sync"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// fakePage is the page format of the Pages double: JSON, so a test can
// build and read pages with no C library behind it.
type fakePage struct {
	Paragraphs []domain.Paragraph `json:"paragraphs"`
	Highlights []domain.Highlight `json:"highlights"`
	Strokes    int                `json:"strokes"`
	// Markers are the identifiers of strokes drawn with the highlighter.
	Markers []string `json:"markers,omitempty"`
}

// PageBytes is a page the Pages double reads: paragraphs, highlights and a
// number of strokes.
func PageBytes(paragraphs []domain.Paragraph, highlights []domain.Highlight, strokes int) []byte {
	return MarkedPageBytes(paragraphs, highlights, strokes, nil)
}

// MarkedPageBytes is PageBytes with strokes drawn with the highlighter,
// named by markers; they count among the strokes and the marks.
func MarkedPageBytes(paragraphs []domain.Paragraph, highlights []domain.Highlight, strokes int, markers []string) []byte {
	out, err := json.Marshal(fakePage{Paragraphs: paragraphs, Highlights: highlights, Strokes: strokes, Markers: markers}, json.Deterministic(true))
	if err != nil {
		panic(err)
	}
	return out
}

// Pages doubles the three page ports on PageBytes pages. Fail makes a method
// ("ReadPage", "Highlights", "Erase", "WritePage") return that error.
type Pages struct {
	mu    sync.Mutex
	Fail  map[string]error
	calls map[string]int
	// Drafts are the drafts WritePage received, in order.
	Drafts []domain.PageDraft
}

type pagePorts interface {
	port.PageReader
	port.PageEditor
	port.PageWriter
}

var _ pagePorts = (*Pages)(nil)

// NewPages returns the double.
func NewPages() *Pages { return &Pages{Fail: map[string]error{}, calls: map[string]int{}} }

// Count is how many times method was called.
func (p *Pages) Count(method string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[method]
}

func (p *Pages) begin(method string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls[method]++
	return p.Fail[method]
}

func decodePage(data []byte) (fakePage, error) {
	var fp fakePage
	if err := json.Unmarshal(data, &fp); err != nil {
		return fakePage{}, domain.MalformedFile("page", err.Error())
	}
	return fp, nil
}

// ReadPage implements port.PageReader.
func (p *Pages) ReadPage(data []byte) (domain.Page, error) {
	if err := p.begin("ReadPage"); err != nil {
		return domain.Page{}, err
	}
	fp, err := decodePage(data)
	if err != nil {
		return domain.Page{}, err
	}
	return domain.Page{
		Paragraphs: fp.Paragraphs,
		Highlights: fp.Highlights,
		Stats: domain.PageStats{
			Strokes: fp.Strokes, StrokesByTool: map[string]int{}, StrokesByColor: map[string]int{},
			Marks: len(fp.Highlights) + len(fp.Markers),
		},
	}, nil
}

// Highlights implements port.PageEditor.
func (p *Pages) Highlights(data []byte) ([]string, error) {
	if err := p.begin("Highlights"); err != nil {
		return nil, err
	}
	fp, err := decodePage(data)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(fp.Highlights)+len(fp.Markers))
	for _, h := range fp.Highlights {
		ids = append(ids, h.ID)
	}
	return append(ids, fp.Markers...), nil
}

// Erase implements port.PageEditor: the named highlights leave the page;
// an identifier the page lacks is refused.
func (p *Pages) Erase(data []byte, ids []string) ([]byte, error) {
	if err := p.begin("Erase"); err != nil {
		return nil, err
	}
	fp, err := decodePage(data)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !slices.ContainsFunc(fp.Highlights, func(h domain.Highlight) bool { return h.ID == id }) && !slices.Contains(fp.Markers, id) {
			return nil, domain.MalformedFile("page", fmt.Sprintf("no item %s", id))
		}
	}
	fp.Highlights = slices.DeleteFunc(fp.Highlights, func(h domain.Highlight) bool { return slices.Contains(ids, h.ID) })
	fp.Markers = slices.DeleteFunc(fp.Markers, func(m string) bool { return slices.Contains(ids, m) })
	return MarkedPageBytes(fp.Paragraphs, fp.Highlights, fp.Strokes, fp.Markers), nil
}

// WritePage implements port.PageWriter.
func (p *Pages) WritePage(d domain.PageDraft) ([]byte, error) {
	if err := p.begin("WritePage"); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.Drafts = append(p.Drafts, d)
	p.mu.Unlock()
	return PageBytes(d.Paragraphs, nil, 0), nil
}
