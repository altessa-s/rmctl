// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output

import (
	"io"
	"strconv"
	"strings"
)

// The layout of the table renderer.
const (
	// gap separates two columns.
	gap = "  "
	// rule draws the line under a header.
	rule = "─"
	// timeLayout is how a table shows an instant, in UTC.
	timeLayout = "2006-01-02 15:04"
	// minName is the narrowest a truncated name column becomes.
	minName = 8
	// pinMark follows the name of a pinned item.
	pinMark = " *"
)

// The tree guides.
const (
	guideMid  = "├── "
	guideLast = "└── "
	guideBar  = "│   "
	guideNone = "    "
)

// The SGR attributes the renderer uses.
const (
	sgrBold   = "1"
	sgrDim    = "2"
	sgrFolder = "1;34"
	sgrPin    = "33"
)

// TableRenderer writes answers for a person: tables with a header and a
// rule, aligned key-value records, trees drawn with guides, optional color.
type TableRenderer struct{}

// Render implements [Renderer].
func (TableRenderer) Render(w io.Writer, r Result, s Style) error {
	var b strings.Builder
	for _, block := range r.Doc {
		var part strings.Builder
		tableBlock(&part, block, s)
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

func tableBlock(b *strings.Builder, block Block, s Style) {
	switch v := block.(type) {
	case Table:
		writeTable(b, v, s)
	case Record:
		writeRecord(b, v, s)
	case Tree:
		writeTree(b, v.Nodes, s)
	case Body:
		b.WriteString(v.Text)
		if v.Text != "" && !strings.HasSuffix(v.Text, "\n") {
			b.WriteByte('\n')
		}
	}
}

// shown is a cell laid out: its text, a "/" for a folder, a pin mark, and
// the attributes to paint them with.
type shown struct {
	text   string
	folder bool
	pinned bool
	attr   string
	dim    bool
}

func (c shown) width() int {
	w := Width(c.text)
	if c.folder {
		w++
	}
	if c.pinned {
		w += len(pinMark)
	}
	return w
}

// fit truncates the text so the whole cell takes at most w columns.
func (c shown) fit(w int) shown {
	if c.width() <= w {
		return c
	}
	c.text = Truncate(c.text, w-(c.width()-Width(c.text)))
	return c
}

func (c shown) paint(color bool) string {
	text := c.text
	if c.folder {
		text += "/"
	}
	if !color {
		if c.pinned {
			text += pinMark
		}
		return text
	}
	attr := c.attr
	if c.dim && attr == "" {
		attr = sgrDim
	}
	text = sgr(attr, text)
	if c.pinned {
		text += sgr(sgrPin, pinMark)
	}
	return text
}

func sgr(attr, s string) string {
	if attr == "" || s == "" {
		return s
	}
	return "\x1b[" + attr + "m" + s + "\x1b[0m"
}

// show lays a cell out for a column of role.
func show(c Cell, role Role) shown {
	out := shown{text: displayValue(c), dim: c.Has(Trashed) || c.Has(Muted)}
	if !c.Empty() {
		out.folder = c.Has(Folder)
		out.pinned = c.Has(Pinned)
	}
	switch {
	case out.folder:
		out.attr = sgrFolder
	case role == RoleID || role == RoleKind || role == RoleHash || c.Kind == KindID:
		out.attr = sgrDim
	}
	if out.dim {
		out.attr = sgrDim
	}
	return out
}

// displayValue is a cell's text for a person: sizes in binary units, times
// to the minute in UTC, yes or no, and "-" for no value.
func displayValue(c Cell) string {
	if c.Empty() {
		return "-"
	}
	switch c.Kind {
	case KindSize:
		return HumanSize(c.Int)
	case KindCount:
		return strconv.FormatInt(c.Int, 10)
	case KindTime:
		return c.Time.UTC().Format(timeLayout)
	case KindBool:
		if c.Bool {
			return "yes"
		}
		return "no"
	default:
		return oneLine.Replace(c.Text)
	}
}

// visible are the indexes of the columns a person sees.
func visible(cols []Column) []int {
	idx := make([]int, 0, len(cols))
	for i, c := range cols {
		if !c.Detail {
			idx = append(idx, i)
		}
	}
	return idx
}

func cellAt(row []Cell, i int) Cell {
	if i < len(row) {
		return row[i]
	}
	return Cell{}
}

// rowDim reports whether a row is of an item in the trash.
func rowDim(row []Cell) bool {
	for _, c := range row {
		if c.Has(Trashed) {
			return true
		}
	}
	return false
}

func writeTable(b *strings.Builder, t Table, s Style) {
	if len(t.Rows) == 0 {
		return
	}
	cols := visible(t.Columns)
	cells := make([][]shown, len(t.Rows))
	widths := make([]int, len(cols))
	for j, ci := range cols {
		widths[j] = Width(t.Columns[ci].Title)
	}
	for i, row := range t.Rows {
		dim := rowDim(row)
		cells[i] = make([]shown, len(cols))
		for j, ci := range cols {
			c := show(cellAt(row, ci), t.Columns[ci].Role)
			if dim {
				c.dim, c.attr = true, sgrDim
			}
			cells[i][j] = c
			widths[j] = max(widths[j], c.width())
		}
	}
	shrink(widths, t.Columns, cols, s.Width)
	writeHeader(b, t.Columns, cols, widths, s.Color)
	for _, row := range cells {
		line := make([]string, len(cols))
		for j, c := range row {
			c = c.fit(widths[j])
			line[j] = pad(c.paint(s.Color), c.width(), widths[j], t.Columns[cols[j]].Right())
		}
		writeLine(b, line)
	}
}

// shrink narrows the first name or path column so the table fits width.
func shrink(widths []int, all []Column, cols []int, width int) {
	if width <= 0 {
		return
	}
	total := len(gap) * (len(widths) - 1)
	for _, w := range widths {
		total += w
	}
	if total <= width {
		return
	}
	for j, ci := range cols {
		if role := all[ci].Role; role == RoleName || role == RolePath {
			widths[j] = max(minName, Width(all[ci].Title), widths[j]-(total-width))
			return
		}
	}
}

func writeHeader(b *strings.Builder, all []Column, cols, widths []int, color bool) {
	head := make([]string, len(cols))
	rules := make([]string, len(cols))
	for j, ci := range cols {
		title := strings.ToUpper(all[ci].Title)
		head[j] = pad(sgrIf(color, sgrBold, title), Width(title), widths[j], all[ci].Right())
		rules[j] = sgrIf(color, sgrDim, strings.Repeat(rule, widths[j]))
	}
	writeLine(b, head)
	writeLine(b, rules)
}

func sgrIf(color bool, attr, s string) string {
	if !color {
		return s
	}
	return sgr(attr, s)
}

// writeLine joins a row's cells and trims the padding at its end, so no
// line ends in spaces.
func writeLine(b *strings.Builder, cells []string) {
	b.WriteString(strings.TrimRight(strings.Join(cells, gap), " "))
	b.WriteByte('\n')
}

func writeRecord(b *strings.Builder, r Record, s Style) {
	keyWidth := 0
	keys := make([]string, len(r.Fields))
	for i, f := range r.Fields {
		keys[i] = strings.ReplaceAll(f.Key, "_", " ")
		keyWidth = max(keyWidth, Width(keys[i]))
	}
	for i, f := range r.Fields {
		key := pad(sgrIf(s.Color, sgrDim, keys[i]), Width(keys[i]), keyWidth, false)
		b.WriteString(key)
		b.WriteString(gap)
		b.WriteString(show(f.Value, roleOf(f.Value.Kind)).paint(s.Color))
		b.WriteByte('\n')
	}
}

func roleOf(k Kind) Role {
	switch k {
	case KindID:
		return RoleID
	case KindSize:
		return RoleSize
	case KindCount:
		return RoleCount
	case KindTime:
		return RoleTime
	case KindPath:
		return RolePath
	default:
		return RoleText
	}
}

// treeLine is one node of a tree laid out: its guides and its cells.
type treeLine struct {
	prefix string
	cells  []shown
	right  []bool
}

// writeTree draws nodes: the top level has no guide, every level below it
// hangs from its parent with ├──, └── and │. The cells of nodes with as many
// cells line up in columns, the first one after the guides.
func writeTree(b *strings.Builder, nodes []Node, s Style) {
	var lines []treeLine
	collectTree(&lines, nodes, "", true)
	widths := map[[2]int]int{}
	for _, l := range lines {
		for j, c := range l.cells {
			w := c.width()
			if j == 0 {
				w += Width(l.prefix)
			}
			key := [2]int{len(l.cells), j}
			widths[key] = max(widths[key], w)
		}
	}
	for _, l := range lines {
		parts := make([]string, len(l.cells))
		for j, c := range l.cells {
			w, cw := widths[[2]int{len(l.cells), j}], c.width()
			text := c.paint(s.Color)
			if j == 0 {
				text = sgrIf(s.Color, sgrDim, l.prefix) + text
				cw += Width(l.prefix)
			}
			parts[j] = pad(text, cw, w, l.right[j])
		}
		writeLine(b, parts)
	}
}

func collectTree(lines *[]treeLine, nodes []Node, prefix string, top bool) {
	for i, n := range nodes {
		last := i == len(nodes)-1
		guide, next := "", ""
		if !top {
			guide, next = guideMid, prefix+guideBar
			if last {
				guide, next = guideLast, prefix+guideNone
			}
		}
		dim := rowDim(n.Cells)
		l := treeLine{prefix: prefix + guide, cells: make([]shown, len(n.Cells)), right: make([]bool, len(n.Cells))}
		for j, c := range n.Cells {
			sh := show(c, roleOf(c.Kind))
			if dim {
				sh.dim, sh.attr = true, sgrDim
			}
			l.cells[j] = sh
			l.right[j] = j > 0 && (c.Kind == KindSize || c.Kind == KindCount)
		}
		*lines = append(*lines, l)
		collectTree(lines, n.Children, next, false)
	}
}
