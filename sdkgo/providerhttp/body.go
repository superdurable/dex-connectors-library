// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package providerhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// MaxErrorBodyBytes is the size limit connectors use when reading a non-2xx
// response body for error tokens. A larger body is truncated, not rejected.
const MaxErrorBodyBytes = 64 << 10

// ErrBodyTooLarge reports that a body exceeded the limit passed to ReadBoundedBody.
var ErrBodyTooLarge = errors.New("provider response body exceeds its size limit")

var errorTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// ReadBoundedBody reads all of body when it holds at most maxBytes bytes. A
// longer body returns ErrBodyTooLarge after reading maxBytes+1 bytes, so memory
// stays bounded. Other errors are read failures from body, wrapped. A
// non-positive maxBytes accepts only an empty body.
func ReadBoundedBody(body io.Reader, maxBytes int64) ([]byte, error) {
	limit := max(maxBytes, 0)
	// A math.MaxInt64 limit cannot overflow into a negative read limit; no body reaches it.
	contents, err := io.ReadAll(io.LimitReader(body, min(limit, math.MaxInt64-1)+1))
	if err != nil {
		return nil, fmt.Errorf("read provider response body: %w", err)
	}
	if int64(len(contents)) > limit {
		return nil, ErrBodyTooLarge
	}
	return contents, nil
}

// ReadErrorTokens returns the machine-readable tokens found at the given
// RFC 6901 JSON pointers of a JSON error body, such as "/error/type" and
// "/error/code", in pointer order.
//
// A value becomes a token only when it is a JSON string or number whose text
// matches ^[A-Za-z0-9_.:-]{1,64}$, so provider message text, which contains
// spaces, is never returned. Missing pointers, other JSON types, non-matching
// values, and a body that is not JSON are skipped, so the result may be empty.
// Callers should read body with a limit such as MaxErrorBodyBytes.
func ReadErrorTokens(body []byte, pointers []string) []string {
	if len(pointers) == 0 || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document any
	if decoder.Decode(&document) != nil {
		return nil
	}
	var tokens []string
	for _, pointer := range pointers {
		value, found := resolveJSONPointer(document, pointer)
		if !found {
			continue
		}
		var text string
		switch typed := value.(type) {
		case string:
			text = typed
		case json.Number:
			text = typed.String()
		default:
			continue
		}
		if errorTokenPattern.MatchString(text) {
			tokens = append(tokens, text)
		}
	}
	return tokens
}

func resolveJSONPointer(document any, pointer string) (any, bool) {
	if pointer == "" {
		return document, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := document
	for _, escaped := range strings.Split(pointer[1:], "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(escaped, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, found := typed[segment]
			if !found {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) || strconv.Itoa(index) != segment {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}
