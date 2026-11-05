// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

func profilesService(cmd *cobra.Command, env *cliutil.Env) (port.Profiles, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Profiles { return s.Profiles })
}

func newProfile(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "profile <command>",
		GroupID: GroupID,
		Short:   "List, add, inspect and delete profiles",
		Long: heredoc.Doc(`
			A profile is one account on this machine: its tokens, its directory under
			the configuration directory, its cache, and its tables in config.toml.
			--profile/-P or RMCTL_PROFILE chooses the one a command uses; "default"
			when neither does.

			ls lists every profile this machine keeps something of, info shows one in
			detail, add registers a new one with a one-time code, and del removes one
			entirely, after a question.
		`),
		Example: heredoc.Doc(`
			rmctl profile ls
			rmctl profile add work abcdefgh
			rmctl profile info work
			rmctl -P work ls /
			rmctl profile del work
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	offline := func(c *cobra.Command) *cobra.Command { return cliutil.WithAccess(c, cliutil.AccessOffline) }
	cmd.AddCommand(offline(newProfileList(env)), newProfileAdd(env), offline(newProfileInfo(env)), offline(newProfileDel(env)))
	return cmd
}

// profileView is one profile as profile ls --json prints it.
type profileView struct {
	Profile    string `json:"profile"`
	Current    bool   `json:"current"`
	Registered bool   `json:"registered"`
	TokenError string `json:"token_error,omitempty"`
	Secrets    string `json:"secrets"`
	CacheBytes int64  `json:"cache_bytes"`
	// CacheBudget is 0 for no limit.
	CacheBudget       int64  `json:"cache_budget"`
	CacheBudgetSource string `json:"cache_budget_source"`
}

func newProfileList(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "Every profile of this machine",
		Long: heredoc.Doc(`
			Lists every profile this machine keeps something of — a directory, a table
			in config.toml, a cache — and the one in use when it is registered: whether
			it holds a device token, where its tokens are kept, what its cache takes,
			and its own cache budget ("unlimited" for 0). The profile this command runs
			as is marked current. Nothing is changed, and nothing is asked of the cloud.
		`),
		Example: heredoc.Doc(`
			rmctl profile ls
			rmctl profile ls --json
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := profilesService(cmd, env)
			if err != nil {
				return err
			}
			list, err := p.List(cmd.Context())
			if err != nil {
				return fmt.Errorf("profile ls: %w", err)
			}
			views := make([]profileView, 0, len(list))
			t := output.Table{Columns: []output.Column{
				{Title: keyProfile, Role: output.RoleName},
				{Title: "current"},
				{Title: keyRegistered},
				{Title: keySecrets, Role: output.RoleKind},
				{Title: "cache", Role: output.RoleSize},
				{Title: "budget", Role: output.RoleSize},
			}}
			for _, s := range list {
				v := profileView{Profile: s.Name, Current: s.Current, Registered: s.Registered, Secrets: string(s.Backend), CacheBytes: s.CacheBytes,
					CacheBudget: s.CacheBudget, CacheBudgetSource: s.CacheBudgetSource}
				registered := output.Bool(s.Registered)
				if s.TokenErr != nil {
					v.TokenError = s.TokenErr.Error()
					registered = output.Text("unknown")
					cliutil.Notef(cmd.ErrOrStderr(), "profile %s: its tokens cannot be read: %v", s.Name, s.TokenErr)
				}
				name := output.Text(s.Name)
				if !s.Current {
					name = name.With(output.Muted)
				}
				views = append(views, v)
				t.Rows = append(t.Rows, []output.Cell{name, output.Bool(s.Current), registered, output.Text(v.Secrets), output.Size(s.CacheBytes),
					cliutil.BudgetCell(s.CacheBudget)})
			}
			return env.Emit(cmd, output.Result{JSON: views, Doc: output.Doc{t}})
		},
	}
}

// addArgs are profile add's arguments: the name and the code.
const addArgs = 2

