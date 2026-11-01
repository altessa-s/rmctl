// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

func secretsService(cmd *cobra.Command, env *cliutil.Env) (port.Secrets, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Secrets { return s.Secrets })
}

func newSecrets(env *cliutil.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		GroupID: GroupID,
		Short:   "Show and move where the profile keeps its tokens",
		Long: heredoc.Doc(`
			A profile keeps its device and user tokens in a file of mode 0600 in its
			directory, or in the macOS Keychain. config.toml chooses, per profile or
			for all of them; RMCTL_SECRETS=file|keychain overrides it:

			  [secrets]
			  backend = "file"

			  [profiles.work.secrets]
			  backend = "keychain"

			status says where the tokens are and which are there, never their values.
			migrate moves them, and --set-default records the new place in
			config.toml.
		`),
		SilenceUsage: true,
	}
	cmd.AddCommand(newSecretsStatus(env), newSecretsMigrate(env))
	return cmd
}

type statusView struct {
	Profile    string   `json:"profile"`
	Backend    string   `json:"backend"`
	Location   string   `json:"location"`
	Source     string   `json:"source"`
	ConfigFile string   `json:"config_file"`
	Present    []string `json:"present"`
}

func newSecretsStatus(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Where the profile's tokens are, and which are there",
		Example:      "  rmctl secrets status\n  rmctl secrets status --json",
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := secretsService(cmd, env)
			if err != nil {
				return err
			}
			st, err := s.Status(cmd.Context())
			if err != nil {
				return fmt.Errorf("secrets: %w", err)
			}
			v := statusView{
				Profile: st.Profile, Backend: string(st.Backend), Location: st.Location,
				Source: st.Source, ConfigFile: st.ConfigFile, Present: st.Present,
			}
			if v.Present == nil {
				v.Present = []string{}
			}
			return env.Emit(cmd, output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: []output.Field{
				{Key: "profile", Value: output.Text(v.Profile)},
				{Key: "backend", Value: output.Text(v.Backend)},
				{Key: "source", Value: output.Text(v.Source)},
				{Key: "location", Value: output.Path(v.Location)},
				{Key: "config_file", Value: output.Path(v.ConfigFile)},
				{Key: "present", Value: output.Text(strings.Join(v.Present, ","))},
			}}}})
		},
	}
}

func newSecretsMigrate(env *cliutil.Env) *cobra.Command {
	var to string
	var opts port.MigrateOptions
	cmd := &cobra.Command{
		Use:   "migrate --to file|keychain",
		Short: "Move the profile's tokens to the file or the Keychain",
		Long: heredoc.Doc(`
			Copies every token to the target, reads each back, and only then removes it
			from where it was: a failure on the way leaves the tokens where they were.
			A target that already holds a token of the profile is refused unless
			--replace. --set-default appends the profile's table to config.toml, so the
			next command uses the new place; without it, the lines to add are printed.
			RMCTL_SECRETS overrides config.toml: --set-default is refused while it
			names another backend than the target, since the next command would
			still look there.
		`),
		Example: heredoc.Doc(`
			rmctl secrets migrate --to keychain --set-default --yes
			rmctl -P work secrets migrate --to file --yes
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := port.SecretsBackend(to)
			if target != port.SecretsFile && target != port.SecretsKeychain {
				return cliutil.Usagef("%s: --to is file or keychain", cmd.CommandPath())
			}
			s, err := secretsService(cmd, env)
			if err != nil {
				return err
			}
			if err = cliutil.Confirm(cmd, env, fmt.Sprintf("Move the profile's tokens to the %s?", to)); err != nil {
				return err
			}
			m, err := s.Migrate(cmd.Context(), target, opts)
			if err != nil {
				return fmt.Errorf("secrets migrate: %w", err)
			}
			return printMigration(cmd, env, m)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "where to move the tokens: file or keychain")
	cmd.Flags().BoolVar(&opts.Replace, "replace", false, "overwrite tokens the target already holds")
	cmd.Flags().BoolVar(&opts.SetDefault, "set-default", false, "record the target as the profile's backend in config.toml")
	_ = cmd.MarkFlagRequired("to") //nolint:errcheck // the flag exists
	return cmd
}

type migrationView struct {
	From        string   `json:"from"`
	To          string   `json:"to"`
	Location    string   `json:"location"`
	Moved       []string `json:"moved"`
	DefaultSet  bool     `json:"default_set"`
	ConfigFile  string   `json:"config_file"`
	ConfigLines string   `json:"config_lines,omitempty"`
	Override    string   `json:"override,omitempty"`
}

func printMigration(cmd *cobra.Command, env *cliutil.Env, m port.SecretsMigration) error {
	v := migrationView{
		From: string(m.From), To: string(m.To), Location: m.Location, Moved: m.Moved,
		DefaultSet: m.DefaultSet, ConfigFile: m.ConfigFile, ConfigLines: m.ConfigLines, Override: m.Override,
	}
	err := env.Emit(cmd, output.Result{JSON: v, Doc: output.Doc{output.Record{Fields: []output.Field{
		{Key: "from", Value: output.Text(v.From)},
		{Key: "to", Value: output.Text(v.To)},
		{Key: "location", Value: output.Path(v.Location)},
		{Key: "moved", Value: output.Text(strings.Join(v.Moved, ","))},
		{Key: "default_set", Value: output.Bool(v.DefaultSet)},
		{Key: "config_file", Value: output.Path(v.ConfigFile)},
	}}}})
	if err != nil || v.DefaultSet || env.JSON(cmd) {
		return err
	}
	cliutil.Notef(cmd.ErrOrStderr(), "rmctl keeps looking in the %s until %s says otherwise; add:\n%s", v.From, v.ConfigFile, v.ConfigLines)
	if v.Override != "" {
		name, _, _ := strings.Cut(v.Override, "=")
		cliutil.Notef(cmd.ErrOrStderr(), "%s in the environment overrides %s: unset %s, or set %s=%s", v.Override, v.ConfigFile, name, name, v.To)
	}
	return nil
}
