// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

func profileList() []port.ProfileSummary {
	return []port.ProfileSummary{
		{Name: "default", Current: true, Registered: true, Backend: port.SecretsFile, CacheBytes: 2048},
		{Name: "travel", Backend: port.SecretsKeychain, TokenErr: errors.New("the Keychain is locked")},
		{Name: "work", Registered: true, Backend: port.SecretsKeychain},
	}
}

func TestProfileList(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{ProfileList: profileList()}
	r := run(t, f, "profile", "ls")
	if r.Code != 0 || !strings.Contains(r.Err, "profile travel: its tokens cannot be read: the Keychain is locked") {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Err)
	}
	clitest.Golden(t, "profile-ls", r.Out)
	clitest.Golden(t, "profile-ls-json", run(t, f, "profile", "ls", "--json").Out)
	r = clitest.Exec(t, f.Services(), clitest.Options{OutTerminal: true}, Commands, "profile", "ls", "--color", "never")
	clitest.Golden(t, "profile-ls-table", r.Out)
	// It reads the cache only, so it runs offline.
	f = &clitest.Fake{Offline: true}
	if r := run(t, f, "profile", "ls", "--offline"); r.Code != 0 || r.Out != "" {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Out)
	}
}

func TestProfileAdd(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{}
	r := run(t, f, "profile", "add", "work", "abcdefgh")
	if r.Code != 0 || r.Out != "registered profile work\n" || !slices.Equal(f.Calls(), []string{"ProfileAdd work abcdefgh"}) {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Out, f.Calls())
	}
	if r = run(t, f, "profile", "add", "work", "abcdefgh", "--json"); r.Out != "{\n  \"profile\": \"work\",\n  \"registered\": true\n}\n" {
		t.Fatalf("%q", r.Out)
	}
	f = &clitest.Fake{ProfilesErr: errors.Join(errors.New("work"), port.ErrProfileRegistered)}
	r = run(t, f, "profile", "add", "work", "abcdefgh")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "rmctl -P work register --replace <code>") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	f = &clitest.Fake{ProfilesErr: port.ErrNotRegistered}
	if r = run(t, f, "profile", "add", "work", "abcdefgh"); r.Code != cliutil.CodeNotRegistered || !strings.HasPrefix(r.Error.Error(), "profile add: ") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	// Registering needs the cloud.
	f = &clitest.Fake{Offline: true}
	if r = run(t, f, "profile", "add", "work", "abcdefgh", "--offline"); r.Code != cliutil.CodeUsage || len(f.Calls()) != 0 {
		t.Fatalf("%d %v %v", r.Code, r.Error, f.Calls())
	}
}

func profileInfo() port.ProfileInfo {
	return port.ProfileInfo{
		Name: "work", ConfigDir: "/cfg/rmctl/work", DirExists: true, Backend: port.SecretsFile, BackendSource: "config",
		ConfigFile: "/cfg/rmctl/config.toml", Table: true, CacheTTL: 5 * time.Minute,
		Exists: true, TokenLocation: "/cfg/rmctl/work/token.json", Tokens: []string{port.DeviceToken, "userToken"}, Registered: true,
		Account: port.AccountInfo{Registered: true, Plan: port.Plan{Level: "connect", SyncScope: "sync:fox"}},
		Cache: port.ProfileCache{
			Profile: "work", Dir: "/cache/rmctl/work", Bytes: 4096, Budget: 1 << 29, Entries: map[string]int{"blobs": 2, "indexes": 3},
			Confirmed: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		},
	}
}

func TestProfileInfo(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{ProfileInfo: profileInfo()}
	r := run(t, f, "profile", "info", "work")
	if r.Code != 0 || r.Err != "" || !slices.Equal(f.Calls(), []string{"ProfileInfo work account"}) {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Err, f.Calls())
	}
	clitest.Golden(t, "profile-info", r.Out)
	clitest.Golden(t, "profile-info-json", run(t, f, "profile", "info", "work", "--json").Out)
	// The profile in use when none is named.
	f = &clitest.Fake{ProfileInfo: port.ProfileInfo{ProfileSettings: port.ProfileSettings{Backend: port.SecretsFile}, Current: true}}
	r = run(t, f, "profile", "info")
	if r.Code != 0 || f.Calls()[0] != "ProfileInfo  account" || !strings.Contains(r.Err, "keeps nothing of profile") {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Err, f.Calls())
	}
	clitest.Golden(t, "profile-info-unknown-json", run(t, f, "profile", "info", "--json").Out)
}

