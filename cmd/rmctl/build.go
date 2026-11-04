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

// seams are the parts of the machine a test replaces: keychain, when set,
// stands in for the login Keychain.
type seams struct {
	keychain wiring.KeychainItems
}

// build is the composition root's entry: the services req asks for, with a
// logger on errOut at Debug when verbose and Warn otherwise. extra are
// transport options a test uses to reach an emulated cloud.
func build(_ context.Context, req commands.Request, errOut io.Writer, s seams, extra ...rmcloud.Option) (commands.Services, func() error, error) {
	logger, err := newLogger(errOut, req.Verbose)
	if err != nil {
		return commands.Services{}, nil, err
	}
	dirs, err := config.SystemDirs()
	if err != nil {
		return commands.Services{}, nil, err
	}
	svc, err := wiring.Build(wiring.Options{
		Env:      config.Environ(),
		Profile:  req.Profile,
		Dirs:     dirs,
		Logger:   logger,
		Client:   extra,
		Offline:  req.Offline,
		NoCache:  req.NoCache,
		ReadOnly: req.ReadOnly,
		Keychain: s.keychain,
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
		Profiles:  svc.Profiles,
		Pages:     svc.Pages,
		Archives:  svc.Archives,
		Rendering: svc.Rendering,
		Cache:     svc.Cache,
		Offline:   svc.Offline,
	}, svc.Close, nil
}
