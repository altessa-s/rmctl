// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fetch

import (
	"errors"
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

// syncView is what a sync did, for --json.
type syncView struct {
	Current    bool   `json:"current"`
	Path       string `json:"path"`
	Root       string `json:"root"`
	Generation int64  `json:"generation"`
	Documents  int    `json:"documents"`
	Fetched    int    `json:"fetched"`
	Cached     int    `json:"cached"`
	Bytes      int64  `json:"bytes_fetched"`
	Needed     int64  `json:"bytes_needed"`
	CacheBytes int64  `json:"cache_bytes"`
	// Budget is 0 for no limit.
	Budget       int64  `json:"budget"`
	BudgetSource string `json:"budget_source"`
}

// mib is a mebibyte, the unit the budget advice rounds up to.
const mib = 1 << 20

// fieldDocuments is the field that counts documents.
const fieldDocuments = "documents"

func newSync(env *cliutil.Env) *cobra.Command {
	var opts port.SyncOptions
	cmd := &cobra.Command{
		Use:     "sync [path]",
		GroupID: cliutil.GroupRead,
		Short:   "Fill the cache ahead of time, for reads without downloads and --offline",
		Long: heredoc.Doc(`
			Fetches into the profile's cache what the reads of the documents below a
			path (the whole library by default) need and the cache lacks, so that they
			download nothing later and run under --offline: every index of the library,
			every .metadata and .content, and each document's .pagedata and .rm pages,
			which text, highlights, find --text and debug page read. --files also
			fetches each document's original and every other file of its index, which
			get, export, mget, backup and render read. Documents in the trash stay out
			unless --all. It changes nothing in the library.

			Files are addressed by their hash, so a file the cache holds is current and
			is never fetched again: a second sync fetches only what changed. A
			complete sync is recorded with the library's root; while the root is the
			same, a sync that the last one covers (the same path, or the whole library,
			with as much) answers at once that the cache is current, after one request.
			--force walks the library anyway.

			Before downloading any document file, it adds up what it keeps (the sizes
			are in the indexes) and refuses a sync that would not fit the profile's
			cache budget: RMCTL_CACHE_BUDGET, else budget under [profiles.<name>.cache]
			in config.toml, else under [cache], else 512 MiB. No page or original is
			downloaded then, but the indexes, .metadata and .content the library is
			measured from may have been read into the cache. A sync whose files do not
			all stay in the cache is refused too, and is not recorded as complete.
			A budget of 0 is no limit: sync never refuses and the cache never evicts.
			Whenever the cache evicts, it forgets the last complete sync.
			On a terminal a progress bar shows the bytes and files done, the rate and
			the time left. Ctrl-C stops it; what was fetched stays in the cache. The
			answer counts the documents, the files fetched and those already cached,
			the bytes fetched, and what the cache takes of its budget.
		`),
		Example: heredoc.Doc(`
			rmctl sync
			rmctl sync /Books --files --jobs 8
			rmctl --offline text /Books/Novel
		`),
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		ValidArgsFunction: cliutil.CompleteRemote(env, true),
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if noCache, _ := cmd.Flags().GetBool(cliutil.FlagNoCache); noCache { //nolint:errcheck // the flag is persistent on the root
				return cliutil.Usage(port.ErrNoCacheToSync)
			}
			if opts.Jobs < 0 {
				return cliutil.Usagef("--jobs is how many files download at once: 1 or more, 0 for the default")
			}
			s, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Syncer { return s.Syncer })
			if err != nil {
				return err
			}
			query := "/"
			if len(args) == 1 {
				query = args[0]
			}
			update, done := cliutil.Transfer(env.Streams, "syncing")
			opts.Progress = update
			r, err := s.Sync(cmd.Context(), query, opts)
			done()
			if err != nil {
				return syncErr(cmd, query, r, err)
			}
			return env.Emit(cmd, syncAnswer(r))
		},
	}
	cmd.Flags().BoolVar(&opts.Files, "files", false, "fetch every file of each document too: originals, for get, export, mget, backup and render")
	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "sync the documents in the trash too")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "walk the library even when the last sync covers this one")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 0, "transfers at a time (0: the default)")
	return cmd
}

// syncErr names what a sync that failed left: a budget too small for it,
// with how to raise it, or the files it kept before it stopped.
func syncErr(cmd *cobra.Command, query string, r port.SyncReport, err error) error {
	if over, ok := errors.AsType[*port.OverBudgetError](err); ok {
		profile := cliutil.ProfileName(cmd)
		if over.Evicted {
			return fmt.Errorf("sync %s: it needs at least %s in the cache of profile %s, and not all of it stays within the budget of %s; "+
				"the sync is not recorded as complete. %s, or sync a smaller path",
				query, cliutil.Size(over.Needed), profile, cliutil.Size(over.Budget), raiseBudget(over, profile))
		}
		return fmt.Errorf("sync %s: it needs %s in the cache of profile %s, and the budget is %s; no page or original was downloaded, "+
			"though the indexes and metadata that measure the library may have been read into the cache. %s, or sync a smaller path",
			query, cliutil.Size(over.Needed), profile, cliutil.Size(over.Budget), raiseBudget(over, profile))
	}
	if r.Fetched > 0 {
		cliutil.Notef(cmd.ErrOrStderr(), "%d files (%s) fetched before the sync stopped stay in the cache", r.Fetched, cliutil.Size(r.Bytes))
	}
	return fmt.Errorf("sync: %w", err)
}

// raiseBudget names the setting that set the profile's budget, which is the
// one to raise: RMCTL_CACHE_BUDGET over everything, else the profile's
// table, else [cache], which bounds every profile, or, when nothing set it,
// the profile's table.
func raiseBudget(over *port.OverBudgetError, profile string) string {
	raise := (over.Needed + mib - 1) / mib
	if over.Evicted {
		// What it needs did not stay within the budget: above it, then.
		raise = max(raise, over.Budget/mib+1)
	}
	switch over.Source {
	case port.BudgetFromEnv:
		return fmt.Sprintf("Raise RMCTL_CACHE_BUDGET, which bounds every profile, to at least %dMiB (0 is no limit)", raise)
	case port.BudgetFromConfig:
		return fmt.Sprintf("Raise budget under [cache] in config.toml to at least %dMiB for every profile, "+
			"or set it under [profiles.%s.cache] for this one alone (0 is no limit)", raise, profile)
	default:
		return fmt.Sprintf("Raise the budget to at least %dMiB with budget under [profiles.%s.cache] in config.toml (0 is no limit)",
			raise, profile)
	}
}

func syncAnswer(r port.SyncReport) output.Result {
	return output.Fields(syncView(r),
		output.Field{Key: "current", Value: output.Bool(r.Current)},
		output.Field{Key: "path", Value: output.Path(r.Path)},
		output.Field{Key: fieldDocuments, Value: output.Count(r.Documents)},
		output.Field{Key: "fetched", Value: output.Count(r.Fetched)},
		output.Field{Key: "cached", Value: output.Count(r.Cached)},
		output.Field{Key: "bytes_fetched", Value: output.Size(r.Bytes)},
		output.Field{Key: "bytes_needed", Value: output.Size(r.Needed)},
		output.Field{Key: "cache_bytes", Value: output.Size(r.CacheBytes)},
		output.Field{Key: "budget", Value: cliutil.BudgetCell(r.Budget)},
		output.Field{Key: "budget_source", Value: output.Text(r.BudgetSource)},
		output.Field{Key: "generation", Value: output.Count(int(r.Generation))})
}