func TestProfileInfoNotes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*port.ProfileInfo)
		account string
		note    string
	}{
		{"offline", func(i *port.ProfileInfo) { i.AccountErr = port.ErrOffline }, "account\toffline\n", "--offline: the account was not asked"},
		{"failed", func(i *port.ProfileInfo) { i.AccountErr = errors.New("no route") }, "account\tfailed\n", "the cloud did not answer (no route)"},
		{"tokens", func(i *port.ProfileInfo) {
			i.TokenErr, i.Registered, i.Tokens = errors.New("locked"), false, nil
		}, "account\t-\n", "its tokens cannot be read: locked"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info := profileInfo()
			tc.mutate(&info)
			r := run(t, &clitest.Fake{ProfileInfo: info}, "profile", "info", "work")
			if r.Code != 0 || !strings.Contains(r.Out, tc.account) || !strings.Contains(r.Err, tc.note) {
				t.Fatalf("%d %v\n%s\n%s", r.Code, r.Error, r.Out, r.Err)
			}
		})
	}
}

func TestProfileDel(t *testing.T) {
	t.Parallel()
	removal := port.ProfileRemoval{
		Name: "work",
		Tokens: []port.RemovedTokens{
			{Backend: port.SecretsFile, Names: []string{port.DeviceToken, "userToken"}},
			{Backend: port.SecretsKeychain, Names: []string{"deviceID"}},
		},
		Dir:   port.RemovedDir{Path: "/cfg/rmctl/work", Existed: true},
		Cache: port.RemovedDir{Path: "/cache/rmctl/work", Existed: true, Kept: true},
		Table: true, ConfigFile: "/cfg/rmctl/config.toml",
	}
	f := &clitest.Fake{ProfileInfo: port.ProfileInfo{Exists: true}, Removal: removal}
	r := run(t, f, "profile", "del", "work", "--yes")
	if r.Code != 0 || !slices.Equal(f.Calls(), []string{"ProfileInfo work", "ProfileRemove work"}) ||
		!strings.Contains(r.Err, "/cache/rmctl/work holds files rmctl did not write") {
		t.Fatalf("%d %v %v %q", r.Code, r.Error, f.Calls(), r.Err)
	}
	clitest.Golden(t, "profile-del", r.Out)
	clitest.Golden(t, "profile-del-json", run(t, f, "profile", "del", "work", "--yes", "--json").Out)

	// The question names the profile, and says when it is the one in use.
	f = &clitest.Fake{ProfileInfo: port.ProfileInfo{Exists: true, Current: true}, Removal: removal}
	r = clitest.Exec(t, f.Services(), clitest.Options{Terminal: true, Stdin: "y\n"}, Commands, "profile", "del", "work", "--revoke")
	if r.Code != 0 || !strings.Contains(r.Err, "Remove the device of profile work from the account, then delete — the profile this command runs as — with") ||
		f.Calls()[1] != "ProfileRemove work revoke" {
		t.Fatalf("%d %v %q %v", r.Code, r.Error, r.Err, f.Calls())
	}
	r = clitest.Exec(t, f.Services(), clitest.Options{Terminal: true, Stdin: "n\n"}, Commands, "profile", "del", "work")
	if !errors.Is(r.Error, cliutil.ErrDeclined) || !strings.Contains(r.Err, "Delete profile work — the profile this command runs as — with") {
		t.Fatalf("%v %q", r.Error, r.Err)
	}
	// Without a terminal, --yes is required.
	f = &clitest.Fake{ProfileInfo: port.ProfileInfo{Exists: true}}
	if r = run(t, f, "profile", "del", "work"); r.Code != cliutil.CodeUsage || slices.Contains(f.Calls(), "ProfileRemove work") {
		t.Fatalf("%d %v", r.Code, f.Calls())
	}
}

