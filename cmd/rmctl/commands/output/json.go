// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package output

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
)

// WriteJSON writes v as indented JSON and a newline, with map keys sorted.
// Field names are the views' snake_case tags, so scripts can rely on them.
func WriteJSON(w io.Writer, v any) error {
	if err := json.MarshalWrite(w, v, jsontext.WithIndent("  "), json.Deterministic(true)); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// JSONRenderer writes a result's JSON value. An answer with no JSON value —
// a confirmation such as "registered" — is written as plain text.
type JSONRenderer struct{}

// Render implements [Renderer].
func (JSONRenderer) Render(w io.Writer, r Result, s Style) error {
	if r.JSON == nil {
		return PlainRenderer{}.Render(w, r, s)
	}
	return WriteJSON(w, r.JSON)
}

// Stream implements [Streamer]: each row's JSON value as it comes, then the
// final value, each as its own document.
func (JSONRenderer) Stream(w io.Writer, _ []Column, _ Style) (Emitter, error) {
	return jsonStream{w: w}, nil
}

type jsonStream struct{ w io.Writer }

func (j jsonStream) Row(r Row) error {
	if r.JSON == nil {
		return nil
	}
	return WriteJSON(j.w, r.JSON)
}

func (j jsonStream) End(final any) error {
	if final == nil {
		return nil
	}
	return WriteJSON(j.w, final)
}
