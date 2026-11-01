// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package clitest

import (
	"os"
	"path/filepath"
	"testing"
)

// The modes of the golden files and their directory.
const (
	dirMode  = 0o750
	fileMode = 0o600
)

// UpdateEnv set to 1 rewrites the golden files instead of comparing:
// RMCTL_UPDATE_GOLDEN=1 go test ./cmd/...
const UpdateEnv = "RMCTL_UPDATE_GOLDEN"

// Golden compares got with testdata/<name>.golden of the calling package,
// rewriting the file instead when $RMCTL_UPDATE_GOLDEN is 1.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv(UpdateEnv) == "1" {
		if err := os.MkdirAll("testdata", dirMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), fileMode); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) // #nosec G304 -- a golden file of the test
	if err != nil {
		t.Fatalf("%v (run with RMCTL_UPDATE_GOLDEN=1 to create it)", err)
		return
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}
