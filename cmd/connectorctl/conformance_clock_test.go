// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratedConformanceRecorderReportsBodyOnBodylessRequest keeps a stale requestBody on a GET, whose request body is nil.
func TestGeneratedConformanceRecorderReportsBodyOnBodylessRequest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "conformance", "connectors", "airtable", "scenarios.json"))
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	var selected map[string]any
	for _, entry := range document["scenarios"].([]any) {
		scenario := entry.(map[string]any)
		if scenario["name"] == "getRecord-success" {
			selected = scenario
		}
	}
	require.NotNil(t, selected, "the airtable fixture keeps its getRecord-success GET scenario")
	exchange := selected["exchanges"].([]any)[0].(map[string]any)
	require.Equal(t, "GET", exchange["method"])
	exchange["requestBody"] = map[string]any{"stale": true}
	document["scenarios"] = []any{selected}
	fixture, err := json.Marshal(document)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "scenarios.json")
	require.NoError(t, os.WriteFile(path, fixture, 0600))
	command := exec.Command("go", "test", ".", "-run", "^TestConnectorConformance$/^getRecord-success$", "-count=1")
	command.Dir = filepath.Join("..", "..", "connectors", "airtable", "go")
	command.Env = append(os.Environ(), "CONNECTOR_CONFORMANCE_FIXTURE="+path, "GOWORK=off")
	output, err := command.CombinedOutput()
	require.Error(t, err, string(output))
	require.NotContains(t, string(output), "panic", "a nil GET body is read as empty")
	require.Contains(t, string(output), "body mismatch")
}

func TestGeneratedConformanceClockFixture(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "conformance", "connectors", "airtable", "scenarios.json")
	data, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	scenarios := document["scenarios"].([]any)
	first := scenarios[0].(map[string]any)
	for _, testCase := range []struct {
		name  string
		clock any
		valid bool
	}{
		{"valid", "2026-10-26T12:00:00.120Z", true},
		{"null", nil, false},
		{"empty", "", false},
		{"date", "2026-10-26", false},
		{"number", 123, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			first["fixedClock"] = testCase.clock
			fixture, err := json.Marshal(document)
			require.NoError(t, err)
			path := filepath.Join(t.TempDir(), "scenarios.json")
			require.NoError(t, os.WriteFile(path, fixture, 0600))
			command := exec.Command("go", "test", ".", "-run", "^TestConnectorConformance$", "-count=1")
			command.Dir = filepath.Join("..", "..", "connectors", "airtable", "go")
			command.Env = append(os.Environ(), "CONNECTOR_CONFORMANCE_FIXTURE="+path, "GOWORK=off")
			output, err := command.CombinedOutput()
			if testCase.valid {
				require.NoError(t, err, string(output))
			} else {
				require.Error(t, err, string(output))
				if testCase.name != "number" {
					require.Contains(t, string(output), "invalid fixed clock")
				}
			}
		})
	}
}
