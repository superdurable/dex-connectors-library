// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type failingPasswordRandomSource struct{}

func (failingPasswordRandomSource) Read([]byte) (int, error) {
	return 0, errors.New("synthetic random source failed")
}

func TestInitialPasswordAcceptsDeterministicRandomSource(t *testing.T) {
	syntheticBytes := make([]byte, initialPasswordRandomBytes)
	for index := range syntheticBytes {
		syntheticBytes[index] = byte(index)
	}
	randomSource := bytes.NewReader(syntheticBytes)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{}, withInitialPasswordRandomSource(randomSource))
	if err != nil || client.initialPasswordRandomSource != randomSource {
		t.Fatalf("client did not retain the test random source: %v", err)
	}
	password, err := generateInitialPassword(bytes.NewReader(syntheticBytes))
	if err != nil {
		t.Fatal(err)
	}
	if password != base64.RawURLEncoding.EncodeToString(syntheticBytes) {
		t.Fatal("deterministic source produced another password")
	}
	if _, err := generateInitialPassword(failingPasswordRandomSource{}); err == nil {
		t.Fatal("random source failure was ignored")
	}
}
