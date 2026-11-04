// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package porttest holds doubles of the ports for the use case tests: an
// in-memory cloud ([Cloud]) with builders for a library ([Doc]), a file
// system in memory ([MemFiles]), predictable identifiers ([IDs]), a fake
// clock, a blob cache ([MemCache]), a PDF inspector ([PDF]) and a renderer
// ([Renderer]).
//
// Production code never imports it; depguard keeps it out.
package porttest