func newProfileAdd(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> <code>",
		Short: "Register a new profile with a one-time code",
		Long: heredoc.Doc(`
			Registers the profile name with a one-time code, as
			rmctl -P <name> register <code> does: the device token is stored where the
			profile's configuration says, in its file or the Keychain. Take the
			eight-character code from https://my.remarkable.com/device/desktop/connect.

			A name is 1-64 characters of a-z, 0-9, _ and -, starting with a letter or
			digit. A profile that already holds a device token is refused; register
			--replace registers it again.
		`),
		Example: heredoc.Doc(`
			rmctl profile add work abcdefgh
			rmctl -P work account
		`),
		Args:         cliutil.Args(cobra.ExactArgs(addArgs)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := profilesService(cmd, env)
			if err != nil {
				return err
			}
			name := args[0]
			if err = p.Add(cmd.Context(), name, args[1]); err != nil {
				if errors.Is(err, port.ErrProfileRegistered) {
					return fmt.Errorf("profile add: %w; to register it again: rmctl -P %s register --replace <code>", err, name)
				}
				return fmt.Errorf("profile add: %w", err)
			}
			return env.Emit(cmd, output.Result{
				JSON: struct {
					Profile    string `json:"profile"`
					Registered bool   `json:"registered"`
				}{Profile: name, Registered: true},
				Doc: output.Doc{output.Body{Text: "registered profile " + name}},
			})
		},
	}
}

// profileInfoView is profile info as --json prints it.
type profileInfoView struct {
	Profile         string         `json:"profile"`
	Current         bool           `json:"current"`
	Exists          bool           `json:"exists"`
	ConfigDir       string         `json:"config_dir"`
	ConfigDirExists bool           `json:"config_dir_exists"`
	Secrets         string         `json:"secrets"`
	SecretsSource   string         `json:"secrets_source"`
	TokenLocation   string         `json:"token_location"`
	Tokens          []string       `json:"tokens"`
	TokenError      string         `json:"token_error,omitempty"`
	Registered      bool           `json:"registered"`
	Account         string         `json:"account"`
	AccountError    string         `json:"account_error,omitempty"`
	Level           string         `json:"level,omitempty"`
	SyncScope       string         `json:"sync_scope,omitempty"`
	CacheDir        string         `json:"cache_dir"`
	CacheBytes      int64          `json:"cache_bytes"`
	CacheBudget     int64          `json:"cache_budget"`
	CacheBudgetFrom string         `json:"cache_budget_source"`
	CacheEntries    map[string]int `json:"cache_entries"`
	Confirmed       string         `json:"confirmed,omitempty"`
	ConfigFile      string         `json:"config_file"`
	ConfigTable     bool           `json:"config_table"`
	CacheTTLSeconds int64          `json:"cache_ttl_seconds"`
	Offline         bool           `json:"offline"`
}

// The states of profile info's account field: what the cloud said, or why
// it was not heard.
const (
	accountAsked   = "asked"
	accountOffline = "offline"
	accountFailed  = "failed"
)

func newProfileInfo(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "info [name]",
		Short: "One profile in detail",
		Long: heredoc.Doc(`
			Shows one profile, the one in use unless named: its directory, where its
			tokens are kept and which are there (never their values), whether it is
			registered, its cache with its size and budget, and the settings of
			config.toml that apply to it.

			For a registered profile the cloud is asked what account shows: the
			subscription's level and sync scope. Under --offline, or when the cloud
			does not answer, what this machine knows is shown and a note says why the
			account is missing.
		`),
		Example: heredoc.Doc(`
			rmctl profile info
			rmctl profile info work --json
			rmctl profile info work --offline
		`),
		Args:              cliutil.Args(cobra.MaximumNArgs(1)),
		SilenceUsage:      true,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := profilesService(cmd, env)
			if err != nil {
				return err
			}
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			info, err := p.Info(cmd.Context(), name, true)
			if err != nil {
				return fmt.Errorf("profile info: %w", err)
			}
			v := infoOf(info)
			switch {
			case v.TokenError != "":
				cliutil.Notef(cmd.ErrOrStderr(), "its tokens cannot be read: %s", v.TokenError)
			case v.Account == accountOffline:
				cliutil.Notef(cmd.ErrOrStderr(), "--offline: the account was not asked; this is what this machine knows")
			case v.Account == accountFailed:
				cliutil.Notef(cmd.ErrOrStderr(), "the cloud did not answer (%s); this is what this machine knows", v.AccountError)
			case !info.Exists:
				cliutil.Notef(cmd.ErrOrStderr(), "this machine keeps nothing of profile %s", v.Profile)
			}
			return env.Emit(cmd, output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: []output.Field{
				{Key: keyProfile, Value: output.Text(v.Profile)},
				{Key: "current", Value: output.Bool(v.Current)},
				{Key: "exists", Value: output.Bool(v.Exists)},
				{Key: "config_dir", Value: output.Path(v.ConfigDir)},
				{Key: "config_dir_exists", Value: output.Bool(v.ConfigDirExists)},
				{Key: keySecrets, Value: output.Text(v.Secrets)},
				{Key: "secrets_source", Value: output.Text(v.SecretsSource)},
				{Key: "token_location", Value: output.Path(v.TokenLocation)},
				{Key: "tokens", Value: output.Text(strings.Join(v.Tokens, ","))},
				{Key: keyRegistered, Value: output.Bool(v.Registered)},
				{Key: "account", Value: output.Text(v.Account)},
				{Key: "level", Value: output.Text(v.Level)},
				{Key: "sync_scope", Value: output.Text(v.SyncScope)},
				{Key: "cache_dir", Value: output.Path(v.CacheDir)},
				{Key: "cache_bytes", Value: output.Size(v.CacheBytes)},
				{Key: "cache_budget", Value: cliutil.BudgetCell(v.CacheBudget)},
				{Key: "cache_budget_source", Value: output.Text(v.CacheBudgetFrom)},
				{Key: "cache_blobs", Value: output.Count(v.CacheEntries["blobs"])},
				{Key: "cache_indexes", Value: output.Count(v.CacheEntries["indexes"])},
				{Key: "confirmed", Value: output.Time(info.Cache.Confirmed)},
				{Key: keyConfigFile, Value: output.Path(v.ConfigFile)},
				{Key: "config_table", Value: output.Bool(v.ConfigTable)},
				{Key: "cache_ttl", Value: durationCell(info.CacheTTL)},
				{Key: "offline", Value: output.Bool(v.Offline)},
			}}}})
		},
	}
}

