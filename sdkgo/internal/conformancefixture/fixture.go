// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package conformancefixture checks golden JSON produced by sdkgo wire types.
package conformancefixture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	directory := fixtureDirectory(t)
	path := filepath.Join(directory, name)
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

// CheckFixtureSet rejects JSON files without a generator.
func CheckFixtureSet(t *testing.T, expected []string) {
	t.Helper()
	wanted := make(map[string]bool, len(expected))
	for _, name := range expected {
		wanted[name] = true
	}
	entries, err := os.ReadDir(fixtureDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") && !wanted[entry.Name()] {
			t.Fatalf("orphan conformance fixture %s", entry.Name())
		}
	}
	for name := range wanted {
		if _, err := os.Stat(filepath.Join(fixtureDirectory(t), name)); err != nil {
			t.Fatalf("missing conformance fixture %s: %v", name, err)
		}
	}
}

func fixtureDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		moduleFile := filepath.Join(directory, "go.mod")
		contents, readErr := os.ReadFile(moduleFile)
		if readErr == nil && strings.Contains(string(contents), "module github.com/superdurable/dex-connectors-library/sdkgo") {
			fixtures := filepath.Join(filepath.Dir(directory), "conformance")
			if info, statErr := os.Stat(fixtures); statErr == nil && info.IsDir() {
				return fixtures
			}
			t.Fatalf("conformance fixtures are absent beside sdkgo module at %s; run tests from a repository checkout", fixtures)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("cannot locate sdkgo module root for conformance fixtures")
		}
		directory = parent
	}
}
