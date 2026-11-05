// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package library_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/porttest"
)

func TestReadManifest(t *testing.T) {
	t.Parallel()
	files := porttest.NewMemFiles()
	if _, found, err := library.ReadManifest(files, "bk"); found || err != nil {
		t.Fatalf("%v %v", found, err)
	}
	files.Write("bk/manifest.json", (domain.BackupManifest{Format: 1, Profile: "p"}).Bytes())
	m, found, err := library.ReadManifest(files, "bk")
	if !found || err != nil || m.Profile != "p" {
		t.Fatalf("%+v %v %v", m, found, err)
	}
	files.Write("bad/manifest.json", []byte("{"))
	if _, _, err = library.ReadManifest(files, "bad"); !errors.Is(err, domain.ErrBackupManifest) {
		t.Fatalf("%v", err)
	}
	if err = files.MkdirAll("dir/manifest.json"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = library.ReadManifest(files, "dir"); !errors.Is(err, domain.ErrBackupManifest) {
		t.Fatalf("%v", err)
	}
	for _, method := range []string{"Stat", "Open"} {
		files.Fail[method] = errBoom
		if _, _, err = library.ReadManifest(files, "bk"); !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", method, err)
		}
		delete(files.Fail, method)
	}
}
