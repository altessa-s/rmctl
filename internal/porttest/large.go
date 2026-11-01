// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package porttest

import (
	"fmt"

	"github.com/altessa-s/rmctl/internal/domain"
)

// largeTags is how many distinct tags AddLarge spreads over its documents.
const largeTags = 10

// AddLarge fills the cloud with folders folders of perFolder PDFs each, for
// benchmarks: folder i is "/Folder i" (id fNNNN...), its documents are
// "Doc j" (id dNNNNMMMM...).
func (c *Cloud) AddLarge(folders, perFolder int) {
	records := make([]domain.Record, 0, folders*(perFolder+1))
	for i := range folders {
		fid := domain.ItemID(fmt.Sprintf("f%07d-0000-4000-8000-000000000000", i))
		records = append(records, c.Record(Doc{ID: fid, Name: fmt.Sprintf("Folder %d", i), Folder: true}))
		for j := range perFolder {
			did := domain.ItemID(fmt.Sprintf("d%03d%04d-0000-4000-8000-000000000000", i, j))
			records = append(records, c.Record(Doc{ID: did, Name: fmt.Sprintf("Doc %d", j), Parent: fid, Ext: "pdf",
				Tags: []string{fmt.Sprintf("t%d", j%largeTags)}}))
		}
	}
	c.SetRoot(append(c.Records(), records...))
}
