// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package profile_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/account"
	"github.com/altessa-s/rmctl/internal/app/profile"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func registered() *porttest.Vault {
	return porttest.NewVault(port.SecretsFile, map[string]string{port.DeviceToken: "d", "userToken": "u"})
}

func newService(p *porttest.Profiles, current string, offline bool) *profile.Service {
	return profile.New(p, p.Caches(), p, current, offline)
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(nil)
	for name, build := range map[string]func(){
		"store":   func() { profile.New(nil, p.Caches(), p, "default", false) },
		"current": func() { profile.New(p, p.Caches(), p, "", false) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			build()
		}()
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{
		"work":    {Dir: true, File: registered(), Cache: true, CacheBytes: 42},
		"travel":  {Table: true, Settings: port.ProfileSettings{Backend: port.SecretsKeychain}},
		"old":     {Cache: true},
		"default": {File: registered()},
	})
	got, err := newService(p, "default", false).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []port.ProfileSummary{
		{Name: "default", Current: true, Registered: true, Backend: port.SecretsFile},
		{Name: "old", Backend: port.SecretsFile},
		{Name: "travel", Backend: port.SecretsKeychain},
		{Name: "work", Registered: true, Backend: port.SecretsFile, CacheBytes: 42},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// The profile in use is listed only once it holds a token: an unused
// default is not a profile of this machine.
func TestListLeavesOutAnUnregisteredCurrentProfile(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true}})
	got, err := newService(p, "default", false).List(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "work" || got[0].Current {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestListReportsAnUnreadableTokenStore(t *testing.T) {
	t.Parallel()
	locked := porttest.NewVault(port.SecretsKeychain, nil)
	locked.Fail["Get"] = errBoom
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{
		"work": {Table: true, Keychain: locked, Settings: port.ProfileSettings{Backend: port.SecretsKeychain}},
	})
	got, err := newService(p, "default", false).List(context.Background())
	if err != nil || len(got) != 1 || !errors.Is(got[0].TokenErr, errBoom) || got[0].Registered {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestListFailures(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"Names", "Settings", "Vaults", "Status"} {
		p := porttest.NewProfiles(map[string]*porttest.ProfileState{"default": {Dir: true}})
		p.Fail[method] = errBoom
		if _, err := newService(p, "default", false).List(context.Background()); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	// The profile in use, unknown otherwise, is looked at too.
	p := porttest.NewProfiles(nil)
	p.Fail["Settings"] = errBoom
	if _, err := newService(p, "default", false).List(context.Background()); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{
		"work": {Dir: true, Table: true, File: registered(), Cache: true, CacheBytes: 7},
	})
	p.Cloud("work").Registered = true
	p.Cloud("work").AccountPlan = port.Plan{Level: "connect", SyncScope: "sync:fox"}
	svc := newService(p, "default", false)

	info, err := svc.Info(context.Background(), "work", true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "work" || info.Current || !info.Exists || !info.Registered || !info.DirExists || !info.Table ||
		info.Account.Plan.Level != "connect" || info.AccountErr != nil || info.Cache.Bytes != 7 ||
		!slices.Equal(info.Tokens, []string{port.DeviceToken, "userToken"}) || info.TokenLocation != "in the file" {
		t.Fatalf("%+v", info)
	}
	if p.Closed != 1 {
		t.Fatalf("the registry was not released: %d", p.Closed)
	}
	// Without the account, the cloud is not asked.
	before := len(p.Cloud("work").Calls())
	if info, err = svc.Info(context.Background(), "work", false); err != nil || info.Account.Registered || len(p.Cloud("work").Calls()) != before {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestInfoOfTheProfileInUse(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(nil)
	info, err := newService(p, "default", false).Info(context.Background(), "", true)
	if err != nil || info.Name != "default" || !info.Current || info.Exists || info.Registered || info.AccountErr != nil {
		t.Fatalf("%+v %v", info, err)
	}
	if slices.Contains(p.Calls(), "Open default") {
		t.Fatal("the cloud was asked about a profile that is not registered")
	}
}

func TestInfoOfflineAndFailures(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {File: registered()}})
	info, err := newService(p, "default", true).Info(context.Background(), "work", true)
	if err != nil || !errors.Is(info.AccountErr, port.ErrOffline) || !info.Exists {
		t.Fatalf("%+v %v", info, err)
	}
	if slices.Contains(p.Calls(), "Open work") {
		t.Fatal("the cloud was asked offline")
	}
	// A cloud that does not answer leaves what is known locally.
	p.Cloud("work").Registered = true
	p.Cloud("work").Fail = map[string]error{"Plan": errBoom}
	if info, err = newService(p, "default", false).Info(context.Background(), "work", true); err != nil || !errors.Is(info.AccountErr, errBoom) {
		t.Fatalf("%+v %v", info, err)
	}
	p.Fail["Open"] = errBoom
	if info, err = newService(p, "default", false).Info(context.Background(), "work", true); err != nil || !errors.Is(info.AccountErr, errBoom) {
		t.Fatalf("%+v %v", info, err)
	}
	for _, method := range []string{"Settings", "Vaults", "Names", "Status"} {
		p := porttest.NewProfiles(nil)
		p.Fail[method] = errBoom
		if _, err := newService(p, "default", false).Info(context.Background(), "work", false); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", method, err)
		}
	}
	if _, err := newService(porttest.NewProfiles(nil), "default", false).Info(context.Background(), "Bad Name", false); !errors.Is(err, porttest.ErrBadName) {
		t.Fatal(err)
	}
}

// A profile kept only by a token in its other place — the Keychain after a
// move by hand, say — still exists.
func TestExistsByAToken(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{
		"work": {Keychain: porttest.NewVault(port.SecretsKeychain, map[string]string{"userToken": "u"})},
	})
	info, err := newService(p, "default", false).Info(context.Background(), "work", false)
	if err != nil || !info.Exists || info.Registered {
		t.Fatalf("%+v %v", info, err)
	}
	// A place this build cannot reach holds nothing of it; one that fails
	// otherwise fails the question.
	unsupported := porttest.NewVault(port.SecretsKeychain, nil)
	unsupported.Fail["Get"] = port.ErrKeychainUnsupported
	p = porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Keychain: unsupported}})
	if info, err = newService(p, "default", false).Info(context.Background(), "work", false); err != nil || info.Exists {
		t.Fatalf("%+v %v", info, err)
	}
	unsupported.Fail["Get"] = errBoom
	if _, err = newService(p, "default", false).Info(context.Background(), "work", false); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestAdd(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(nil)
	svc := newService(p, "default", false)
	if err := svc.Add(context.Background(), "work", "abcdefgh"); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.State("work").File.Values()[port.DeviceToken]; !ok || p.Closed != 1 {
		t.Fatalf("not registered: %v %d", p.State("work").File.Values(), p.Closed)
	}
	if err := svc.Add(context.Background(), "work", "abcdefgh"); !errors.Is(err, port.ErrProfileRegistered) {
		t.Fatal(err)
	}
	if err := svc.Add(context.Background(), "Bad Name", "abcdefgh"); !errors.Is(err, porttest.ErrBadName) {
		t.Fatal(err)
	}
	if err := svc.Add(context.Background(), "home", "wrong"); !errors.Is(err, porttest.ErrInvalidCode) {
		t.Fatal(err)
	}
	if err := svc.Add(context.Background(), "home", " "); !errors.Is(err, account.ErrEmptyCode) {
		t.Fatal(err)
	}
	if err := newService(p, "default", true).Add(context.Background(), "home", "abcdefgh"); !errors.Is(err, port.ErrOffline) {
		t.Fatal(err)
	}
	p.Fail["Open"] = errBoom
	if err := svc.Add(context.Background(), "home", "abcdefgh"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	locked := porttest.NewVault(port.SecretsFile, nil)
	locked.Fail["Get"] = errBoom
	p = porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {File: locked}})
	if err := newService(p, "default", false).Add(context.Background(), "work", "abcdefgh"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	p = porttest.NewProfiles(nil)
	p.Fail["Settings"] = errBoom
	if err := newService(p, "default", false).Add(context.Background(), "work", "abcdefgh"); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{
		"work": {
			Dir: true, Table: true, Cache: true, File: registered(),
			Keychain: porttest.NewVault(port.SecretsKeychain, map[string]string{"deviceID": "i"}),
		},
	})
	got, err := newService(p, "work", false).Remove(context.Background(), "work", port.RemoveProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "work" || got.Revoked || !got.Table || got.ConfigFile != "/config/config.toml" ||
		!got.Dir.Existed || got.Dir.Kept || !got.Cache.Existed || len(got.Unreachable) != 0 || len(got.Tokens) != 2 ||
		!slices.Equal(got.Tokens[0].Names, []string{port.DeviceToken, "userToken"}) || got.Tokens[1].Backend != port.SecretsKeychain {
		t.Fatalf("%+v", got)
	}
	st := p.State("work")
	if len(st.File.Values()) != 0 || len(st.Keychain.Values()) != 0 || st.Dir || st.Table || st.Cache {
		t.Fatalf("left %+v", st)
	}
	// Tokens first, the tables last.
	var order []string
	for _, c := range p.Calls() {
		switch c {
		case "RemoveDir work", "RemoveCache work", "RemoveTable work":
			order = append(order, c)
		}
	}
	if !slices.Equal(order, []string{"RemoveDir work", "RemoveCache work", "RemoveTable work"}) {
		t.Fatalf("%v", order)
	}
	if slices.Contains(p.Calls(), "Open work") {
		t.Fatal("the cloud was told without --revoke")
	}
	// Gone now.
	if _, err = newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{}); !errors.Is(err, port.ErrNoProfile) {
		t.Fatal(err)
	}
}

