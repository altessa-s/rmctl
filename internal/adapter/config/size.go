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

// EnvCacheBudget is the cache budget over the configuration file.
const EnvCacheBudget = "RMCTL_CACHE_BUDGET"

// MinCacheBudget is the smallest budget a profile's cache takes: a cache
// smaller than one page of a notebook keeps nothing worth keeping.
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
	return int64(bytes), true
}

// parseBudget reads a cache budget: a size of at least MinCacheBudget, ""
// for none. There is no unlimited budget, and 0 does not turn the cache
// off (--no-cache does): a cache is always bounded.
func parseBudget(key, value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	n, ok := ParseSize(value)
	if !ok || n < MinCacheBudget {
		return 0, fmt.Errorf("%w: %s = %q: a budget is a size such as \"512MiB\" or \"4GiB\", at least 1MiB", ErrConfig, key, value)
	}
	return n, nil
}

// budgetOf chooses a profile's cache budget: RMCTL_CACHE_BUDGET, then the
// profile's [profiles.<name>.cache], then [cache]; 0 is none set.
func budgetOf(env map[string]string, f File, profile string) (int64, error) {
	if v := env[EnvCacheBudget]; v != "" {
		return parseBudget(EnvCacheBudget, v)
	}
	if v := f.Profiles[profile].Cache.Budget; v != "" {
		return parseBudget("profiles."+profile+".cache.budget", v)
	}
	return parseBudget("cache.budget", f.Cache.Budget)
}
