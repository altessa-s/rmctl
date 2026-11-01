// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output

import (
	"io"
	"slices"
	"sync"
)

// The names of the built-in renderers.
const (
	FormatPlain = "plain"
	FormatTable = "table"
	FormatJSON  = "json"
)

// Renderer writes a whole answer.
type Renderer interface {
	Render(w io.Writer, r Result, s Style) error
}

// Row is one row of a stream: the cells the renderers for people write, and
// the value the json renderer writes for it, if any.
type Row struct {
	Cells []Cell
	JSON  any
}

// Emitter receives the rows of a stream as they happen.
type Emitter interface {
	// Row writes one row.
	Row(r Row) error
	// End finishes the stream; final, if not nil, is the value the json
	// renderer writes once the stream is over.
	End(final any) error
}

// Streamer is a [Renderer] that writes rows as they come. A renderer that is
// not one gets the whole table at the end of the stream.
type Streamer interface {
	Stream(w io.Writer, cols []Column, s Style) (Emitter, error)
}

type registry struct {
	mu        sync.RWMutex
	renderers map[string]Renderer
}

// renderers is the process-wide registry; the built-in renderers register
// themselves when the package loads.
var renderers = &registry{renderers: map[string]Renderer{ //nolint:gochecknoglobals // the renderer registry
	FormatPlain: PlainRenderer{},
	FormatTable: TableRenderer{},
	FormatJSON:  JSONRenderer{},
}}

// Register adds a renderer under name, or replaces the one there.
func Register(name string, r Renderer) {
	renderers.mu.Lock()
	defer renderers.mu.Unlock()
	renderers.renderers[name] = r
}

// Lookup returns the renderer registered under name.
func Lookup(name string) (Renderer, bool) {
	renderers.mu.RLock()
	defer renderers.mu.RUnlock()
	r, ok := renderers.renderers[name]
	return r, ok
}

// Names lists the registered renderers in order.
func Names() []string {
	renderers.mu.RLock()
	defer renderers.mu.RUnlock()
	names := make([]string, 0, len(renderers.renderers))
	for n := range renderers.renderers {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Stream starts a stream of rows with cols through r: row by row when r is a
// [Streamer], else as one table rendered at the end.
func Stream(w io.Writer, r Renderer, cols []Column, s Style) (Emitter, error) {
	if st, ok := r.(Streamer); ok {
		return st.Stream(w, cols, s)
	}
	return &buffered{w: w, r: r, s: s, table: Table{Columns: cols}}, nil
}

// buffered collects a stream for a renderer that writes whole answers.
type buffered struct {
	w     io.Writer
	r     Renderer
	s     Style
	table Table
}

func (b *buffered) Row(r Row) error {
	b.table.Rows = append(b.table.Rows, r.Cells)
	return nil
}

func (b *buffered) End(final any) error {
	return b.r.Render(b.w, Result{JSON: final, Doc: Doc{b.table}}, b.s)
}
