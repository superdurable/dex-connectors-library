// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGoTimeJSONFixtureMatchesStandardLibrary(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("testdata", "go_time_json_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input        string `json:"input"`
		Accepted     bool   `json:"accepted"`
		Output       string `json:"output"`
		MarshalError bool   `json:"marshalError"`
	}
	if err := json.Unmarshal(contents, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 400 {
		t.Fatalf("expected generated boundary table, got %d cases", len(cases))
	}
	for _, testCase := range cases {
		t.Run(testCase.Input, func(t *testing.T) {
			input, err := json.Marshal(testCase.Input)
			if err != nil {
				t.Fatal(err)
			}
			var value time.Time
			decodeError := json.Unmarshal(input, &value)
			if (decodeError == nil) != testCase.Accepted {
				t.Fatalf("Go parse result changed: %v", decodeError)
			}
			if decodeError != nil {
				return
			}
			encoded, marshalError := json.Marshal(value)
			if (marshalError != nil) != testCase.MarshalError {
				t.Fatalf("Go marshal result changed: %v", marshalError)
			}
			if marshalError == nil && string(encoded) != `"`+testCase.Output+`"` {
				t.Fatalf("Go encoded %s, fixture wants %s", encoded, testCase.Output)
			}
		})
	}
}
