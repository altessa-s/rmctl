// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"fmt"
	"io"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/domain"
)

// WriteJSON writes v as indented JSON and a newline. Field names are the
// views' snake_case tags, so scripts can rely on them.
func WriteJSON(w io.Writer, v any) error { return output.WriteJSON(w, v) }

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

// Size is a byte count for a person: 512 B, 1.5 KiB, 3.2 MiB.
func Size(n int64) string { return output.HumanSize(n) }

// ItemColumns are the columns of a list of items: the name and the kind,
// with long the modification time and the size, then the identifier, and
// whether the item is pinned or in the trash.
func ItemColumns(long bool) []output.Column {
	cols := []output.Column{{Title: "name", Role: output.RoleName}, {Title: "kind", Role: output.RoleKind}}
	if long {
		cols = append(cols, output.Column{Title: "modified", Role: output.RoleTime}, output.Column{Title: "size", Role: output.RoleSize})
	}
	return append(cols,
		output.Column{Title: "id", Role: output.RoleID},
		output.Column{Title: "pinned", Detail: true},
		output.Column{Title: "trashed", Detail: true})
}

// ItemRow is it as a row of [ItemColumns].
func ItemRow(it domain.Item, trashed, long bool) []output.Cell {
	row := []output.Cell{NameCell(it, trashed), output.Text(string(it.Kind))}
	if long {
		size := output.Size(it.Size)
		if it.Kind.IsFolder() {
			size = output.Text("")
		}
		row = append(row, output.Time(it.Modified), size)
	}
	return append(row, output.ID(string(it.ID)), output.Bool(it.Pinned), output.Bool(trashed))
}

// NameCell is an item's name, flagged as a folder, pinned or in the trash.
func NameCell(it domain.Item, trashed bool) output.Cell {
	return output.Text(it.Name).With(itemFlags(it, trashed))
}

func itemFlags(it domain.Item, trashed bool) output.Flag {
	var f output.Flag
	if it.Kind.IsFolder() {
		f |= output.Folder
	}
	if it.Pinned {
		f |= output.Pinned
	}
	if trashed {
		f |= output.Trashed
	}
	return f
}

// Items is the answer listing items: a table of [ItemColumns], and their
// views as JSON. tree gives the trash state and may be nil.
func Items(tree *domain.Tree, items []domain.Item, long bool) output.Result {
	t := output.Table{Columns: ItemColumns(long)}
	for _, it := range items {
		trashed := it.TrashedHere()
		if tree != nil {
			trashed = tree.IsTrashed(it.ID)
		}
		t.Rows = append(t.Rows, ItemRow(it, trashed, long))
	}
	return output.Result{JSON: Views(tree, items), Doc: output.Doc{t}}
}

// Notef writes a line of commentary to the error stream.
func Notef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...) //nolint:errcheck // commentary
}
