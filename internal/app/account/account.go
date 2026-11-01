// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package account registers this machine with the cloud and reports on the
// account. It implements port.Account on port.Registry.
package account

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/altessa-s/rmctl/internal/port"
)

// ErrAlreadyRegistered means a device token is stored and replace was not asked.
var ErrAlreadyRegistered = errors.New("already registered: run rmctl logout first, or register --replace")

// ErrEmptyCode means the one-time code is empty.
var ErrEmptyCode = errors.New("the one-time code is empty")

// Service implements port.Account.
type Service struct {
	registry port.Registry
}

var _ port.Account = (*Service)(nil)

// New returns the account use case. A nil registry panics.
func New(registry port.Registry) *Service {
	if registry == nil {
		panic("account: Registry is required")
	}
	return &Service{registry: registry}
}

// Register implements port.Account.
func (s *Service) Register(ctx context.Context, code string, replace bool) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrEmptyCode
	}
	registered, err := s.registry.IsRegistered(ctx)
	if err != nil {
		return err
	}
	if registered {
		if !replace {
			return ErrAlreadyRegistered
		}
		if err := s.registry.Forget(ctx); err != nil {
			return fmt.Errorf("drop the stored token: %w", err)
		}
	}
	return s.registry.Register(ctx, code)
}

// Logout implements port.Account. Logging out of a machine that holds no
// token is not an error.
func (s *Service) Logout(ctx context.Context, revoke bool) error {
	registered, err := s.registry.IsRegistered(ctx)
	if err != nil {
		return err
	}
	if !registered {
		return nil
	}
	if revoke {
		if err := s.registry.Revoke(ctx); err != nil {
			return fmt.Errorf("revoke the device: %w", err)
		}
	}
	return s.registry.Forget(ctx)
}

// Info implements port.Account.
func (s *Service) Info(ctx context.Context) (port.AccountInfo, error) {
	registered, err := s.registry.IsRegistered(ctx)
	if err != nil || !registered {
		return port.AccountInfo{}, err
	}
	plan, err := s.registry.Plan(ctx)
	if err != nil {
		return port.AccountInfo{}, err
	}
	return port.AccountInfo{Registered: true, Plan: plan}, nil
}
