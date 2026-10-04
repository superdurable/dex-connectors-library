// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	submissionintake "github.com/superdurable/dex-connectors-library/connectors/notion/examples/submission-intake/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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

// TestTheDexWebConnectionSettingsLoadTheExampleConnection proves the settings Dex Web saves for the
// example's static connection name open a Notion connection without exposing the token.
func TestTheDexWebConnectionSettingsLoadTheExampleConnection(t *testing.T) {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: notion.ConnectorID, ConnectionName: submissionintake.ConnectionName,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/notion", Provider: "notion",
		Configuration: json.RawMessage(`{}`),
	}}}
	var config notion.Config
	require.NoError(t, configuration.DecodeConnectionConfiguration(
		projectconfig.ConnectionKey{ConnectorID: notion.ConnectorID, ConnectionName: submissionintake.ConnectionName}, &config))
	reference := sdkgo.ConnectionRef{Provider: "notion", Name: submissionintake.ConnectionName}
	client, err := notion.New(config, sdkgo.StaticCredentialProvider[notion.Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("ntn_example_token")},
	})
	require.NoError(t, err)
	connection, err := notion.NewConnection(client, reference)
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
