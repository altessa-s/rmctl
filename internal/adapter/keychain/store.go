// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package keychain

import (
	"context"
	"fmt"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/adapter/keychain/system"
)

// DefaultService is the Keychain service rmctl's items are filed under.
const DefaultService = "io.altessa.rmctl"

// Items is the Keychain as the store needs it: generic passwords by service
// and account.
type Items interface {
	Get(service, account string) (string, bool, error)
	// Put adds the item, or updates it when it is there.
	Put(service, account, label, value string) error
	// Delete removes the item; a missing one is not an error.
	Delete(service, account string) error
}

// Store is a profile's tokens in the Keychain.
type Store struct {
	items   Items
	service string
	profile string
}

var _ rmcloud.SecretStore = (*Store)(nil)

// New is the store of profile under service in the login Keychain.
func New(service, profile string) *Store {
	return NewWith(system.Keychain{}, service, profile)
}

// NewWith is the store of profile under service in it.
func NewWith(it Items, service, profile string) *Store {
	if service == "" || profile == "" {
		panic("keychain: a service and a profile are required")
	}
	return &Store{items: it, service: service, profile: profile}
}

// Location is where the items are, for a person.
func (s *Store) Location() string {
	return fmt.Sprintf("Keychain service %s, accounts %s/…", s.service, s.profile)
}

func (s *Store) account(which rmcloud.SecretName) string { return s.profile + "/" + string(which) }

// Secret implements rmcloud.SecretStore.
func (s *Store) Secret(_ context.Context, which rmcloud.SecretName) (string, bool, error) {
	v, ok, err := s.items.Get(s.service, s.account(which))
	if err != nil {
		return "", false, fmt.Errorf("read %s from the Keychain: %w", which, err)
	}
	return v, ok, nil
}

// Save implements rmcloud.SecretStore.
func (s *Store) Save(_ context.Context, which rmcloud.SecretName, value string) error {
	label := fmt.Sprintf("rmctl %s %s", s.profile, which)
	if err := s.items.Put(s.service, s.account(which), label, value); err != nil {
		return fmt.Errorf("save %s in the Keychain: %w", which, err)
	}
	return nil
}

// Forget implements rmcloud.SecretStore.
func (s *Store) Forget(_ context.Context, which rmcloud.SecretName) error {
	if err := s.items.Delete(s.service, s.account(which)); err != nil {
		return fmt.Errorf("remove %s from the Keychain: %w", which, err)
	}
	return nil
}
