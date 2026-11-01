// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package domain

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
)

// PageOrder is the order of a document's pages, read from its .content: the
// identifiers of its live pages, first to last. Both layouts are read:
// formatVersion 1 lists the identifiers in pages; formatVersion 2 keeps
// cPages.pages, each page with an idx key that orders it and, once the page
// is gone, a deleted record with a non-zero value. A .content without pages
// has none.
func PageOrder(content []byte) ([]string, error) {
	var c struct {
		Pages  jsontext.Value `json:"pages"`
		CPages *struct {
			Pages []cPage `json:"pages"`
		} `json:"cPages"`
	}
	if err := json.Unmarshal(content, &c); err != nil {
		return nil, MalformedFile("content", err.Error())
	}
	if c.CPages != nil {
		return orderCPages(c.CPages.Pages), nil
	}
	if len(c.Pages) == 0 || c.Pages.Kind() == 'n' {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal(c.Pages, &ids); err != nil {
		return nil, MalformedFile("content", "pages is not a list of identifiers")
	}
	return slices.DeleteFunc(ids, func(id string) bool { return id == "" }), nil
}

type cPage struct {
	ID  string `json:"id"`
	Idx struct {
		Value string `json:"value"`
	} `json:"idx"`
	Deleted *struct {
		Value jsontext.Value `json:"value"`
	} `json:"deleted"`
}

// deleted reports a page the tablet removed: a deleted record whose value
// is anything but zero, false or null.
func (p cPage) deleted() bool {
	if p.Deleted == nil {
		return false
	}
	switch string(p.Deleted.Value) {
	case "", "0", "false", "null":
		return false
	default:
		return true
	}
}

func orderCPages(pages []cPage) []string {
	live := slices.DeleteFunc(slices.Clone(pages), func(p cPage) bool { return p.ID == "" || p.deleted() })
	// The idx keys sort as strings; two pages of one key keep the file's order.
	slices.SortStableFunc(live, func(a, b cPage) int { return cmp.Compare(a.Idx.Value, b.Idx.Value) })
	ids := make([]string, len(live))
	for i, p := range live {
		ids[i] = p.ID
	}
	return ids
}

// PageFileName is the record name of a page of a document.
func PageFileName(doc ItemID, page string) string {
	return string(doc) + "/" + page + ".rm"
}
