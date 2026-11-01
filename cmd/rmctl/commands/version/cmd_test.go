// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package version

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/altessa-s/go-atlas-core/runtime/appinfo"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
)

func build(*cliutil.Env) []*cobra.Command { return []*cobra.Command{New()} }

func run(t *testing.T, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, cliutil.Services{}, clitest.Options{}, build, args...)
}

func TestVersion(t *testing.T) {
	t.Parallel()
	if r := run(t, "version"); r.Code != 0 || r.Out != appinfo.Version+"\n" {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	if r := run(t, "version", "--full"); r.Code != 0 || !strings.Contains(r.Out, appinfo.Version) {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	for _, args := range [][]string{{"version", "-o", "json"}, {"version", "--json"}} {
		r := run(t, args...)
		var v struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(r.Out), &v); err != nil || v.Version != appinfo.Version {
			t.Fatalf("%v: %q %v", args, r.Out, err)
		}
	}
	r := run(t, "version", "--full", "--json")
	var full fullView
	if err := json.Unmarshal([]byte(r.Out), &full); err != nil || full.GoVersion == "" || full.Platform == "" {
		t.Fatalf("%q %v", r.Out, err)
	}
	if r := run(t, "version", "-o", "yaml"); r.Code != cliutil.CodeUsage {
		t.Fatalf("%d", r.Code)
	}
}

func TestPrintFailingWriter(t *testing.T) {
	t.Parallel()
	if printText(failWriter{}, false) == nil || printJSON(failWriter{}, true) == nil {
		t.Fatal("a failing writer went unnoticed")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