func TestProfileDelRefuses(t *testing.T) {
	t.Parallel()
	// A profile this machine keeps nothing of, before any question.
	f := &clitest.Fake{}
	r := run(t, f, "profile", "del", "nope", "--yes")
	if !errors.Is(r.Error, port.ErrNoProfile) || slices.Contains(f.Calls(), "ProfileRemove nope") {
		t.Fatalf("%v %v", r.Error, f.Calls())
	}
	// --revoke needs the cloud; del alone runs offline.
	f = &clitest.Fake{ProfileInfo: port.ProfileInfo{Exists: true}, Offline: true, Removal: port.ProfileRemoval{Name: "work"}}
	if r = run(t, f, "profile", "del", "work", "--revoke", "--yes", "--offline"); r.Code != cliutil.CodeUsage || !strings.Contains(r.Error.Error(), "--revoke needs the cloud") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = run(t, f, "profile", "del", "work", "--yes", "--offline"); r.Code != 0 {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
	if r = run(t, &clitest.Fake{Err: errors.New("boom")}, "profile", "del", "work", "--yes"); r.Code != cliutil.CodeError || !strings.HasPrefix(r.Error.Error(), "profile del: ") {
		t.Fatalf("%d %v", r.Code, r.Error)
	}
}

// A removal that stops part way prints what it did, then fails.
func TestProfileDelPartial(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{
		ProfileInfo: port.ProfileInfo{Exists: true},
		Removal: port.ProfileRemoval{
			Name: "work", Tokens: []port.RemovedTokens{{Backend: port.SecretsFile, Names: []string{port.DeviceToken}}},
			Unreachable: []port.SecretsBackend{port.SecretsKeychain}, Dir: port.RemovedDir{Path: "/cfg/rmctl/work", Existed: true, Kept: true},
		},
		RemoveErr: errors.New("remove the profile's cache: busy"),
	}
	r := run(t, f, "profile", "del", "work", "--yes")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Out, "file_tokens\tdeviceToken\n") || !strings.Contains(r.Out, "cache_removed\tfalse\n") ||
		!strings.Contains(r.Error.Error(), "profile del: remove the profile's cache: busy") ||
		!strings.Contains(r.Err, "cannot reach the keychain") || !strings.Contains(r.Err, "/cfg/rmctl/work holds files") {
		t.Fatalf("%d %v\n%s\n%s", r.Code, r.Error, r.Out, r.Err)
	}
	// A removal refused before its first step prints nothing.
	f = &clitest.Fake{ProfileInfo: port.ProfileInfo{Exists: true}, RemoveErr: port.ErrNoProfile}
	if r = run(t, f, "profile", "del", "work", "--yes"); r.Out != "" || !errors.Is(r.Error, port.ErrNoProfile) {
		t.Fatalf("%q %v", r.Out, r.Error)
	}
}

func TestProfileUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"profile", "ls", "x"}, {"profile", "add", "work"}, {"profile", "add", "a", "b", "c"},
		{"profile", "info", "a", "b"}, {"profile", "del"}, {"profile", "del", "a", "b"}, {"profile", "x"},
	} {
		if r := run(t, &clitest.Fake{}, args...); r.Code != cliutil.CodeUsage {
			t.Errorf("%v: %d %v", args, r.Code, r.Error)
		}
	}
	r := run(t, &clitest.Fake{}, "profile")
	if r.Code != 0 || !strings.Contains(r.Out, "rmctl profile del work") {
		t.Fatalf("%d %q", r.Code, r.Out)
	}
	for _, args := range [][]string{{"profile", "ls"}, {"profile", "info"}} {
		if r := run(t, &clitest.Fake{Err: errors.New("boom")}, args...); r.Code != cliutil.CodeError {
			t.Errorf("%v: %d %v", args, r.Code, r.Error)
		}
	}
}
