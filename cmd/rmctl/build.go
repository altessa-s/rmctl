// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"io"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands"
	"github.com/altessa-s/rmctl/internal/adapter/config"
	"github.com/altessa-s/rmctl/internal/wiring"

	rmcloud "github.com/altessa-s/rmcloud-go"
)

// build is the composition root's entry: the services for profile, with a
// logger on errOut at Debug when verbose and Warn otherwise. extra are
// transport options a test uses to reach an emulated cloud.
func build(_ context.Context, profile string, verbose bool, errOut io.Writer, extra ...rmcloud.Option) (commands.Services, func() error, error) {
	logger, err := newLogger(errOut, verbose)
	if err != nil {
		return commands.Services{}, nil, err
	}
	dirs, err := config.SystemDirs()
	if err != nil {
		return commands.Services{}, nil, err
	}
	svc, err := wiring.Build(wiring.Options{
		Env:     config.Environ(),
		Profile: profile,
		Dirs:    dirs,
		Logger:  logger,
		Client:  extra,
	})
	if err != nil {
		return commands.Services{}, nil, err
	}
	return commands.Services{
		Account:   svc.Account,
		Browser:   svc.Browser,
		Fetcher:   svc.Fetcher,
		Writer:    svc.Writer,
		Watcher:   svc.Watcher,
		Inspector: svc.Inspector,
		Secrets:   svc.Secrets,
		Pages:     svc.Pages,
		Archives:  svc.Archives,
	}, svc.Close, nil
}
