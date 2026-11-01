// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package pdf_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/altessa-s/rmctl/internal/adapter/pdf"
	"github.com/altessa-s/rmctl/internal/domain"
)

// makePDF writes a valid PDF of n empty pages, cross-reference table and all.
func makePDF(t testing.TB, n int) string {
	t.Helper()
	var objs []string
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>")
	kids := ""
	for i := range n {
		kids += fmt.Sprintf("%d 0 R ", 3+i)
	}
	objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, n))
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
	path := filepath.Join(t.TempDir(), fmt.Sprintf("doc-%d.pdf", n))
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPages(t *testing.T) {
	t.Parallel()
	in := pdf.New()
	for _, n := range []int{1, 3, 12} {
		got, err := in.Pages(makePDF(t, n))
		if err != nil || got != n {
			t.Fatalf("%d pages: got %d, %v", n, got, err)
		}
	}
}

func TestRefusesWhatTheTabletCannotOpen(t *testing.T) {
	t.Parallel()
	in := pdf.New()
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pdf")
	if err := os.WriteFile(garbage, []byte("not a pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked.pdf")
	conf := model.NewAESConfiguration("user-pw", "owner-pw", 256)
	if err := api.EncryptFile(t.Context(), makePDF(t, 2), locked, conf); err != nil {
		t.Fatal(err)
	}
	empty := makePDF(t, 0)
	for name, path := range map[string]string{"garbage": garbage, "locked": locked, "missing": filepath.Join(dir, "none.pdf"), "no pages": empty} {
		if _, err := in.Pages(path); !errors.Is(err, domain.ErrUnsupportedFile) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}

func BenchmarkPages(b *testing.B) {
	in := pdf.New()
	path := makePDF(b, 100)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := in.Pages(path); err != nil {
			b.Fatal(err)
		}
	}
}
