// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"bytes"
	"errors"
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type failingPasswordRandomSource struct{}

func (failingPasswordRandomSource) Read([]byte) (int, error) {
	return 0, errors.New("synthetic random source failed")
}

func TestInitialPasswordAcceptsDeterministicRandomSource(t *testing.T) {
	syntheticBytes := make([]byte, 256)
	for index := range syntheticBytes {
		syntheticBytes[index] = byte(index)
	}
	randomSource := bytes.NewReader(syntheticBytes)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{}, withInitialPasswordRandomSource(randomSource))
	if err != nil || client.initialPasswordRandomSource != randomSource {
		t.Fatalf("client did not retain the test random source: %v", err)
	}
	first, err := generateInitialPassword(bytes.NewReader(syntheticBytes))
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateInitialPassword(bytes.NewReader(syntheticBytes))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != initialPasswordLength {
		t.Fatalf("deterministic source produced inconsistent passwords")
	}
	for _, characterClass := range initialPasswordCharacterClasses {
		if !bytes.ContainsAny([]byte(first), characterClass) {
			t.Fatal("password omitted a required character class")
		}
	}
	if _, err := generateInitialPassword(failingPasswordRandomSource{}); err == nil {
		t.Fatal("random source failure was ignored")
	}
}
