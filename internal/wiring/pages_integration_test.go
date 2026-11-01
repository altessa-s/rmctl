//go:build integration && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// The page features end to end: the use cases, the rmlines adapter and the
// real transport, against the emulated cloud, on pages the tablet wrote.

package wiring_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/clouddouble"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/wiring"
)

func tabletPage(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func connected(t *testing.T) (wiring.Services, *clouddouble.Cloud) {
	t.Helper()
	cloud := clouddouble.New(t)
	opts := options(t)
	opts.Client = cloud.Options()
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.Account.Register(t.Context(), clouddouble.ValidCode, false); err != nil {
		t.Fatal(err)
	}
	return svc, cloud
}

func TestIntegration_PagesOfTabletDocuments(t *testing.T) {
	t.Parallel()
	svc, cloud := connected(t)
	cloud.Seed(t,
		clouddouble.Item{ID: "n0000000-0000-4000-8000-000000000001", Name: "Typed", Kind: clouddouble.Notebook,
			PageBodies: [][]byte{tabletPage(t, "Bold_Heading_Bullet_Normal.rm")}},
		clouddouble.Item{ID: "p0000000-0000-4000-8000-000000000001", Name: "Wiki", Kind: clouddouble.PDF,
			PageBodies: [][]byte{tabletPage(t, "Wikipedia_highlighted_p1.rm")}},
	)

	text, err := svc.Pages.Pages(t.Context(), "/Typed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(text.Pages) != 1 || len(text.Pages[0].Paragraphs) != 4 || text.Pages[0].Paragraphs[1].Style != domain.StyleHeading {
		t.Fatalf("%+v", text.Pages)
	}

	wiki, err := svc.Pages.Pages(t.Context(), "/Wiki", nil)
	if err != nil || len(wiki.Pages) != 1 || len(wiki.Pages[0].Highlights) != 4 {
		t.Fatalf("%+v %v", wiki, err)
	}

	_, found, err := svc.Browser.Find(t.Context(), port.FindQuery{Text: "operating system"})
	if err != nil || len(found) != 1 || found[0].Name != "Wiki" {
		t.Fatalf("find: %v %v", found, err)
	}

	report, err := svc.Pages.Unmark(t.Context(), "/Wiki", nil)
	if err != nil || report.Erased != 4 || report.Pages != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	wiki, err = svc.Pages.Pages(t.Context(), "/Wiki", nil)
	if err != nil || len(wiki.Pages[0].Highlights) != 0 {
		t.Fatalf("after unmark: %+v %v", wiki.Pages, err)
	}
	if _, found, err = svc.Browser.Find(t.Context(), port.FindQuery{Text: "operating system"}); err != nil || len(found) != 0 {
		t.Fatalf("an erased highlight is still found: %v %v", found, err)
	}
}

func TestIntegration_NotebookFromMarkdown(t *testing.T) {
	t.Parallel()
	svc, cloud := connected(t)
	var md strings.Builder
	md.WriteString("# Field notes\n\n")
	for i := range 80 {
		md.WriteString("- an observation written down on the walk, number ")
		md.WriteString(strings.Repeat("x", i%7))
		md.WriteString("\n")
	}
	local := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(local, []byte(md.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	it, err := svc.Pages.Notebook(t.Context(), local, "", port.NotebookOptions{Tags: []string{"walk"}, Device: domain.DeviceRM2})
	if err != nil {
		t.Fatal(err)
	}
	if it.Kind != domain.KindNotebook || it.Name != "notes" {
		t.Fatalf("%+v", it)
	}
	got, err := svc.Pages.Pages(t.Context(), string(it.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pages) < 2 {
		t.Fatalf("%d pages for 81 paragraphs", len(got.Pages))
	}
	var paras []domain.Paragraph
	for _, p := range got.Pages {
		paras = append(paras, p.Paragraphs...)
	}
	if len(paras) != 81 || paras[0] != (domain.Paragraph{Text: "Field notes", Style: domain.StyleHeading}) || paras[1].Style != domain.StyleBullet {
		t.Fatalf("%d paragraphs, first %+v", len(paras), paras[:2])
	}
	meta := cloud.Metadata(t, string(it.ID))
	if meta["type"] != "DocumentType" || meta["visibleName"] != "notes" {
		t.Fatalf("%v", meta)
	}
	names := []string{}
	for _, f := range cloud.Files(t, string(it.ID)) {
		names = append(names, f.DocumentID)
	}
	if !slices.ContainsFunc(names, func(n string) bool { return strings.HasSuffix(n, ".pagedata") }) {
		t.Fatalf("%v", names)
	}
}
