// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package watch follows the notification socket. It implements
// port.Watcher on port.Notifier.
package watch

import (
	"context"
	"errors"

	"github.com/altessa-s/rmctl/internal/port"
)

// Service implements port.Watcher.
type Service struct {
	notifier port.Notifier
}

var _ port.Watcher = (*Service)(nil)

// New returns the watch use case. A nil notifier panics.
func New(notifier port.Notifier) *Service {
	if notifier == nil {
		panic("watch: Notifier is required")
	}
	return &Service{notifier: notifier}
}

// Watch implements port.Watcher: it delivers events until ctx ends, which is
// how a watch is meant to stop, so that end is not an error.
func (s *Service) Watch(ctx context.Context, on func(port.Event)) error {
	if on == nil {
		on = func(port.Event) {}
	}
	err := s.notifier.Listen(ctx, on)
	if ctx.Err() != nil && (err == nil || errors.Is(err, ctx.Err())) {
		return nil
	}
	return err
}
