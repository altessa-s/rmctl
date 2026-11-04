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
	"github.com/altessa-s/rmctl/internal/adapter/indexcache"
	"github.com/altessa-s/rmctl/internal/adapter/keychain"
	"github.com/altessa-s/rmctl/internal/adapter/localfs"
	"github.com/altessa-s/rmctl/internal/adapter/pdf"
	"github.com/altessa-s/rmctl/internal/adapter/renderer"
	"github.com/altessa-s/rmctl/internal/adapter/rmdocs"
	"github.com/altessa-s/rmctl/internal/adapter/rmpages"
	"github.com/altessa-s/rmctl/internal/app/account"
	"github.com/altessa-s/rmctl/internal/app/archives"
	"github.com/altessa-s/rmctl/internal/app/browse"
	"github.com/altessa-s/rmctl/internal/app/fetch"
	"github.com/altessa-s/rmctl/internal/app/inspect"
	"github.com/altessa-s/rmctl/internal/app/library"
	"github.com/altessa-s/rmctl/internal/app/pages"
	"github.com/altessa-s/rmctl/internal/app/profile"
	"github.com/altessa-s/rmctl/internal/app/render"
	"github.com/altessa-s/rmctl/internal/app/secrets"
	"github.com/altessa-s/rmctl/internal/app/watch"
	"github.com/altessa-s/rmctl/internal/app/write"
	"github.com/altessa-s/rmctl/internal/port"

	rmcloud "github.com/altessa-s/rmcloud-go"
)

// DefaultCacheBytes bounds each profile's cache on disk: its blobs and
// indexes together.
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
	// CacheBytes bounds each profile's cache; zero is DefaultCacheBytes,
	// negative turns the cache off.
	CacheBytes int64
	// Offline is --offline when it was given; nil defers to the profile's
	// configuration. NoCache is --no-cache: no cache on disk is read or
	// written.
	Offline *bool
	NoCache bool
	// ReadOnly says the command changes nothing, so a root the cloud
	// confirmed within the profile's TTL may serve it.
	ReadOnly bool
	// Client are extra transport options, after the ones the configuration
	// gives: a test points the transport at an emulated cloud with them.
	Client []rmcloud.Option
	// ReadConfig reads the configuration file; nil reads the machine's.
	ReadConfig config.ReadFunc
	// Keychain is the Keychain the tokens of a profile may be kept in; nil
	// is the login Keychain. A test keeps the user's Keychain out of reach
	// with it.
	Keychain KeychainItems
}

// KeychainItems are the Keychain as the keychain adapter needs it.
type KeychainItems = keychain.Items

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
	Rendering port.Rendering
	Secrets   port.Secrets
	Profiles  port.Profiles
	// Cache reports and clears the caches on disk.
	Cache port.Cache
	// Offline says this run never reaches the cloud.
	Offline bool
	// Config is the configuration the services were built from.
	Config config.Config
	// Close releases the transport. It is safe to call more than once.
	Close func() error
}

// ErrNoLogger means Options.Logger is nil.
var ErrNoLogger = errors.New("wiring: a logger is required")

