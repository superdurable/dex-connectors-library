// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package canonicaljson encodes JSON with sorted object keys, no insignificant
// whitespace, and no HTML escaping, so equal values always have equal bytes.
package canonicaljson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Canonicalize re-encodes one JSON value canonically. Numbers keep their
// original text, because their meaning to a provider can depend on it. Go
// always escapes U+2028 and U+2029, so canonical text contains   and
//   for them. It returns an error when raw is not exactly one JSON value.
func Canonicalize(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON value: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decode JSON value: data follows the value")
	}
	return encode(value)
}

// Marshal encodes value with encoding/json and then canonicalizes the result.
func Marshal(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return Canonicalize(encoded)
}

// SHA256Hex returns the lowercase hexadecimal SHA-256 of contents.
func SHA256Hex(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func encode(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
