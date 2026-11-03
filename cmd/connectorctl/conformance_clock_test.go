// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConformanceClock(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "conformance", "runners", "clock_cases.json"))
	require.NoError(t, err)
	var cases struct {
		Valid   []string          `json:"valid"`
		Invalid []json.RawMessage `json:"invalid"`
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, value := range cases.Valid {
		t.Run(value, func(t *testing.T) {
			require.True(t, isConformanceClock(value))
		})
	}
	for _, raw := range cases.Invalid {
		t.Run(string(raw), func(t *testing.T) {
			var value string
			if json.Unmarshal(raw, &value) == nil {
				require.False(t, isConformanceClock(value))
			}
		})
	}
}

func isConformanceClock(value string) bool {
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d{1,3})?Z$`).MatchString(value) || strings.HasPrefix(value, "0000") {
		return false
	}
	fixed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return false
	}
	layout := "2006-01-02T15:04:05"
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		layout += "." + strings.Repeat("0", len(value)-dot-2)
	}
	return fixed.UTC().Format(layout+"Z") == value
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
			first["clock"] = testCase.clock
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
				require.Contains(t, string(output), "invalid clock timestamp")
			}
		})
	}
}