// ErrOfflineNoCache means --offline was asked for with the cache turned off.
var ErrOfflineNoCache = errors.New("--offline reads the cache, and --no-cache turns it off")

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
	tokens := secretsOf(cfg, opts.Keychain)
	fileVault, keyVault := tokens.vaults()
	current := fileVault
	if cfg.Secrets == config.BackendKeychain {
		current = keyVault
	}
	client, err := newClient(tokens.inUse(cfg), cfg, opts)
	if err != nil {
		return Services{}, err
	}

	remote := cloud.New(client)
	files := localfs.New()
	clk := clock.Real()
	st, err := stores(remote, cfg, opts, clk)
	if err != nil {
		return Services{}, err
	}
	libOpts := []library.Option{library.WithLogger(opts.Logger)}
	if st.blobCache != nil {
		libOpts = append(libOpts, library.WithCache(st.blobCache))
	}
	lib := library.New(st.roots, st.docs, st.blobs, libOpts...)
	rm := rmpages.New(opts.Logger)
	docs := rmdocs.New()
	reader := pages.NewReader(lib, rm)
	newIDs := ids.New()

	return Services{
		Account:   account.New(indexcache.NewRegistry(remote, st.profile)),
		Browser:   browse.New(lib, reader),
		Fetcher:   fetch.New(lib, st.blobs, files, docs, clk),
		Writer:    write.New(lib, st.docs, st.blobs, files, pdf.New(), docs, newIDs, clk),
		Watcher:   watch.New(remote),
		Inspector: inspect.New(lib, remote, remote, st.blobs, files),
		Pages:     pages.New(lib, reader, rm, rm, st.docs, st.blobs, files, newIDs, clk),
		Archives:  archives.New(docs, rm),
		Rendering: render.New(lib, st.blobs, files, docs, docs, renderer.New()),
		Secrets: secrets.New(cfg.Profile, cfg.SecretsSource, cfg.ConfigFile, current,
			[]port.SecretVault{fileVault, keyVault}, config.Writer{Path: cfg.ConfigFile, Read: read, Env: opts.Env}),
		Profiles: profile.New(newProfileStore(cfg, opts, read), indexcache.NewCaches(profiles{st.root}, opts.Logger),
			registries{opts: opts, read: read, root: st.root}, cfg.Profile, st.offline),
		Cache:   st.admin,
		Offline: st.offline,
		Config:  cfg,
		Close:   client.Close,
	}, nil
}

// built are the stores the use cases read and write through: the cloud,
// decorated with the profile's cache unless it is off, and refusing every
// request offline.
type built struct {
	roots     port.RootStore
	docs      port.DocumentStore
	blobs     port.BlobStore
	blobCache port.BlobCache
	profile   *blobcache.Cache
	root      *blobcache.Root
	admin     *indexcache.Admin
	offline   bool
}

func stores(remote *cloud.Cloud, cfg config.Config, opts Options, clk port.Clock) (built, error) {
	bytes := cacheBytes(opts.CacheBytes)
	root := blobcache.NewRoot(cfg.CacheDir, max(bytes, 0), clk, opts.Logger)
	profile, err := root.Profile(cfg.Profile)
	if err != nil {
		return built{}, err
	}
	offline := cfg.Offline
	if opts.Offline != nil {
		offline = *opts.Offline
	}
	caching := !opts.NoCache && bytes > 0
	if offline && !caching {
		return built{}, ErrOfflineNoCache
	}
	b := built{roots: remote, docs: remote, blobs: remote, profile: profile, root: root, offline: offline}
	b.admin = indexcache.NewAdmin(profiles{root}, indexcache.Settings{
		Profile: cfg.Profile, TTL: cfg.CacheTTL, Offline: offline, Disabled: !caching,
	}, opts.Logger)
	if !caching {
		return b, nil
	}
	root.RemoveLegacy()
	mode := indexcache.Live
	switch {
	case offline:
		mode = indexcache.Offline
		b.blobs = indexcache.OfflineBlobs{}
	case opts.ReadOnly && cfg.CacheTTL > 0:
		mode = indexcache.Fresh
	}
	ic := indexcache.New(remote, profile, indexcache.Options{
		Profile: cfg.Profile, Mode: mode, TTL: cfg.CacheTTL, Clock: clk, Log: opts.Logger,
	})
	b.roots, b.docs, b.blobCache = ic, ic, profile
	if !offline {
		b.blobs = indexcache.NewBlobs(remote, profile)
	}
	return b, nil
}

// profiles opens the caches of blobcache's root for the cache report.
type profiles struct{ root *blobcache.Root }

func (p profiles) Dir() string     { return p.root.Dir() }
func (p profiles) Names() []string { return p.root.Profiles() }
func (p profiles) Open(profile string) (indexcache.ProfileStore, error) {
	return p.root.Profile(profile)
}

// newClient is the transport of the profile cfg describes, keeping its
// tokens in store.
func newClient(store rmcloud.SecretStore, cfg config.Config, opts Options) (*rmcloud.Client, error) {
	clientOpts := append([]rmcloud.Option{
		rmcloud.WithEndpoints(cfg.Endpoints),
		rmcloud.WithLogger(opts.Logger),
	}, opts.Client...)
	client, err := rmcloud.New(store, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("start the transport: %w", err)
	}
	return client, nil
}

func cacheBytes(n int64) int64 {
	if n == 0 {
		return DefaultCacheBytes
	}
	return n
}
