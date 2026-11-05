// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// The layers of Clean Architecture, checked on the imports of every package.
// depguard in .golangci.yml keeps the same lines for production code; this
// test is the one that fails the build when a new package has no rule.

package rmctl_test

import (
	"go/build"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const module = "github.com/altessa-s/rmctl/"

// transport is rmcloud-go. The domain, the ports, the use cases and the
// command packages never import it; the adapters that stand on it may (cloud
// over its client, secretfile and keychain as its SecretStore, vault over a
// SecretStore, config for its endpoints), and so may the composition root
// (internal/wiring and cmd/rmctl), which builds the client.
const transport = "github.com/altessa-s/rmcloud-go"

// pageFormat is the .rm library only the rmpages adapter may import.
const pageFormat = "github.com/altessa-s/rmlines-go"

// archiveFormat is the .rmdoc library only the rmdocs adapter may import.
const archiveFormat = "github.com/altessa-s/rmdoc-go"

// renderLib is the renderer only the renderer adapter may import, with
// its bundled fonts below it.
const renderLib = "github.com/altessa-s/rmrender-go"

// keychainLib is the Keychain binding only keychain/system may import, and
// configFormat the TOML parser only the config adapter may.
const (
	keychainLib  = "github.com/keybase/go-keychain"
	configFormat = "github.com/pelletier/go-toml/v2"
)

type rule struct {
	// allow are the packages of this module the layer may import, relative
	// to the module root.
	allow []string
	// forbid are outside packages it may not import.
	forbid []string
}

func useCase(allow ...string) rule {
	return rule{
		allow:  append([]string{"internal/domain", "internal/port"}, allow...),
		forbid: []string{"os", "net/http", "github.com/spf13/cobra", transport, pageFormat, archiveFormat, renderLib, keychainLib, configFormat},
	}
}

// adapter is an adapter that may import own, the one outside framework it
// stands on, and none of the others ("" for none).
func adapter(own string) rule {
	r := rule{allow: []string{"internal/domain", "internal/port"}, forbid: []string{"github.com/spf13/cobra"}}
	for _, framework := range []string{transport, pageFormat, archiveFormat, renderLib, keychainLib, configFormat} {
		if framework != own {
			r.forbid = append(r.forbid, framework)
		}
	}
	return r
}

// command is a command package of the command line: the input ports and the
// model, and its own helpers.
func command() rule {
	return rule{
		allow:  []string{"internal/domain", "internal/port", "cmd/rmctl/commands/cliutil", "cmd/rmctl/commands/output"},
		forbid: []string{transport, pageFormat, archiveFormat, renderLib, keychainLib, configFormat},
	}
}

var layers = map[string]rule{
	"internal/domain": {forbid: []string{"os", "syscall", "log/slog", "net", "net/http", "io/fs", transport, pageFormat, archiveFormat, renderLib}},
	"internal/port":   {allow: []string{"internal/domain"}, forbid: []string{"os", "net/http", transport, pageFormat, archiveFormat, renderLib}},

	"internal/app/library":  useCase(),
	"internal/app/account":  useCase(),
	"internal/app/browse":   useCase("internal/app/library", "internal/app/pages"),
	"internal/app/pages":    useCase("internal/app/library"),
	"internal/app/fetch":    useCase("internal/app/library"),
	"internal/app/write":    useCase("internal/app/library"),
	"internal/app/watch":    useCase(),
	"internal/app/inspect":  useCase("internal/app/library"),
	"internal/app/archives": useCase("internal/app/pages"),
	"internal/app/secrets":  useCase(),
	"internal/app/render":   useCase("internal/app/library", "internal/app/fetch"),
	"internal/app/profile":  useCase("internal/app/account"),
	"internal/app/prefetch": useCase("internal/app/library"),

	"internal/adapter/cloud":      adapter(transport),
	"internal/adapter/config":     {allow: []string{"internal/domain"}, forbid: []string{keychainLib}},
	"internal/adapter/secretfile": adapter(transport),
	"internal/adapter/localfs":    adapter(""),
	"internal/adapter/pdf":        adapter(""),
	"internal/adapter/blobcache":  adapter(""),
	"internal/adapter/ids":        adapter(""),
	"internal/adapter/clock":      adapter(""),
	"internal/adapter/rmpages":    adapter(pageFormat),
	"internal/adapter/rmdocs":     adapter(archiveFormat),
	"internal/adapter/renderer":   adapter(renderLib),
	"internal/adapter/vault":      adapter(transport),
	"internal/adapter/indexcache": adapter(""),
	"internal/adapter/keychain": func() rule {
		r := adapter(transport)
		r.allow = append(r.allow, "internal/adapter/keychain/system")
		return r
	}(),
	"internal/adapter/keychain/system": adapter(keychainLib),

	"internal/wiring": {allow: []string{
		"internal/domain", "internal/port",
		"internal/app/library", "internal/app/account", "internal/app/browse", "internal/app/fetch",
		"internal/app/write", "internal/app/watch", "internal/app/inspect", "internal/app/pages", "internal/app/archives",
		"internal/adapter/cloud", "internal/adapter/config", "internal/adapter/secretfile", "internal/adapter/localfs",
		"internal/adapter/pdf", "internal/adapter/blobcache", "internal/adapter/ids", "internal/adapter/clock",
		"internal/adapter/rmpages", "internal/adapter/rmdocs", "internal/adapter/keychain", "internal/adapter/vault",
		"internal/app/secrets", "internal/adapter/indexcache", "internal/app/render", "internal/adapter/renderer",
		"internal/app/profile", "internal/app/prefetch",
	}},

	"internal/porttest":    {allow: []string{"internal/domain", "internal/port"}},
	"internal/clouddouble": {},

	"cmd/rmctl": {allow: []string{
		"cmd/rmctl/commands", "cmd/rmctl/commands/cliutil", "internal/adapter/config", "internal/wiring",
	}},
	"cmd/rmctl/commands": {allow: []string{
		"internal/domain", "internal/port", "cmd/rmctl/commands/cliutil", "cmd/rmctl/commands/account",
		"cmd/rmctl/commands/browse", "cmd/rmctl/commands/fetch", "cmd/rmctl/commands/write",
		"cmd/rmctl/commands/watch", "cmd/rmctl/commands/debug", "cmd/rmctl/commands/version",
		"cmd/rmctl/commands/rmdoc", "cmd/rmctl/commands/render",
	}, forbid: []string{transport, pageFormat, archiveFormat, renderLib}},
	"cmd/rmctl/commands/cliutil": {
		allow:  []string{"internal/domain", "internal/port", "cmd/rmctl/commands/output"},
		forbid: []string{transport, pageFormat, archiveFormat, renderLib},
	},
	// output renders answers: it knows nothing of the library, the ports or
	// the command line.
	"cmd/rmctl/commands/output":  {forbid: []string{"github.com/spf13/cobra", "os", transport, pageFormat, archiveFormat, renderLib}},
	"cmd/rmctl/commands/account": command(),
	"cmd/rmctl/commands/browse":  command(),
	"cmd/rmctl/commands/fetch":   command(),
	"cmd/rmctl/commands/write":   command(),
	"cmd/rmctl/commands/watch":   command(),
	"cmd/rmctl/commands/debug":   command(),
	"cmd/rmctl/commands/version": command(),
	"cmd/rmctl/commands/rmdoc":   command(),
	"cmd/rmctl/commands/render":  command(),
	"cmd/rmctl/commands/clitest": {allow: []string{"internal/domain", "internal/port", "cmd/rmctl/commands/cliutil", "cmd/rmctl/commands/output"}},
}

func forbidden(forbid []string, imp string) bool {
	return slices.ContainsFunc(forbid, func(f string) bool {
		return imp == f || (strings.Contains(f, ".") && strings.HasPrefix(imp, f+"/"))
	})
}

func TestLayers(t *testing.T) {
	t.Parallel()
	for name, rule := range layers {
		pkg, err := build.ImportDir(filepath.FromSlash(name), 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, imp := range pkg.Imports {
			if own, ok := strings.CutPrefix(imp, module); ok {
				if !slices.Contains(rule.allow, own) {
					t.Errorf("%s imports %s", name, own)
				}
				continue
			}
			if forbidden(rule.forbid, imp) {
				t.Errorf("%s imports %s", name, imp)
			}
		}
	}
}

// Test support never reaches production: no production package imports the
// port doubles, the emulated cloud or the command doubles.
func TestTestSupportStaysInTests(t *testing.T) {
	t.Parallel()
	support := []string{"internal/porttest", "internal/clouddouble", "cmd/rmctl/commands/clitest"}
	for name := range layers {
		if slices.Contains(support, name) {
			continue
		}
		pkg, err := build.ImportDir(filepath.FromSlash(name), 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range pkg.Imports {
			if own, ok := strings.CutPrefix(imp, module); ok && slices.Contains(support, own) {
				t.Errorf("%s imports test support %s", name, own)
			}
		}
	}
}

func TestEveryPackageHasALayer(t *testing.T) {
	t.Parallel()
	var dirs []string
	for _, pattern := range []string{"internal/*", "internal/app/*", "internal/adapter/*", "internal/adapter/*/*", "cmd/rmctl", "cmd/rmctl/commands", "cmd/rmctl/commands/*"} {
		found, err := filepath.Glob(filepath.FromSlash(pattern))
		if err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, found...)
	}
	for _, dir := range dirs {
		name := filepath.ToSlash(dir)
		if name == "internal/app" || name == "internal/adapter" || strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "testdata") {
			continue
		}
		if _, ok := layers[name]; !ok {
			t.Errorf("%s has no rule in layers", name)
		}
	}
}
