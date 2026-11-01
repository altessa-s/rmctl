// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output

import (
	"io"
	"strconv"
	"strings"
	"time"
)

// PlainRenderer writes the stable, line-oriented text the package
// documentation specifies: no header, no color, fields separated by a TAB,
// raw values.
type PlainRenderer struct{}

// Render implements [Renderer].
func (PlainRenderer) Render(w io.Writer, r Result, _ Style) error {
	var b strings.Builder
	for _, block := range r.Doc {
		var part strings.Builder
		plainBlock(&part, block)
		if part.Len() == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part.String())
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Stream implements [Streamer]: each row is written as it comes.
func (PlainRenderer) Stream(w io.Writer, _ []Column, _ Style) (Emitter, error) {
	return plainStream{w: w}, nil
}

type plainStream struct{ w io.Writer }

func (p plainStream) Row(r Row) error {
	var b strings.Builder
	plainCells(&b, r.Cells)
	_, err := io.WriteString(p.w, b.String())
	return err
}

func (plainStream) End(any) error { return nil }

func plainBlock(b *strings.Builder, block Block) {
	switch v := block.(type) {
	case Table:
		for _, row := range v.Rows {
			plainCells(b, row)
		}
	case Record:
		for _, f := range v.Fields {
			b.WriteString(f.Key)
			b.WriteByte('\t')
			b.WriteString(PlainValue(f.Value))
			b.WriteByte('\n')
		}
	case Tree:
		plainNodes(b, v.Nodes, 0)
	case Body:
		b.WriteString(v.Text)
		if v.Text != "" && !strings.HasSuffix(v.Text, "\n") {
			b.WriteByte('\n')
		}
	}
}

func plainNodes(b *strings.Builder, nodes []Node, depth int) {
	for _, n := range nodes {
		b.WriteString(strings.Repeat("  ", depth))
		plainCells(b, n.Cells)
		plainNodes(b, n.Children, depth+1)
	}
}

func plainCells(b *strings.Builder, cells []Cell) {
	for i, c := range cells {
		if i > 0 {
			b.WriteByte('\t')
		}
		b.WriteString(PlainValue(c))
	}
	b.WriteByte('\n')
}

// escaper keeps a value on one field: a TAB, a line break or a backslash in
// it is written as \t, \n, \r or \\.
var escaper = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`) //nolint:gochecknoglobals // immutable

// PlainValue is a cell as the plain renderer writes it: text verbatim (with
// TAB, line breaks and backslashes escaped), sizes and counts as integers,
// times in RFC 3339 UTC, booleans as true or false, and "-" for no value.
func PlainValue(c Cell) string {
	if c.Empty() {
		return "-"
	}
	switch c.Kind {
	case KindSize, KindCount:
		return strconv.FormatInt(c.Int, 10)
	case KindTime:
		return c.Time.UTC().Format(time.RFC3339)
	case KindBool:
		return strconv.FormatBool(c.Bool)
	default:
		return escaper.Replace(c.Text)
	}
}