func TestRemoveKeepsForeignFiles(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true, KeptDir: true, Cache: true, KeptCache: true}})
	got, err := newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{})
	if err != nil || !got.Dir.Kept || !got.Cache.Kept {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRemoveRevokes(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true, File: registered()}})
	p.Cloud("work").Registered = true
	got, err := newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{Revoke: true})
	if err != nil || !got.Revoked || p.Cloud("work").Count("Revoke") != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	// A profile that is not registered has no device to revoke.
	p = porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true}})
	if got, err = newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{Revoke: true}); err != nil || got.Revoked {
		t.Fatalf("%+v %v", got, err)
	}
}

// A revoke that fails, or cannot be made, changes nothing.
func TestRemoveRevokeFailureChangesNothing(t *testing.T) {
	t.Parallel()
	for name, setup := range map[string]func(*porttest.Profiles) *profile.Service{
		"refused": func(p *porttest.Profiles) *profile.Service {
			p.Cloud("work").Registered = true
			p.Cloud("work").Fail = map[string]error{"Revoke": errBoom}
			return newService(p, "default", false)
		},
		"offline": func(p *porttest.Profiles) *profile.Service { return newService(p, "default", true) },
		"unreadable": func(p *porttest.Profiles) *profile.Service {
			p.State("work").File.Fail["Get"] = errBoom
			p.State("work").Table = true
			return newService(p, "default", false)
		},
	} {
		p := porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true, File: registered()}})
		svc := setup(p)
		got, err := svc.Remove(context.Background(), "work", port.RemoveProfileOptions{Revoke: true})
		if err == nil || got.Revoked || !p.State("work").Dir {
			t.Errorf("%s: %+v %v", name, got, err)
		}
		p.State("work").File.Fail["Get"] = nil
		if _, ok := p.State("work").File.Values()[port.DeviceToken]; !ok {
			t.Errorf("%s: the token went", name)
		}
	}
}

