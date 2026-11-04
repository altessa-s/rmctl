// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package debug

import (
	"fmt"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

// The kinds of cache entry the report counts.
const (
	entryBlobs   = "blobs"
	entryIndexes = "indexes"
)

func newCache(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache <command>",
		Short: "What the cache on disk holds, and clearing it",
		Long: heredoc.Doc(`
			Every profile keeps its own cache: the indexes of its library by hash,
			its .metadata and .content files, and its last known root with the time
			the cloud confirmed it. status reports the current profile's, or every
			profile's with --all, each with its own budget and what set it (env,
			profile, config or default); clear empties it, or every profile's with
			--all.
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	offline := func(c *cobra.Command) *cobra.Command { return cliutil.WithAccess(c, cliutil.AccessOffline) }
	cmd.AddCommand(offline(newCacheStatus(env)), offline(newCacheClear(env)))
	return cmd
}

func cacheService(cmd *cobra.Command, env *cliutil.Env) (port.Cache, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Cache { return s.Cache })
}

// profileCacheView is one profile's cache as --json prints it.
type profileCacheView struct {
	Profile        string         `json:"profile"`
	Dir            string         `json:"dir"`
	Bytes          int64          `json:"bytes"`
	Budget         int64          `json:"budget"`
	BudgetSource   string         `json:"budget_source"`
	Entries        map[string]int `json:"entries"`
	RootHash       string         `json:"root_hash"`
	RootGeneration int64          `json:"root_generation"`
	Confirmed      string         `json:"confirmed,omitempty"`
}

// cacheView is debug cache status as --json prints it.
type cacheView struct {
	Dir        string             `json:"dir"`
	Profile    string             `json:"profile"`
	TTLSeconds int64              `json:"ttl_seconds"`
	Offline    bool               `json:"offline"`
	NoCache    bool               `json:"no_cache"`
	Profiles   []profileCacheView `json:"profiles"`
}

func newCacheStatus(env *cliutil.Env) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "The cache of this profile, or of every profile",
		Example: heredoc.Doc(`
			rmctl debug cache status
			rmctl debug cache status --all --json
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := cacheService(cmd, env)
			if err != nil {
				return err
			}
			report, err := c.Status(cmd.Context(), all)
			if err != nil {
				return fmt.Errorf("debug cache status: %w", err)
			}
			return env.Emit(cmd, cacheResult(report, all))
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "every profile")
	return cmd
}

func cacheResult(r port.CacheReport, all bool) output.Result {
	v := cacheView{
		Dir: r.Dir, Profile: r.Profile, TTLSeconds: int64(r.TTL / time.Second), Offline: r.Offline, NoCache: r.Disabled,
		Profiles: []profileCacheView{},
	}
	for _, p := range r.Profiles {
		pv := profileCacheView{
			Profile: p.Profile, Dir: p.Dir, Bytes: p.Bytes, Budget: p.Budget, BudgetSource: p.BudgetSource, Entries: p.Entries,
			RootHash: p.RootHash, RootGeneration: p.RootGeneration, Confirmed: cliutil.Stamp(p.Confirmed),
		}
		if pv.Entries == nil {
			pv.Entries = map[string]int{}
		}
		v.Profiles = append(v.Profiles, pv)
	}
	settings := []output.Field{
		{Key: "dir", Value: output.Path(r.Dir)},
		{Key: "ttl", Value: ttlCell(r.TTL)},
		{Key: "offline", Value: output.Bool(r.Offline)},
		{Key: "no_cache", Value: output.Bool(r.Disabled)},
	}
	if !all && len(r.Profiles) == 1 {
		p := r.Profiles[0]
		fields := append([]output.Field{{Key: "profile", Value: output.Text(p.Profile)}}, settings...)
		fields = append(fields,
			output.Field{Key: "bytes", Value: output.Size(p.Bytes)},
			output.Field{Key: "budget", Value: cliutil.BudgetCell(p.Budget)},
			output.Field{Key: "budget_source", Value: output.Text(p.BudgetSource)},
			output.Field{Key: entryBlobs, Value: output.Count(p.Entries[entryBlobs])},
			output.Field{Key: entryIndexes, Value: output.Count(p.Entries[entryIndexes])},
			output.Field{Key: "root_hash", Value: output.ID(p.RootHash)},
			output.Field{Key: "root_generation", Value: generationCell(p)},
			output.Field{Key: "confirmed", Value: output.Time(p.Confirmed)},
		)
		return output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: fields}}}
	}
	t := output.Table{Columns: []output.Column{
		{Title: "profile", Role: output.RoleName},
		{Title: "bytes", Role: output.RoleSize},
		{Title: "budget", Role: output.RoleSize},
		{Title: "from", Role: output.RoleKind},
		{Title: entryBlobs, Role: output.RoleCount},
		{Title: entryIndexes, Role: output.RoleCount},
		{Title: "root", Role: output.RoleHash},
		{Title: "generation", Role: output.RoleCount},
		{Title: "confirmed", Role: output.RoleTime},
	}}
	for _, p := range r.Profiles {
		name := output.Text(p.Profile)
		if p.Profile != r.Profile {
			name = name.With(output.Muted)
		}
		t.Rows = append(t.Rows, []output.Cell{
			name, output.Size(p.Bytes), cliutil.BudgetCell(p.Budget), output.Text(p.BudgetSource),
			output.Count(p.Entries[entryBlobs]), output.Count(p.Entries[entryIndexes]),
			output.ID(p.RootHash), generationCell(p), output.Time(p.Confirmed),
		})
	}
	return output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: settings}, t}}
}

// generationCell is a root's generation, or nothing when no root is known.
func generationCell(p port.ProfileCache) output.Cell {
	if p.Confirmed.IsZero() {
		return output.Text("")
	}
	return output.Count(int(p.RootGeneration))
}

// ttlCell is a TTL, or nothing when it is off.
func ttlCell(d time.Duration) output.Cell {
	if d <= 0 {
		return output.Text("")
	}
	return output.Text(d.String())
}

func newCacheClear(env *cliutil.Env) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Empty the cache of this profile, or of every profile",
		Long: heredoc.Doc(`
			Removes the profile's cached indexes, files and last known root, or every
			profile's with --all. The next command reads the library from the cloud
			again. It asks first; --yes answers for it.
		`),
		Example: heredoc.Doc(`
			rmctl debug cache clear --yes
			rmctl debug cache clear --all
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := cacheService(cmd, env)
			if err != nil {
				return err
			}
			report, err := c.Status(cmd.Context(), all)
			if err != nil {
				return fmt.Errorf("debug cache clear: %w", err)
			}
			names := make([]string, 0, len(report.Profiles))
			for _, p := range report.Profiles {
				names = append(names, p.Profile)
			}
			question := "Clear the cache of profile " + strings.Join(names, ", ") + "?"
			if len(names) > 1 {
				question = "Clear the caches of profiles " + strings.Join(names, ", ") + "?"
			}
			if err = cliutil.Confirm(cmd, env, question); err != nil {
				return err
			}
			cleared, err := c.Clear(cmd.Context(), all)
			if err != nil {
				return fmt.Errorf("debug cache clear: %w", err)
			}
			t := output.Table{Columns: []output.Column{{Title: "cleared", Role: output.RoleName}}}
			for _, n := range cleared {
				t.Rows = append(t.Rows, []output.Cell{output.Text(n)})
			}
			return env.Emit(cmd, output.Result{JSON: struct {
				Cleared []string `json:"cleared"`
			}{Cleared: cleared}, Doc: output.Doc{t}})
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "every profile")
	return cmd
}
