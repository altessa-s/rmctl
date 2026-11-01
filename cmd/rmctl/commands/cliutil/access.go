// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import "github.com/spf13/cobra"

// Access is how a command may use the cache, which decides whether a TTL
// may answer it and whether it runs under --offline.
type Access string

// The kinds of access.
const (
	// AccessLive is the default: the command changes something, or asks
	// the cloud a question only the cloud can answer. It always reads the
	// live root and refuses to run offline.
	AccessLive Access = ""
	// AccessRead changes nothing but needs the cloud for files the cache
	// does not keep (an original, every page): a TTL may answer its root,
	// and it refuses to run offline.
	AccessRead Access = "read"
	// AccessOffline changes nothing and can run from the cache alone.
	AccessOffline Access = "offline"
)

// accessKey is the annotation that carries a command's access.
const accessKey = "rmctl.access"

// WithAccess marks cmd with access and returns it.
func WithAccess(cmd *cobra.Command, access Access) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[accessKey] = string(access)
	return cmd
}

// AccessOf is cmd's access; an unmarked command is AccessLive.
func AccessOf(cmd *cobra.Command) Access {
	return Access(cmd.Annotations[accessKey])
}
