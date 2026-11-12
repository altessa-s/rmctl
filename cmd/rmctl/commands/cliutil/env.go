// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
)

// ProfileEnv names the profile when --profile is not given.
const ProfileEnv = "RMCTL_PROFILE"

// The persistent flags every command reads.
const (
	FlagProfile = "profile"
	FlagJSON    = "json"
	FlagVerbose = "verbose"
	FlagYes     = "yes"
	FlagFormat  = "format"
	FlagColor   = "color"
	FlagOffline = "offline"
	FlagNoCache = "no-cache"
)

// Services are the use cases the command line drives. The command line
// imports nothing below the input ports.
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
	Notes     port.Notes
	Syncer    port.Syncer
	Secrets   port.Secrets
	Profiles  port.Profiles
	Cache     port.Cache
	// Completion lists the library for shell completion. A factory fills
	// it alone when the request is for completion.
	Completion port.Completion
	Viewing    port.Viewing
	// Mirrors defines and runs the mirrors, and MirrorService is their
	// autostart service.
	Mirrors       port.Mirrors
	MirrorService port.MirrorService
	// Offline says the services never reach the cloud: --offline, or the
	// profile's configuration.
	Offline bool
}

// Streams are the process's standard streams and what is known of the
// terminal. IsTerminal says whether both the input and the error stream are
// a terminal: confirmation prompts and progress lines are only for a person.
// OutTerminal says whether the output stream is one, which makes the answer
// a table; OutColor whether that terminal shows ANSI color; Width is its
// width in columns, 0 when unknown. Getenv reads the environment; nil is the
// process's. Now is the wall clock a progress bar measures its rate by;
// nil is the machine's. After calls f once d has passed unless stop is
// called first, for the line that waits before it shows; nil is the
// machine's timer. Renders says this build draws documents, which
// "version --full" reports: false in rmctl-lite and in a build without cgo;
// the composition root asks the renderer.
type Streams struct {
	In          io.Reader
	Out, Err    io.Writer
	IsTerminal  bool
	OutTerminal bool
	OutColor    bool
	Width       int
	Getenv      func(string) string
	Now         func() time.Time
	After       func(d time.Duration, f func()) (stop func())
	Renders     bool
}

// Request is what the command line asks of the services: the profile, the
// verbosity, and how the command may use the cache.
type Request struct {
	Profile string
	Verbose bool
	// Offline is --offline when it was given; nil leaves it to the
	// profile's configuration.
	Offline *bool
	// NoCache is --no-cache.
	NoCache bool
	// ReadOnly says the command changes nothing, so a root confirmed within
	// the profile's TTL may answer it.
	ReadOnly bool
	// JSON says the answer is JSON: a program reads it, and the line of a
	// slow load is not drawn for one, even at a terminal.
	JSON bool
	// Completion asks for Services.Completion alone, for shell completion:
	// nothing that reaches the cloud, reads a token or writes the cache.
	Completion bool
}

// Factory builds the services once the flags are parsed. The returned
// close releases what they hold.
type Factory func(ctx context.Context, req Request) (Services, func() error, error)

// Env is what every command shares: the streams, the persistent flags and
// the services, built on first use.
type Env struct {
	Streams Streams
	factory Factory

	built    bool
	services Services
	closer   func() error
	err      error

	listed     bool
	listing    port.Completion
	listCloser func() error
}

// NewEnv returns an environment that builds its services with factory.
func NewEnv(streams Streams, factory Factory) *Env {
	return &Env{Streams: streams, factory: factory}
}

// Fixed is a factory that always returns svc, for tests and embedding.
func Fixed(svc Services) Factory {
	return func(context.Context, Request) (Services, func() error, error) {
		return svc, func() error { return nil }, nil
	}
}

// Services builds the services on first use with the profile, verbosity
// and cache flags of cmd, and refuses a command that needs the cloud when
// they are offline.
func (e *Env) Services(cmd *cobra.Command) (Services, error) {
	if e.built {
		return e.services, e.err
	}
	e.built = true
	access := AccessOf(cmd)
	req := Request{Profile: Profile(cmd), ReadOnly: access != AccessLive, JSON: e.JSON(cmd)}
	req.Verbose, _ = cmd.Flags().GetBool(FlagVerbose) //nolint:errcheck // the flag is persistent on the root
	req.NoCache, _ = cmd.Flags().GetBool(FlagNoCache) //nolint:errcheck // the flag is persistent on the root
	if f := cmd.Flags().Lookup(FlagOffline); f != nil && f.Changed {
		on := f.Value.String() == "true"
		req.Offline = &on
	}
	e.services, e.closer, e.err = e.factory(cmd.Context(), req)
	if e.err == nil && e.services.Offline && access != AccessOffline {
		e.err = Usagef("%s needs the cloud: run it without --offline (or with --offline=false)", cmd.CommandPath())
	}
	return e.services, e.err
}

