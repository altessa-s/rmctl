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

	out "github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
)

func build(env *cliutil.Env) []*cobra.Command { return []*cobra.Command{New(env)} }

func run(t *testing.T, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, cliutil.Services{}, clitest.Options{}, build, args...)
}

// version --full says whether the build draws documents, so rmctl-lite is
// told from rmctl.
func TestVersionRender(t *testing.T) {
	t.Parallel()
	for _, renders := range []bool{true, false} {
		want := map[bool]string{true: "render\tyes\n", false: "render\tno\n"}[renders]
		opts := clitest.Options{Renders: renders}
		if r := clitest.Exec(t, cliutil.Services{}, opts, build, "version", "--full"); r.Code != 0 || !strings.Contains(r.Out, want) {
			t.Fatalf("%v: %d %q", renders, r.Code, r.Out)
		}
		r := clitest.Exec(t, cliutil.Services{}, opts, build, "version", "--full", "-o", "json")
		var full fullView
		if err := json.Unmarshal([]byte(r.Out), &full); err != nil || full.Render != renders {
			t.Fatalf("%v: %q %v", renders, r.Out, err)
		}
		if r = clitest.Exec(t, cliutil.Services{}, opts, build, "version"); strings.Contains(r.Out, "render") {
			t.Fatalf("%v: %q", renders, r.Out)
		}
	}
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
	if (out.PlainRenderer{}).Render(failWriter{}, result(false, false), out.Style{}) == nil ||
		(out.JSONRenderer{}).Render(failWriter{}, result(true, true), out.Style{}) == nil {
		t.Fatal("a failing writer went unnoticed")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
