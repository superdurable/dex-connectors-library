// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	submissionintake "github.com/superdurable/dex-connectors-library/connectors/notion/examples/submission-intake/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("NOTION_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("NOTION_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("NOTION_EXAMPLE_MISSING", "fallback"))
}

func TestNewLoggerFallsBackToInfoForAnInvalidLevel(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "verbose")
	logger.Debug("hidden")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error")
	require.NotContains(t, output.String(), "hidden")
}

// TestTheDexWebConnectionRecordLoadsTheExampleConnection proves the record Dex Web writes for the
// example's static connection name opens a Notion connection without exposing the token.
func TestTheDexWebConnectionRecordLoadsTheExampleConnection(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": notion.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/notion",
			"moduleVersion": "v0.1.0", "provider": "notion", "connectionName": submissionintake.ConnectionName,
			"configuration": map[string]any{}, "credentials": map[string]any{"api_token": "ntn_example_token"},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	connection, err := notion.NewLocalConnection(store, submissionintake.ConnectionName)
	require.NoError(t, err)
	require.NotContains(t, connection.String(), "ntn_example_token")
}

func TestWaitForDexServerRetriesUntilTheServerAnswers(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "info")
	calls := 0
	healthCheck := func(context.Context) (dex.HealthInfo, error) {
		calls++
		if calls < 2 {
			return dex.HealthInfo{}, errors.New("connection refused")
		}
		return dex.HealthInfo{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, waitForDexServer(ctx, healthCheck, logger))
	require.Equal(t, 2, calls)
	require.True(t, strings.Contains(output.String(), "dex server unavailable; retrying"))
	require.True(t, strings.Contains(output.String(), "dex server available after retry"))
}
