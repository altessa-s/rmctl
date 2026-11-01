// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package keychain_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/adapter/keychain"
)

// memory is a Keychain in a map, with a failure to inject per operation.
type memory struct {
	mu     sync.Mutex
	items  map[string]string
	labels map[string]string
	fail   map[string]error
}

func newMemory() *memory {
	return &memory{items: map[string]string{}, labels: map[string]string{}, fail: map[string]error{}}
}

func (m *memory) Get(service, account string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail["get"]; err != nil {
		return "", false, err
	}
	v, ok := m.items[service+"|"+account]
	return v, ok, nil
}

func (m *memory) Put(service, account, label, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail["put"]; err != nil {
		return err
	}
	m.items[service+"|"+account] = value
	m.labels[service+"|"+account] = label
	return nil
}

func (m *memory) Delete(service, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail["delete"]; err != nil {
		return err
	}
	delete(m.items, service+"|"+account)
	return nil
}

func TestStore(t *testing.T) {
	t.Parallel()
	mem := newMemory()
	s := keychain.NewWith(mem, "svc", "work")
	ctx := t.Context()
	if _, ok, err := s.Secret(ctx, rmcloud.SecretDeviceToken); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if err := s.Save(ctx, rmcloud.SecretDeviceToken, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, rmcloud.SecretDeviceToken, "v2"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Secret(ctx, rmcloud.SecretDeviceToken); v != "v2" || !ok || err != nil {
		t.Fatalf("%q %v %v", v, ok, err)
	}
	// Each profile's items are its own, filed under the service.
	if mem.labels["svc|work/deviceToken"] != "rmctl work deviceToken" {
		t.Fatalf("%v", mem.labels)
	}
	other := keychain.NewWith(mem, "svc", "home")
	if _, ok, _ := other.Secret(ctx, rmcloud.SecretDeviceToken); ok {
		t.Fatal("a profile read another's token")
	}
	if err := s.Forget(ctx, rmcloud.SecretDeviceToken); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(ctx, rmcloud.SecretDeviceToken); err != nil {
		t.Fatal("forgetting a missing token failed:", err)
	}
	if !strings.Contains(s.Location(), "svc") || !strings.Contains(s.Location(), "work/") {
		t.Fatal(s.Location())
	}
}

// A failure names the operation and the secret, and never the value.
func TestStoreErrorsCarryNoValue(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	for _, op := range []string{"get", "put", "delete"} {
		mem := newMemory()
		mem.fail[op] = errors.New("errSecInteractionNotAllowed")
		s := keychain.NewWith(mem, "svc", "work")
		var err error
		switch op {
		case "get":
			_, _, err = s.Secret(ctx, rmcloud.SecretUserToken)
		case "put":
			err = s.Save(ctx, rmcloud.SecretUserToken, "very-secret-value")
		case "delete":
			err = s.Forget(ctx, rmcloud.SecretUserToken)
		}
		if err == nil || !strings.Contains(err.Error(), "userToken") || strings.Contains(err.Error(), "very-secret-value") {
			t.Errorf("%s: %v", op, err)
		}
	}
}

func TestStoreNeedsAServiceAndAProfile(t *testing.T) {
	t.Parallel()
	for _, args := range [][2]string{{"", "work"}, {"svc", ""}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%v: no panic", args)
				}
			}()
			keychain.NewWith(newMemory(), args[0], args[1])
		}()
	}
	if keychain.New(keychain.DefaultService, "work") == nil {
		t.Fatal("New")
	}
}
