// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output

import "time"

// Kind is what a cell holds, which decides how each renderer writes it.
type Kind uint8

// The kinds of cell.
const (
	// KindText is free text.
	KindText Kind = iota
	// KindID is an identifier, written verbatim.
	KindID
	// KindSize is a byte count.
	KindSize
	// KindTime is an instant.
	KindTime
	// KindBool is true or false.
	KindBool
	// KindCount is a whole number.
	KindCount
	// KindPath is a library or local path.
	KindPath
)

// Flag marks a cell, or a row through its cells, for the renderers that show
// state by style: a folder, an item in the trash, a pinned item, a value of
// little interest.
type Flag uint8

// The flags of a cell.
const (
	// Folder marks the name of a folder.
	Folder Flag = 1 << iota
	// Trashed marks a cell of an item in the trash.
	Trashed
	// Pinned marks the name of a pinned item.
	Pinned
	// Muted marks a value of little interest.
	Muted
)

// Cell is one typed value. The zero Cell is empty text.
type Cell struct {
	Kind  Kind
	Text  string
	Int   int64
	Time  time.Time
	Bool  bool
	Flags Flag
}

// Text is a text cell.
func Text(s string) Cell { return Cell{Kind: KindText, Text: s} }

// ID is an identifier cell.
func ID(s string) Cell { return Cell{Kind: KindID, Text: s} }

// Path is a path cell.
func Path(s string) Cell { return Cell{Kind: KindPath, Text: s} }

// Size is a byte-count cell.
func Size(n int64) Cell { return Cell{Kind: KindSize, Int: n} }

// Count is a whole-number cell.
func Count(n int) Cell { return Cell{Kind: KindCount, Int: int64(n)} }

// Time is an instant cell; the zero time is empty.
func Time(t time.Time) Cell { return Cell{Kind: KindTime, Time: t} }

// Bool is a true-or-false cell.
func Bool(b bool) Cell { return Cell{Kind: KindBool, Bool: b} }

// With is c with flags added.
func (c Cell) With(flags Flag) Cell {
	c.Flags |= flags
	return c
}

// Has reports whether c carries flag.
func (c Cell) Has(flag Flag) bool { return c.Flags&flag != 0 }

// Empty reports whether c has no value to show.
func (c Cell) Empty() bool {
	switch c.Kind {
	case KindText, KindID, KindPath:
		return c.Text == ""
	case KindTime:
		return c.Time.IsZero()
	default:
		return false
	}
}

// Role is what a column means, which decides its alignment and style in the
// renderers for people.
type Role uint8

// The roles of a column.
const (
	// RoleText is any other value.
	RoleText Role = iota
	// RoleName is an item's name: the column a narrow terminal truncates.
	RoleName
	// RoleID is an identifier.
	RoleID
	// RoleKind is an item's kind.
	RoleKind
	// RoleSize is a byte count.
	RoleSize
	// RoleTime is an instant.
	RoleTime
	// RoleCount is a whole number.
	RoleCount
	// RolePath is a path: truncated like a name.
	RolePath
	// RoleHash is a content hash.
	RoleHash
)

// Align places a value in its column.
type Align uint8

// The alignments: the zero value follows the role.
const (
	// AlignAuto right-aligns sizes and counts and left-aligns the rest.
	AlignAuto Align = iota
	// AlignLeft pads on the right.
	AlignLeft
	// AlignRight pads on the left.
	AlignRight
)

// Column describes one column of a table.
type Column struct {
	// Title names the column; a renderer may change its case.
	Title string
	Role  Role
	Align Align
	// Detail marks a column whose content the renderers for people show by
	// style instead (pinned, trashed): they may leave it out.
	Detail bool
}

// Right reports whether the column's values are right-aligned.
func (c Column) Right() bool {
	switch c.Align {
	case AlignRight:
		return true
	case AlignLeft:
		return false
	default:
		return c.Role == RoleSize || c.Role == RoleCount
	}
}

// Block is one part of a [Doc]: a [Table], a [Record], a [Tree] or a [Body].
type Block interface{ block() }

// Table is a list of records with the same columns.
type Table struct {
	Columns []Column
	Rows    [][]Cell
}

// Field is one key and its value.
type Field struct {
	// Key is lowercase snake_case, the JSON field name where there is one.
	Key   string
	Value Cell
}

// Record is one record as keys and values.
type Record struct {
	Fields []Field
}

// Node is one line of a tree and the nodes below it.
type Node struct {
	Cells    []Cell
	Children []Node
}

// Tree is nested nodes.
type Tree struct {
	Nodes []Node
}

// Body is opaque text written as it is: page text, a file's content.
type Body struct {
	Text string
}

func (Table) block()  {}
func (Record) block() {}
func (Tree) block()   {}
func (Body) block()   {}

// Doc is an answer as blocks, in order.
type Doc []Block

// Result is one answer: JSON is the value the json renderer writes (the
// command's view struct), Doc what the renderers for people write.
type Result struct {
	JSON any
	Doc  Doc
}

// Style is what a renderer knows of where it writes.
type Style struct {
	// Color allows ANSI color and attributes.
	Color bool
	// Width is the terminal's width in columns; 0 is unknown.
	Width int
}
