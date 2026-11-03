// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSharedStringFormatCases(t *testing.T) {
	contents, err := os.ReadFile("textgentest/testdata/string_format_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name string `json:"name"`
			Format string `json:"format"`
			Value string `json:"value"`
			Valid bool `json:"valid"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			if actual := matchesStringFormat(testCase.Value, testCase.Format); actual != testCase.Valid {
				t.Errorf("matchesStringFormat(%q, %q) = %v, want %v", testCase.Value, testCase.Format, actual, testCase.Valid)
			}
		})
	}
}
