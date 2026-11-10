// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// agentToolNameVectors is shared with connectorctl's lowerCamelToSnake test.
type agentToolNameVectors struct {
	Vectors []struct {
		OperationID string `json:"operationId"`
		SnakeCase   string `json:"snakeCase"`
	} `json:"vectors"`
}

func TestLowerCamelToSnakeMatchesSharedVectors(t *testing.T) {
	contents, err := os.ReadFile("testdata/agent_tool_names.json")
	require.NoError(t, err)
	var document agentToolNameVectors
	require.NoError(t, json.Unmarshal(contents, &document))
	require.NotEmpty(t, document.Vectors)
	for _, vector := range document.Vectors {
		require.Equal(t, vector.SnakeCase, lowerCamelToSnake(vector.OperationID), vector.OperationID)
	}
}
