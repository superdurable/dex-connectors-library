// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	answerduplicate "github.com/superdurable/dex-connectors-library/connectors/intercom/examples/answer-duplicate-conversation/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestNewLoggerUsesLogLevelAndWarnsAboutAnInvalidOne(t *testing.T) {
	var output bytes.Buffer
	require.True(t, newLogger(&output, "debug").Enabled(context.Background(), slog.LevelDebug))
	require.False(t, newLogger(&output, "").Enabled(context.Background(), slog.LevelDebug))
	newLogger(&output, "verbose")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error")
}

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("INTERCOM_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("INTERCOM_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("INTERCOM_EXAMPLE_MISSING", "fallback"))
}

func TestLocalAPIOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Empty(t, localAPIOptions(logger))
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:8899")
	require.Len(t, localAPIOptions(logger), 1)
}

func TestReplyConfigurationLoadsTheSavedAdminOrAnEmptyOne(t *testing.T) {
	reference := answerduplicate.ReplyConfigurationRef()
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{{
		ConnectorID: reference.ConnectorID, ConnectionName: reference.ConnectionName, OperationID: reference.OperationID,
		FlowType: reference.FlowType, StepType: reference.StepType, Configuration: json.RawMessage(`{"adminId":"` + fakeAdminID + `"}`),
	}}}
	var output bytes.Buffer
	loaded, err := loadReplyConfiguration(configuration, slog.New(slog.NewTextHandler(&output, nil)))
	require.NoError(t, err)
	require.Equal(t, fakeAdminID, loaded.Value.AdminID)
	require.Empty(t, output.String())

	loaded, err = loadReplyConfiguration(projectconfig.Configuration{}, slog.New(slog.NewTextHandler(&output, nil)))
	require.NoError(t, err)
	require.Equal(t, reference, loaded.Reference)
	require.Empty(t, loaded.Value.AdminID, "an unsaved adminPicker fails each Flow with guidance instead of stopping the Worker")
	require.Contains(t, output.String(), "the replying admin is not configured")
}
