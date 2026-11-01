// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// isMaskedKey reports the JSON fields that hold the user's own words. The
// list names what to hide, not what to show: an allowlist would also hide
// every field rmctl does not model, which the diagnostic output is there to
// show.
func isMaskedKey(key string) bool {
	switch key {
	case "visibleName", "source", "tags":
		return true
	default:
		return false
	}
}

// maxExtension is the longest extension MaskName keeps visible.
const maxExtension = 8

// MaskName is a name reduced to its length: <name:N>, or <name:N>.ext when
// it ends in an extension of one to eight ASCII letters and digits, N being
// the characters before it. An empty name stays empty.
func MaskName(s string) string {
	if s == "" {
		return ""
	}
	if dot := strings.LastIndexByte(s, '.'); dot > 0 {
		ext := s[dot+1:]
		if len(ext) >= 1 && len(ext) <= maxExtension && isAlnumASCII(ext) {
			return fmt.Sprintf("<name:%d>.%s", utf8.RuneCountInString(s[:dot]), ext)
		}
	}
	return fmt.Sprintf("<name:%d>", utf8.RuneCountInString(s))
}

func isAlnumASCII(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// MaskJSON masks every string under the keys visibleName, source and tags,
// at any depth, and pretty-prints the result with sorted keys. What is not
// JSON comes back unchanged.
func MaskJSON(data []byte) []byte {
	var v any
	// Numbers stay the digits that arrived: through float64 a millisecond
	// stamp or a large size would print differently from the file.
	keepNumbers := json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, out *any) error {
		if dec.PeekKind() != '0' {
			return errors.ErrUnsupported
		}
		raw, err := dec.ReadValue()
		if err != nil {
			return err
		}
		*out = raw.Clone()
		return nil
	}))
	if err := json.Unmarshal(data, &v, keepNumbers); err != nil {
		return data
	}
	masked := mask(v, false)
	var out bytes.Buffer
	if err := json.MarshalWrite(&out, masked, jsontext.WithIndent("  "), json.Deterministic(true)); err != nil {
		return data
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func mask(v any, under bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = mask(child, under || isMaskedKey(k))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = mask(child, under)
		}
		return out
	case string:
		if under {
			return MaskName(t)
		}
		return t
	default:
		return v
	}
}