// Listing is the library as the profile's cache last listed it, for
// shell completion, with the profile and --no-cache of cmd; nil when there
// is none. The factory is asked for the completion services alone, once,
// and whatever fails is nil: completion says nothing.
func (e *Env) Listing(cmd *cobra.Command) *domain.Tree {
	if !e.listed {
		e.listed = true
		req := Request{Profile: Profile(cmd), ReadOnly: true, Completion: true}
		req.NoCache, _ = cmd.Flags().GetBool(FlagNoCache) //nolint:errcheck // the flag is persistent on the root
		svc, closer, err := e.factory(cmd.Context(), req)
		if err == nil {
			e.listing, e.listCloser = svc.Completion, closer
		}
	}
	if e.listing == nil {
		return nil
	}
	tree, err := e.listing.Listing(cmd.Context())
	if err != nil {
		return nil
	}
	return tree
}

// Close releases the services if they were built.
func (e *Env) Close() error {
	var errs []error
	for _, c := range []*func() error{&e.listCloser, &e.closer} {
		if *c == nil {
			continue
		}
		closer := *c
		*c = nil
		errs = append(errs, closer())
	}
	return errors.Join(errs...)
}

// Profile is --profile, else $RMCTL_PROFILE, else "".
func Profile(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup(FlagProfile); f != nil && f.Changed {
		return f.Value.String()
	}
	return os.Getenv(ProfileEnv)
}

// DefaultProfile is the profile used when none is named.
const DefaultProfile = "default"

// ProfileName is [Profile], or DefaultProfile when none is named.
func ProfileName(cmd *cobra.Command) string {
	if p := Profile(cmd); p != "" {
		return p
	}
	return DefaultProfile
}

// Yes reports --yes.
func Yes(cmd *cobra.Command) bool {
	on, _ := cmd.Flags().GetBool(FlagYes) //nolint:errcheck // the flag is persistent on the root
	return on
}

// ErrNoService means a command needs a service the factory did not provide.
var ErrNoService = errors.New("rmctl: this command is not available in this build")

// Need returns the service pick selects, building the services on first use;
// a service the factory left nil is [ErrNoService].
func Need[T any](cmd *cobra.Command, env *Env, pick func(Services) T) (T, error) {
	var zero T
	svc, err := env.Services(cmd)
	if err != nil {
		return zero, err
	}
	s := pick(svc)
	if any(s) == nil {
		return zero, ErrNoService
	}
	return s, nil
}

// AddPersistentFlags declares the flags every command reads on root, and
// checks the output flags of env before any command runs, so a bad --format
// is refused before a change is made rather than after.
func AddPersistentFlags(root *cobra.Command, env *Env) {
	flags := root.PersistentFlags()
	flags.StringP(FlagProfile, "P", "", "account profile (or "+ProfileEnv+")")
	flags.Bool(FlagJSON, false, "print answers as JSON (the same as --format json)")
	flags.String(FlagFormat, "", "answer format: table, plain or json (or "+FormatEnv+"; default: table on a terminal, plain otherwise)")
	flags.String(FlagColor, ColorAuto, "color a table: auto, always or never")
	flags.BoolP(FlagVerbose, "v", false, "log what happens on standard error")
	flags.BoolP(FlagYes, "y", false, "confirm a command that changes the library")
	flags.Bool(FlagOffline, false, "read the library from the cache only; never reach the cloud")
	flags.Bool(FlagNoCache, false, "neither read nor write the cache on disk")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		offline, _ := cmd.Flags().GetBool(FlagOffline) //nolint:errcheck // declared just above
		noCache, _ := cmd.Flags().GetBool(FlagNoCache) //nolint:errcheck // declared just above
		if offline && noCache {
			return Usagef("--offline reads the cache, and --no-cache turns it off")
		}
		_, err := env.Output(cmd)
		return err
	}
}
