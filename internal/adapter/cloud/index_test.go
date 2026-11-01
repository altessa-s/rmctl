// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// The split reads give what Root and Files give, and the index bytes when
// the hash is theirs: a node-hashed index comes parsed, without them.
func TestPointerAndIndexes(t *testing.T) {
	t.Parallel()
	double, c := registered(t)
	double.Seed(t,
		clouddouble.Item{ID: "by-file", Name: "File", Kind: clouddouble.PDF},
		clouddouble.Item{ID: "by-node", Name: "Node", Kind: clouddouble.PDF, ByNodeHash: true},
		clouddouble.Item{ID: "v3-node", Name: "Old", Kind: clouddouble.Notebook, Pages: 1, IndexV3: true, ByNodeHash: true},
	)
	state, err := c.Root(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	hash, gen, err := c.Pointer(t.Context())
	if err != nil || hash != state.Hash || gen != state.Generation {
		t.Fatalf("%q %d %v", hash, gen, err)
	}
	raw, records, err := c.RootIndex(t.Context(), hash)
	if err != nil || raw == nil || fmt.Sprint(records) != fmt.Sprint(state.Records) {
		t.Fatalf("root index: %v %v", records, err)
	}
	if decoded, err := c.DecodeRoot(raw, hash); err != nil || fmt.Sprint(decoded) != fmt.Sprint(records) {
		t.Fatalf("decoded root: %v %v", decoded, err)
	}
	if _, err := c.DecodeRoot(append(raw, '\n'), hash); err == nil {
		t.Fatal("altered root bytes decoded")
	}
	for _, rec := range state.Records {
		want, err := c.Files(t.Context(), rec)
		if err != nil {
			t.Fatal(err)
		}
		raw, files, err := c.Index(t.Context(), rec)
		if err != nil || fmt.Sprint(files) != fmt.Sprint(want) {
			t.Fatalf("%s: %v %v", rec.ID, files, err)
		}
		sum := sha256.Sum256(raw)
		if byFile := raw != nil; byFile != (hex.EncodeToString(sum[:]) == rec.Hash) || byFile == (rec.ID != "by-file") {
			t.Fatalf("%s: bytes %v", rec.ID, byFile)
		}
		if raw != nil {
			if decoded, err := c.DecodeFiles(raw, rec.Hash); err != nil || fmt.Sprint(decoded) != fmt.Sprint(want) {
				t.Fatalf("%s: %v %v", rec.ID, decoded, err)
			}
		}
	}
	if _, err := c.DecodeFiles([]byte("garbage"), hash); err == nil {
		t.Fatal("garbage decoded")
	}
	if _, _, err := c.Index(t.Context(), state.Records[0]); err != nil {
		t.Fatal(err)
	}
	missing := state.Records[0]
	missing.Hash = hex.EncodeToString(make([]byte, 32))
	if _, _, err := c.Index(t.Context(), missing); err == nil {
		t.Fatal("a missing index read")
	}
	if _, _, err := c.RootIndex(t.Context(), missing.Hash); err == nil {
		t.Fatal("a missing root index read")
	}
}
