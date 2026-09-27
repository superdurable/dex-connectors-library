// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmtest

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

//go:embed testdata/model_id_cases.json
var modelIDCasesJSON []byte

// modelIDCase is one shared case; the TypeScript picker reads the same file.
type modelIDCase struct {
	Name    string `json:"name"`
	Rule    string `json:"rule"`
	Input   string `json:"input"`
	IsValid bool   `json:"valid"`
	ModelID string `json:"modelId"`
}

type modelIDCasesFile struct {
	SchemaVersion string            `json:"schemaVersion"`
	Rules         map[string]string `json:"rules"`
	Cases         []modelIDCase     `json:"cases"`
}

// modelIDCasesForRule returns the embedded cases that pin rule's behavior.
func modelIDCasesForRule(rule llm.ModelIDRule) ([]modelIDCase, error) {
	var file modelIDCasesFile
	if err := json.Unmarshal(modelIDCasesJSON, &file); err != nil {
		return nil, fmt.Errorf("decode model ID cases: %w", err)
	}
	var cases []modelIDCase
	for _, candidate := range file.Cases {
		if candidate.Rule == rule.String() {
			cases = append(cases, candidate)
		}
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("model ID cases have no entries for rule %q", rule)
	}
	return cases, nil
}
