// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/altessa-s/rmctl/internal/adapter/blobcache"
	"github.com/altessa-s/rmctl/internal/adapter/clock"
	"github.com/altessa-s/rmctl/internal/adapter/cloud"
	"github.com/altessa-s/rmctl/internal/adapter/config"
	"github.com/altessa-s/rmctl/internal/adapter/ids"
	"github.com/altessa-s/rmctl/internal/adapter/keychain"
	"github.com/altessa-s/rmctl/internal/adapter/localfs"
	"github.com/altessa-s/rmctl/internal/adapter/pdf"
	"github.com/altessa-s/rmctl/internal/adapter/rmdocs"
	"github.com/altessa-s/rmctl/internal/adapter/rmpages"
	"github.com/altessa-s/rmctl/internal/adapter/secretfile"
	"github.com/altessa-s/rmctl/internal/adapter/vault"
	"github.com/altessa-s/rmctl/internal/app/account"
	"github.com/altessa-s/rmctl/internal/app/archives"
	"github.com/altessa-s/rmctl/internal/app/browse"
	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/inspect"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/app/secrets"
	"github.com/altessa-s/rmctl/internal/app/watch"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/port"

	rmcloud "github.com/altessa-s/rmcloud-go"
)

// DefaultCacheBytes bounds the blob cache on disk.
const DefaultCacheBytes int64 = 512 << 20

// Options are what the command line knows before anything is built.
type Options struct {
	// Env is the environment, as config.Environ returns it.
	Env map[string]string
	// Profile is the --profile flag; empty defers to the environment.
	Profile string
	// Dirs are the per-user base directories, as config.SystemDirs returns them.
	Dirs config.BaseDirs
	// Logger receives the diagnostics of every layer. Required.
	Logger *slog.Logger
	// CacheBytes bounds the blob cache; zero is DefaultCacheBytes, negative
	// turns the cache off.
	CacheBytes int64
	// Client are extra transport options, after the ones the configuration
	// gives: a test points the transport at an emulated cloud with them.
	Client []rmcloud.Option
	// ReadConfig reads the configuration file; nil reads the machine's.
	ReadConfig config.ReadFunc
}

// Services are the input ports, built and connected.
type Services struct {
	Account   port.Account
	Browser   port.Browser
	Fetcher   port.Fetcher
	Writer    port.Writer
	Watcher   port.Watcher
	Inspector port.Inspector
	Pages     port.Pages
	Archives  port.Archives
	Secrets   port.Secrets
	// Config is the configuration the services were built from.
	Config config.Config
	// Close releases the transport. It is safe to call more than once.
	Close func() error
}

// ErrNoLogger means Options.Logger is nil.
var ErrNoLogger = errors.New("wiring: a logger is required")

// Build resolves the configuration and connects the adapters to the use
// cases. Nothing touches the network here: the transport dials on first use.
func Build(opts Options) (Services, error) {
	if opts.Logger == nil {
		return Services{}, ErrNoLogger
	}
	read := opts.ReadConfig
	if read == nil {
		read = config.ReadFile
	}
	cfg, err := config.LoadFrom(opts.Env, opts.Profile, opts.Dirs, read)
	if err != nil {
		return Services{}, err
	}
	service := cfg.KeychainService
	if service == "" {
		service = keychain.DefaultService
	}
	fileStore := secretfile.New(cfg.TokenPath)
	keyStore := keychain.New(service, cfg.Profile)
	fileVault := vault.New(fileStore, port.SecretsFile, fileStore.Path())
	keyVault := vault.New(keyStore, port.SecretsKeychain, keyStore.Location())
	var store rmcloud.SecretStore = fileStore
	current := fileVault
	if cfg.Secrets == config.BackendKeychain {
		store, current = keyStore, keyVault
	}
	clientOpts := append([]rmcloud.Option{
		rmcloud.WithEndpoints(cfg.Endpoints),
		rmcloud.WithLogger(opts.Logger),
	}, opts.Client...)
	client, err := rmcloud.New(store, clientOpts...)
	if err != nil {
		return Services{}, fmt.Errorf("start the transport: %w", err)
	}

	remote := cloud.New(client)
	files := localfs.New()
	clk := clock.Real()
	libOpts := []library.Option{library.WithLogger(opts.Logger)}
	if bytes := cacheBytes(opts.CacheBytes); bytes > 0 {
		libOpts = append(libOpts, library.WithCache(blobcache.New(cfg.CacheDir, bytes, clk, opts.Logger)))
	}
	lib := library.New(remote, remote, remote, libOpts...)
	rm := rmpages.New(opts.Logger)
	docs := rmdocs.New()
	reader := pages.NewReader(lib, rm)
	newIDs := ids.New()

	return Services{
		Account:   account.New(remote),
		Browser:   browse.New(lib, reader),
		Fetcher:   fetch.New(lib, remote, files, docs),
		Writer:    write.New(lib, remote, remote, files, pdf.New(), docs, newIDs, clk),
		Watcher:   watch.New(remote),
		Inspector: inspect.New(lib, remote, remote, remote, files),
		Pages:     pages.New(lib, reader, rm, rm, remote, remote, files, newIDs, clk),
		Archives:  archives.New(docs, rm),
		Secrets: secrets.New(cfg.Profile, cfg.SecretsSource, cfg.ConfigFile, current,
			[]port.SecretVault{fileVault, keyVault}, config.Writer{Path: cfg.ConfigFile, Read: read, Env: opts.Env}),
		Config: cfg,
		Close:  client.Close,
	}, nil
}

func cacheBytes(n int64) int64 {
	if n == 0 {
		return DefaultCacheBytes
	}
	return n
}
