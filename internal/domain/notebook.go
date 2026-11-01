// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

// Device is a tablet model a notebook is laid out for.
type Device string

// The tablet models.
const (
	DeviceRM2  Device = "rm2"
	DevicePure Device = "pure"
	DevicePro  Device = "pro"
	DeviceMove Device = "move"
)

// Sheet is the page geometry a notebook page declares.
type Sheet struct {
	ScreenWidth, ScreenHeight float64
	CanvasWidth, CanvasHeight float64
}

// TextColumn is the width setting of the typed text column.
type TextColumn int

// The text column settings and their widths.
const (
	ColumnNarrow TextColumn = 0
	ColumnMedium TextColumn = 1
	ColumnWide   TextColumn = 2
)

// PageDraft is one fresh notebook page to write.
type PageDraft struct {
	// Author is the document's author identifier, shared by every page.
	Author     [16]byte
	Sheet      Sheet
	Column     TextColumn
	Paragraphs []Paragraph
}

// NotebookPage is a written page and the identifier it is filed under.
type NotebookPage struct {
	ID   string
	Data []byte
}
