// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package profile lists, adds, describes and removes the profiles of this
// machine. It implements port.Profiles on port.ProfileStore,
// port.ProfileCaches and port.ProfileRegistries, and registers a profile,
// revokes its device and reads its account through the account use case.
//
// A profile is known by its directory, its table in config.toml or its
// cache; the one in use is known as well once it holds a device token.
// Removing one takes its tokens first, then its directory, its cache, and
// its tables in config.toml last: until the end, the profile stays known,
// so a removal that stopped part way is finished by running it again.
package profile