func infoOf(info port.ProfileInfo) profileInfoView {
	v := profileInfoView{
		Profile: info.Name, Current: info.Current, Exists: info.Exists,
		ConfigDir: info.ConfigDir, ConfigDirExists: info.DirExists,
		Secrets: string(info.Backend), SecretsSource: info.BackendSource, TokenLocation: info.TokenLocation,
		Tokens: info.Tokens, Registered: info.Registered,
		CacheDir: info.Cache.Dir, CacheBytes: info.Cache.Bytes, CacheEntries: info.Cache.Entries,
		CacheBudget: info.Cache.Budget, CacheBudgetFrom: info.Cache.BudgetSource,
		Confirmed:  cliutil.Stamp(info.Cache.Confirmed),
		ConfigFile: info.ConfigFile, ConfigTable: info.Table, CacheTTLSeconds: int64(info.CacheTTL / time.Second), Offline: info.Offline,
	}
	if v.Tokens == nil {
		v.Tokens = []string{}
	}
	if v.CacheEntries == nil {
		v.CacheEntries = map[string]int{}
	}
	if info.TokenErr != nil {
		v.TokenError = info.TokenErr.Error()
	}
	switch {
	case !info.Registered:
	case errors.Is(info.AccountErr, port.ErrOffline):
		v.Account = accountOffline
	case info.AccountErr != nil:
		v.Account, v.AccountError = accountFailed, info.AccountErr.Error()
	default:
		v.Account, v.Level, v.SyncScope = accountAsked, info.Account.Plan.Level, info.Account.Plan.SyncScope
	}
	return v
}

// durationCell is a duration, or nothing when it is off.
func durationCell(d time.Duration) output.Cell {
	if d <= 0 {
		return output.Text("")
	}
	return output.Text(d.String())
}

// removalView is what profile del did, as --json prints it.
type removalView struct {
	Profile        string   `json:"profile"`
	Revoked        bool     `json:"revoked"`
	FileTokens     []string `json:"file_tokens"`
	KeychainTokens []string `json:"keychain_tokens"`
	Unreachable    []string `json:"unreachable,omitempty"`
	Dir            string   `json:"dir"`
	DirRemoved     bool     `json:"dir_removed"`
	CacheDir       string   `json:"cache_dir"`
	CacheRemoved   bool     `json:"cache_removed"`
	ConfigFile     string   `json:"config_file"`
	TableRemoved   bool     `json:"table_removed"`
}

