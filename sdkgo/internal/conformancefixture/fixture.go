// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package conformancefixture checks golden JSON produced by sdkgo wire types.
package conformancefixture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ExampleCallID is the derived identity shared by conformance fixtures.
const ExampleCallID = "5af8fafa-06ff-5b2d-bb13-04693809fabf"

// Check compares a generated fixture with its committed form, or updates it when requested.
func Check(t *testing.T, name string, value any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate conformance fixture source")
	}
	path := filepath.Join(filepath.Dir(source), "../../../conformance", name)
	if os.Getenv("UPDATE_CONFORMANCE_FIXTURES") == "1" {
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, encoded) {
		t.Fatalf("%s drifted; run UPDATE_CONFORMANCE_FIXTURES=1 GOWORK=off go test ./... from sdkgo", name)
	}
}
