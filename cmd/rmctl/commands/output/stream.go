// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output

import (
	"io"
	"strings"
)

// The widths a streamed table gives its columns, which it must choose before
// the first row arrives.
const (
	streamID       = 36
	streamTime     = len(timeLayout)
	streamSize     = 10
	streamCount    = 7
	streamKind     = 13
	streamHash     = 16
	streamFlexible = 30
	streamMinFlex  = 12
)

// Stream implements [Streamer]: the header at once, then each row as it
// comes, in columns as wide as their roles need; a value longer than its
// column is truncated when it is free text, a name or a path, and overflows
// otherwise.
func (TableRenderer) Stream(w io.Writer, cols []Column, s Style) (Emitter, error) {
	t := &tableStream{w: w, all: cols, cols: visible(cols), s: s}
	t.widths = streamWidths(cols, t.cols, s.Width)
	var b strings.Builder
	writeHeader(&b, cols, t.cols, t.widths, s.Color)
	if _, err := io.WriteString(w, b.String()); err != nil {
		return nil, err
	}
	return t, nil
}

type tableStream struct {
	w      io.Writer
	all    []Column
	cols   []int
	widths []int
	s      Style
}

func (t *tableStream) Row(r Row) error {
	dim := rowDim(r.Cells)
	line := make([]string, len(t.cols))
	for j, ci := range t.cols {
		col := t.all[ci]
		c := show(cellAt(r.Cells, ci), col.Role)
		if dim {
			c.dim, c.attr = true, sgrDim
		}
		if flexible(col.Role) {
			c = c.fit(t.widths[j])
		}
		line[j] = pad(c.paint(t.s.Color), c.width(), t.widths[j], col.Right())
	}
	var b strings.Builder
	writeLine(&b, line)
	_, err := io.WriteString(t.w, b.String())
	return err
}

func (*tableStream) End(any) error { return nil }

func flexible(r Role) bool { return r == RoleName || r == RolePath || r == RoleText }

// streamWidths gives each fixed role its width and shares what the terminal
// has left among the flexible columns.
func streamWidths(all []Column, cols []int, width int) []int {
	widths := make([]int, len(cols))
	fixed, flex := len(gap)*(len(cols)-1), 0
	for j, ci := range cols {
		switch all[ci].Role {
		case RoleID:
			widths[j] = streamID
		case RoleTime:
			widths[j] = streamTime
		case RoleSize:
			widths[j] = streamSize
		case RoleCount:
			widths[j] = streamCount
		case RoleKind:
			widths[j] = streamKind
		case RoleHash:
			widths[j] = streamHash
		default:
			flex++
			continue
		}
		widths[j] = max(widths[j], Width(all[ci].Title))
		fixed += widths[j]
	}
	share := streamFlexible
	if width > 0 && flex > 0 {
		share = max(streamMinFlex, (width-fixed)/flex)
	}
	for j, ci := range cols {
		if flexible(all[ci].Role) {
			widths[j] = max(share, Width(all[ci].Title))
		}
	}
	return widths
}