func newProfileDel(env *cliutil.Env) *cobra.Command {
	var revoke bool
	cmd := &cobra.Command{
		Use:   "del <name>",
		Short: "Delete a profile and everything this machine keeps of it",
		Long: heredoc.Doc(`
			Removes the profile entirely, in this order: its tokens, from its file and
			from the Keychain; its directory; its cache; and its tables in config.toml,
			taken out line by line so every other line and comment stays as it is.
			Files rmctl did not write are left where they are, and their directory
			with them. A step that fails stops the removal; the profile is still
			listed until the last step, and running del again finishes it.

			Like logout, it drops the device token and tells the cloud nothing: the
			device stays on the account until it is removed at
			https://my.remarkable.com. --revoke removes it from the account first, and
			a revoke that fails changes nothing; it needs the cloud, so --offline
			refuses it.

			It asks first, and says so when the profile is the one in use; --yes
			answers for it. A profile this machine keeps nothing of is refused.
		`),
		Example: heredoc.Doc(`
			rmctl profile del work
			rmctl profile del work --revoke --yes
		`),
		Args:         cliutil.Args(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := profilesService(cmd, env)
			if err != nil {
				return err
			}
			name := args[0]
			info, err := p.Info(cmd.Context(), name, false)
			if err != nil {
				return fmt.Errorf("profile del: %w", err)
			}
			if !info.Exists {
				return fmt.Errorf("profile del: %s: %w", name, port.ErrNoProfile)
			}
			if svc, _ := env.Services(cmd); revoke && svc.Offline {
				return cliutil.Usagef("%s --revoke needs the cloud: run it without --offline", cmd.CommandPath())
			}
			if err = cliutil.Confirm(cmd, env, delQuestion(name, info.Current, revoke)); err != nil {
				return err
			}
			removal, err := p.Remove(cmd.Context(), name, port.RemoveProfileOptions{Revoke: revoke})
			if emitErr := printRemoval(cmd, env, removal); err == nil {
				err = emitErr
			}
			if err != nil {
				return fmt.Errorf("profile del: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "remove the device from the account first")
	return cmd
}

func delQuestion(name string, current, revoke bool) string {
	var q strings.Builder
	if revoke {
		fmt.Fprintf(&q, "Remove the device of profile %s from the account, then delete", name)
	} else {
		fmt.Fprintf(&q, "Delete profile %s", name)
	}
	if current {
		q.WriteString(" — the profile this command runs as —")
	}
	q.WriteString(" with its tokens, its directory, its cache and its tables in config.toml?")
	return q.String()
}

func printRemoval(cmd *cobra.Command, env *cliutil.Env, r port.ProfileRemoval) error {
	v := removalView{
		Profile: r.Name, Revoked: r.Revoked, FileTokens: []string{}, KeychainTokens: []string{},
		Dir: r.Dir.Path, DirRemoved: r.Dir.Existed && !r.Dir.Kept,
		CacheDir: r.Cache.Path, CacheRemoved: r.Cache.Existed && !r.Cache.Kept,
		ConfigFile: r.ConfigFile, TableRemoved: r.Table,
	}
	for _, t := range r.Tokens {
		switch t.Backend {
		case port.SecretsFile:
			v.FileTokens = append(v.FileTokens, t.Names...)
		case port.SecretsKeychain:
			v.KeychainTokens = append(v.KeychainTokens, t.Names...)
		}
	}
	for _, b := range r.Unreachable {
		v.Unreachable = append(v.Unreachable, string(b))
	}
	if r.Name == "" {
		return nil
	}
	err := env.Emit(cmd, output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: []output.Field{
		{Key: keyProfile, Value: output.Text(v.Profile)},
		{Key: "revoked", Value: output.Bool(v.Revoked)},
		{Key: "file_tokens", Value: output.Text(strings.Join(v.FileTokens, ","))},
		{Key: "keychain_tokens", Value: output.Text(strings.Join(v.KeychainTokens, ","))},
		{Key: "dir", Value: output.Path(v.Dir)},
		{Key: "dir_removed", Value: output.Bool(v.DirRemoved)},
		{Key: "cache_dir", Value: output.Path(v.CacheDir)},
		{Key: "cache_removed", Value: output.Bool(v.CacheRemoved)},
		{Key: keyConfigFile, Value: output.Path(v.ConfigFile)},
		{Key: "table_removed", Value: output.Bool(v.TableRemoved)},
	}}}})
	if r.Dir.Kept {
		cliutil.Notef(cmd.ErrOrStderr(), "%s holds files rmctl did not write; they and the directory were left", r.Dir.Path)
	}
	if r.Cache.Kept {
		cliutil.Notef(cmd.ErrOrStderr(), "%s holds files rmctl did not write; they and the directory were left", r.Cache.Path)
	}
	for _, b := range v.Unreachable {
		cliutil.Notef(cmd.ErrOrStderr(), "this build cannot reach the %s; tokens of profile %s there, if any, were left", b, r.Name)
	}
	return err
}
