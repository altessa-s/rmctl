// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

func status() port.SecretsStatus {
	return port.SecretsStatus{
		Profile: "work", Backend: port.SecretsKeychain, Location: "Keychain service io.altessa.rmctl, accounts work/…",
		Source: "profile", ConfigFile: "/c/config.toml", Present: []string{"deviceToken", "userToken"},
	}
}

func TestSecretsStatus(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{SecretsAt: status()}
	r := run(t, f, "secrets", "status")
	want := "profile   work\nbackend   keychain (chosen by profile)\nlocation  Keychain service io.altessa.rmctl, accounts work/…\n" +
		"config    /c/config.toml\npresent   deviceToken, userToken\n"
	if r.Code != cliutil.CodeOK || r.Out != want {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Out)
	}
	j := run(t, &clitest.Fake{SecretsAt: port.SecretsStatus{Profile: "default", Backend: port.SecretsFile}}, "secrets", "status", "--json")
	clitest.Golden(t, "secrets-status", j.Out)
	if e := run(t, &clitest.Fake{SecretsErr: port.ErrKeychainUnsupported}, "secrets", "status"); e.Code != cliutil.CodeError ||
		!strings.Contains(e.Error.Error(), "needs a macOS build") {
		t.Fatalf("%d %v", e.Code, e.Error)
	}
	none := cliutil.Services{}
	if r := clitest.Exec(t, none, clitest.Options{}, Commands, "secrets", "status"); r.Code == cliutil.CodeOK {
		t.Fatal("status ran without the service")
	}
}

func TestSecretsMigrate(t *testing.T) {
	t.Parallel()
	moved := port.SecretsMigration{From: port.SecretsFile, To: port.SecretsKeychain, Location: "Keychain", Moved: []string{"deviceToken"},
		ConfigFile: "/c/config.toml", ConfigLines: "[profiles.default.secrets]\nbackend = \"keychain\"\n"}
	// A change to where the tokens are needs --yes off a terminal.
	f := &clitest.Fake{Migration: moved}
	if r := run(t, f, "secrets", "migrate", "--to", "keychain"); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
		t.Fatalf("%d %v", r.Code, f.Calls())
	}
	r := run(t, f, "secrets", "migrate", "--to", "keychain", "--replace", "--yes")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"Migrate keychain replace"}) ||
		r.Out != "moved deviceToken from the file to Keychain\n" || !strings.Contains(r.Err, "backend = \"keychain\"") {
		t.Fatalf("%d %v %v %q %q", r.Code, r.Error, f.Calls(), r.Out, r.Err)
	}
	set := moved
	set.DefaultSet, set.ConfigLines = true, ""
	f = &clitest.Fake{Migration: set}
	r = run(t, f, "secrets", "migrate", "--to", "keychain", "--set-default", "--yes")
	if r.Code != cliutil.CodeOK || !slices.Equal(f.Calls(), []string{"Migrate keychain  set-default"}) ||
		!strings.Contains(r.Out, "/c/config.toml now chooses the keychain") || r.Err != "" {
		t.Fatalf("%d %v %v %q %q", r.Code, r.Error, f.Calls(), r.Out, r.Err)
	}
	overridden := moved
	overridden.Override = "RMCTL_SECRETS=file"
	r = run(t, &clitest.Fake{Migration: overridden}, "secrets", "migrate", "--to", "keychain", "--yes")
	if r.Code != cliutil.CodeOK || !strings.Contains(r.Err, "RMCTL_SECRETS=file in the environment overrides /c/config.toml") ||
		!strings.Contains(r.Err, "set RMCTL_SECRETS=keychain") {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
	j := run(t, &clitest.Fake{Migration: moved}, "secrets", "migrate", "--to", "keychain", "--yes", "--json")
	clitest.Golden(t, "secrets-migrate", j.Out)
	for _, args := range [][]string{{"secrets", "migrate"}, {"secrets", "migrate", "--to", "vault", "--yes"}} {
		if r := run(t, &clitest.Fake{}, args...); r.Code != cliutil.CodeUsage {
			t.Errorf("%v: %d", args, r.Code)
		}
	}
	failing := &clitest.Fake{SecretsErr: port.ErrTargetOccupied}
	if r := run(t, failing, "secrets", "migrate", "--to", "file", "--yes"); r.Code != cliutil.CodeError || !errors.Is(r.Error, port.ErrTargetOccupied) {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r := clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "secrets", "migrate", "--to", "file", "--yes"); r.Code == cliutil.CodeOK {
		t.Fatal("migrate ran without the service")
	}
}
