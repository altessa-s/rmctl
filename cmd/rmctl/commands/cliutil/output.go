// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/altessa-s/rmctl/internal/domain"
)

// WriteJSON writes v as indented JSON and a newline. Field names are the
// views' snake_case tags, so scripts can rely on them.
func WriteJSON(w io.Writer, v any) error {
	if err := json.MarshalWrite(w, v, jsontext.WithIndent("  "), json.Deterministic(true)); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// ItemView is an item as --json prints it.
type ItemView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Parent   string   `json:"parent"`
	Path     string   `json:"path"`
	Trashed  bool     `json:"trashed"`
	Pinned   bool     `json:"pinned"`
	Size     int64    `json:"size"`
	Modified string   `json:"modified,omitempty"`
	Created  string   `json:"created,omitempty"`
	Tags     []string `json:"tags"`
	Hash     string   `json:"hash"`
}

// View is it as --json prints it; tree gives the path and the trash state,
// and may be nil when the command has none.
func View(tree *domain.Tree, it domain.Item) ItemView {
	v := ItemView{
		ID:       string(it.ID),
		Name:     it.Name,
		Kind:     string(it.Kind),
		Parent:   string(it.Parent),
		Pinned:   it.Pinned,
		Size:     it.Size,
		Modified: Stamp(it.Modified),
		Created:  Stamp(it.Created),
		Tags:     it.Tags,
		Hash:     it.Hash,
		Trashed:  it.TrashedHere(),
	}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if tree != nil {
		v.Path = tree.Path(it.ID)
		v.Trashed = tree.IsTrashed(it.ID)
	}
	return v
}

// Views is [View] of each item.
func Views(tree *domain.Tree, items []domain.Item) []ItemView {
	out := make([]ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, View(tree, it))
	}
	return out
}

// Stamp is t in RFC 3339, or "" for the zero time.
func Stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Line is an item the way the tree shows it: the name, "/" for a folder or
// the kind in brackets, "(trashed)", "*" when pinned, then the identifier.
func Line(it domain.Item, trashed bool) string {
	var b strings.Builder
	b.WriteString(it.Name)
	if it.Kind.IsFolder() {
		b.WriteString("/")
	} else {
		b.WriteString(" [")
		b.WriteString(string(it.Kind))
		b.WriteString("]")
	}
	if trashed {
		b.WriteString(" (trashed)")
	}
	if it.Pinned {
		b.WriteString(" *")
	}
	b.WriteString("  ")
	b.WriteString(string(it.ID))
	return b.String()
}

// LongLine is [Line] after the modification time and the size.
func LongLine(it domain.Item, trashed bool) string {
	modified := "-"
	if !it.Modified.IsZero() {
		modified = it.Modified.UTC().Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%-16s %9s  %s", modified, Size(it.Size), Line(it, trashed))
}

// Size is a byte count for a person: 512 B, 1.5 KiB, 3.2 MiB.
func Size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// Notef writes a line of commentary to the error stream.
func Notef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...) //nolint:errcheck // commentary
}
