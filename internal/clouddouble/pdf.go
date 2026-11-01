// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clouddouble

import (
	"bytes"
	"fmt"
	"strings"
)

// MakePDF is a valid PDF of n empty pages, cross-reference table and all:
// enough for a page counter to read, and nothing else in it.
func MakePDF(n int) []byte {
	// Object 1 is the catalog, 2 the page tree, and the pages follow.
	const firstPage = 3
	objs := make([]string, 0, firstPage-1+n)
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>")
	var kids strings.Builder
	for i := range n {
		fmt.Fprintf(&kids, "%d 0 R ", firstPage+i)
	}
	objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), n))
	for range n {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>")
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}
