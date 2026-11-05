// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account

import (
	"fmt"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/output"
	"github.com/altessa-s/rmctl/internal/port"
)

// GroupID is the help group of the account commands.
const GroupID = "account"

// The keys several answers share.
const (
	keyProfile    = "profile"
	keyRegistered = "registered"
	keySecrets    = "secrets"
	keyConfigFile = "config_file"
)

// Commands are register, logout, account, secrets and profile.
func Commands(env *cliutil.Env) []*cobra.Command {
	return []*cobra.Command{
		newRegister(env), newLogout(env), cliutil.WithAccess(newAccount(env), cliutil.AccessRead), newSecrets(env), newProfile(env),
	}
}

func service(cmd *cobra.Command, env *cliutil.Env) (port.Account, error) {
	return cliutil.Need(cmd, env, func(s cliutil.Services) port.Account { return s.Account })
}

func newRegister(env *cliutil.Env) *cobra.Command {
	var replace bool
	cmd := &cobra.Command{
		Use:     "register <code>",
		GroupID: GroupID,
		Short:   "Register this machine with a one-time code",
		Long: heredoc.Doc(`
			Exchanges a one-time code for a device token and stores the token for the
			profile. Take the eight-character code from
			https://my.remarkable.com/device/desktop/connect.

			A profile that already holds a token is refused; run logout first, or pass
			--replace. The token is never printed.
		`),
		Example: heredoc.Doc(`
			rmctl register abcdefgh
			rmctl register abcdefgh --profile work --replace
		`),
		Args:         cliutil.Args(cobra.ExactArgs(1)),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			acc, err := service(cmd, env)
			if err != nil {
				return err
			}
			if err = acc.Register(cmd.Context(), args[0], replace); err != nil {
				return fmt.Errorf("register: %w", err)
			}
			return env.Emit(cmd, output.Result{Doc: output.Doc{output.Body{Text: "registered"}}})
		},
	}
	cmd.Flags().BoolVar(&replace, "replace", false, "replace a token the profile already holds")
	return cmd
}

func newLogout(env *cliutil.Env) *cobra.Command {
	var revoke bool
	cmd := &cobra.Command{
		Use:     "logout",
		GroupID: GroupID,
		Short:   "Drop the profile's device token",
		Long: heredoc.Doc(`
			Drops the stored device token of the profile. With --revoke the device is
			first removed from the account at the cloud.
		`),
		Example: heredoc.Doc(`
			rmctl logout
			rmctl logout --revoke
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			acc, err := service(cmd, env)
			if err != nil {
				return err
			}
			if err = acc.Logout(cmd.Context(), revoke); err != nil {
				return fmt.Errorf("logout: %w", err)
			}
			msg := "logged out"
			if revoke {
				msg = "revoked and logged out"
			}
			return env.Emit(cmd, output.Result{Doc: output.Doc{output.Body{Text: msg}}})
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "remove the device from the account first")
	return cmd
}

// infoView is the account as --json prints it.
type infoView struct {
	Registered bool   `json:"registered"`
	Level      string `json:"level"`
	SyncScope  string `json:"sync_scope"`
	Secrets    string `json:"secrets,omitempty"`
}

func newAccount(env *cliutil.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "account",
		GroupID: GroupID,
		Short:   "Show the registration and the subscription",
		Long: heredoc.Doc(`
			Shows whether the profile is registered and what the account's token says
			about its subscription: the level and the sync scope. No token or claim
			beyond these is printed.
		`),
		Example: heredoc.Doc(`
			rmctl account
			rmctl account --json
		`),
		Args:         cliutil.Args(cobra.NoArgs),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			acc, err := service(cmd, env)
			if err != nil {
				return err
			}
			info, err := acc.Info(cmd.Context())
			if err != nil {
				return fmt.Errorf("account: %w", err)
			}
			view := infoView{Registered: info.Registered, Level: info.Plan.Level, SyncScope: info.Plan.SyncScope}
			// The token store is part of the answer: one that cannot be read —
			// a locked Keychain, an unreadable token file — fails the command
			// rather than print an account that looks fine.
			secrets, err := cliutil.Need(cmd, env, func(s cliutil.Services) port.Secrets { return s.Secrets })
			if err != nil {
				return err
			}
			st, err := secrets.Status(cmd.Context())
			if err != nil {
				return fmt.Errorf("account: token store: %w", err)
			}
			view.Secrets = string(st.Backend)
			return env.Emit(cmd, output.Result{JSON: view, Doc: output.Doc{output.Record{Fields: []output.Field{
				{Key: keyRegistered, Value: output.Bool(view.Registered)},
				{Key: "level", Value: output.Text(view.Level)},
				{Key: "sync_scope", Value: output.Text(view.SyncScope)},
				{Key: keySecrets, Value: output.Text(view.Secrets)},
			}}}})
		},
	}
}
