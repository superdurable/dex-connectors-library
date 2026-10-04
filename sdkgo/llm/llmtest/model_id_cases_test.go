// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmtest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

func TestModelIDCasesMatchTheGoRules(t *testing.T) {
	var file modelIDCasesFile
	require.NoError(t, json.Unmarshal(modelIDCasesJSON, &file))
	require.Equal(t, "connectors.dex.dev/model-id-cases/v1", file.SchemaVersion)
	for _, rule := range []llm.ModelIDRule{llm.ModelIDRuleBody, llm.ModelIDRulePathSegment} {
		require.NotEmpty(t, file.Rules[rule.String()], "every rule is described for the TypeScript mirror")
		cases, err := modelIDCasesForRule(rule)
		require.NoError(t, err)
		for _, modelCase := range cases {
			t.Run(rule.String()+"/"+modelCase.Name, func(t *testing.T) {
				modelID, err := rule.ValidateModelID(modelCase.Input)
				if !modelCase.IsValid {
					require.Error(t, err)
					require.Empty(t, modelCase.ModelID, "an invalid case has no model ID")
					return
				}
				require.NoError(t, err)
				require.Equal(t, modelCase.ModelID, modelID)
			})
		}
	}
	_, err := llm.ModelIDRule(0).ValidateModelID("anything")
	require.Error(t, err)
}
