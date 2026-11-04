// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// EnvCacheBudget is the cache budget over the configuration file, for
// every profile.
const EnvCacheBudget = "RMCTL_CACHE_BUDGET"

// DefaultCacheBudget bounds a profile's cache when nothing sets a budget.
const DefaultCacheBudget int64 = 512 << 20

// MinCacheBudget is the smallest budget a profile's cache takes, other than
// 0 for no limit: a cache smaller than one page of a notebook keeps nothing
// worth keeping.
const MinCacheBudget int64 = 1 << 20

// sizeUnits are the units a size takes, by their lower-case name: binary
// (KiB, MiB, GiB, TiB) and decimal (KB, MB, GB, TB), and B for bytes.
var sizeUnits = map[string]int64{ //nolint:gochecknoglobals // a constant table
	"": 1, "b": 1,
	"kib": kib, "mib": kib * kib, "gib": kib * kib * kib, "tib": kib * kib * kib * kib,
	"kb": kb, "mb": kb * kb, "gb": kb * kb * kb, "tb": kb * kb * kb * kb,
}

// kib and kb are the binary and the decimal thousand.
const (
	kib = 1024
	kb  = 1000
)

// ParseSize reads a size such as "512MiB", "4GiB", "1.5 GB" or "1048576":
// a number, then a unit (any case, a space between allowed), bytes without
// one.
func ParseSize(s string) (int64, bool) {
	bytes, ok := parseSize(s)
	return int64(bytes), ok
}

// parseSize is ParseSize before the bytes are rounded down to a whole
// number: "0.5" is half a byte, not 0.
func parseSize(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i < 0 {
		i = len(s)
	}
	unit, ok := sizeUnits[strings.ToLower(strings.TrimSpace(s[i:]))]
	if !ok || i == 0 {
		return 0, false
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || n < 0 {
		return 0, false
	}
	bytes := n * float64(unit)
	if bytes >= math.MaxInt64 {
		return 0, false
	}
	return bytes, true
}

// parseBudget reads a cache budget: 0 (in any unit) for no limit, else a
// size of at least MinCacheBudget; "" is none set. 0 keeps everything, it
// does not turn the cache off: --no-cache does.
func parseBudget(key, value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	n, ok := parseSize(value)
	if !ok || (n != 0 && n < float64(MinCacheBudget)) {
		return 0, fmt.Errorf("%w: %s = %q: a budget is a size of at least 1MiB, such as \"512MiB\" or \"4GiB\", or 0 for no limit", ErrConfig, key, value)
	}
	return int64(n), nil
}

// Budget is a profile's cache budget: Bytes of blobs and indexes, 0 for no
// limit, and Source, what set it: SourceEnv, SourceProfile, SourceConfig
// (the file's [cache]) or SourceDefault.
type Budget struct {
	Bytes  int64
	Source string
}

// Unlimited reports a budget of no limit.
func (b Budget) Unlimited() bool { return b.Bytes == 0 }

// Budgets chooses the cache budget of any profile, as LoadFrom read the
// environment and the configuration file: every profile's cache is bounded
// by its own.
type Budgets struct {
	// env is RMCTL_CACHE_BUDGET and file the configuration file, both
	// already checked.
	env  string
	file File
}

// Of is the budget of profile: RMCTL_CACHE_BUDGET, which applies to every
// profile, then the profile's [profiles.<name>.cache], then [cache], then
// DefaultCacheBudget.
func (b Budgets) Of(profile string) Budget {
	for _, c := range []struct{ value, source string }{
		{b.env, SourceEnv},
		{b.file.Profiles[profile].Cache.Budget, SourceProfile},
		{b.file.Cache.Budget, SourceConfig},
	} {
		if c.value != "" {
			n, _ := parseBudget("", c.value) //nolint:errcheck // LoadFrom and ParseFile checked it
			return Budget{Bytes: n, Source: c.source}
		}
	}
	return Budget{Bytes: DefaultCacheBudget, Source: SourceDefault}
}

// budgetsOf checks RMCTL_CACHE_BUDGET and keeps it with the file.
func budgetsOf(env map[string]string, f File) (Budgets, error) {
	if _, err := parseBudget(EnvCacheBudget, env[EnvCacheBudget]); err != nil {
		return Budgets{}, err
	}
	return Budgets{env: env[EnvCacheBudget], file: f}, nil
}