// Each step that fails stops the removal there: what came before is
// reported, nothing after it is done, and the profile is still there to
// remove again.
func TestRemoveStopsAtTheFailingStep(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"RemoveDir", "RemoveCache", "RemoveTable"} {
		p := porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true, Table: true, Cache: true, File: registered()}})
		p.Fail[method] = errBoom
		got, err := newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{})
		if !errors.Is(err, errBoom) || len(got.Tokens) != 1 || !p.State("work").Table {
			t.Errorf("%s: %+v %v", method, got, err)
		}
		delete(p.Fail, method)
		if _, err := newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{}); err != nil {
			t.Errorf("%s: again: %v", method, err)
		}
	}
	// A token that cannot be removed stops it before the directory.
	p := porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true, File: registered()}})
	p.State("work").File.Fail["Delete"] = errBoom
	got, err := newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{})
	if !errors.Is(err, errBoom) || got.Dir.Path != "" || !p.State("work").Dir {
		t.Fatalf("%+v %v", got, err)
	}
	p = porttest.NewProfiles(map[string]*porttest.ProfileState{"work": {Dir: true}})
	p.State("work").Keychain.Fail["Get"] = errBoom
	if _, err := newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// The Keychain off macOS: a profile kept there is still removed elsewhere,
// and the place is reported; one kept in the file does not hear of it.
func TestRemoveWithoutTheKeychain(t *testing.T) {
	t.Parallel()
	for backend, want := range map[port.SecretsBackend]int{port.SecretsKeychain: 1, port.SecretsFile: 0} {
		unsupported := porttest.NewVault(port.SecretsKeychain, nil)
		unsupported.Fail["Get"] = port.ErrKeychainUnsupported
		p := porttest.NewProfiles(map[string]*porttest.ProfileState{
			"work": {Table: true, Keychain: unsupported, Settings: port.ProfileSettings{Backend: backend}},
		})
		got, err := newService(p, "default", false).Remove(context.Background(), "work", port.RemoveProfileOptions{})
		if err != nil || len(got.Unreachable) != want || !got.Table {
			t.Errorf("%s: %+v %v", backend, got, err)
		}
	}
}

func TestRemoveRefuses(t *testing.T) {
	t.Parallel()
	p := porttest.NewProfiles(nil)
	svc := newService(p, "default", false)
	if _, err := svc.Remove(context.Background(), "nope", port.RemoveProfileOptions{}); !errors.Is(err, port.ErrNoProfile) {
		t.Fatal(err)
	}
	if _, err := svc.Remove(context.Background(), "Bad Name", port.RemoveProfileOptions{}); !errors.Is(err, porttest.ErrBadName) {
		t.Fatal(err)
	}
	p.Fail["Names"] = errBoom
	if _, err := svc.Remove(context.Background(), "work", port.RemoveProfileOptions{}); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}
