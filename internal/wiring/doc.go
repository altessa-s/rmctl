// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package wiring is the composition root: the one place that knows every
// adapter and every use case, and connects them.
//
// [Build] resolves the configuration of a profile, opens the token file,
// starts the rmcloud-go transport, and hands the adapters to the use cases.
// The command line receives only the input ports in [Services].
package wiring
