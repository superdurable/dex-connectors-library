// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	escalateblocked "github.com/superdurable/dex-connectors-library/connectors/clickup/examples/escalate-blocked-task/flow"
)

func TestNewLoggerUsesLogLevelAndWarnsAboutAnInvalidOne(t *testing.T) {
	var output bytes.Buffer
	require.True(t, newLogger(&output, "debug").Enabled(context.Background(), slog.LevelDebug))
	require.False(t, newLogger(&output, "").Enabled(context.Background(), slog.LevelDebug))
	newLogger(&output, "verbose")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error")
}

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("CLICKUP_EXAMPLE_VALUE", " configured ")
	require.Equal(t, "configured", environmentOr("CLICKUP_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("CLICKUP_EXAMPLE_MISSING", "fallback"))
}

func TestLocalAPIOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	t.Setenv(localAPIBaseURLEnvironmentVariable, "")
	require.Empty(t, localAPIOptions(logger))
	t.Setenv(localAPIBaseURLEnvironmentVariable, "http://127.0.0.1:8899")
	require.Len(t, localAPIOptions(logger), 1)
}

func TestEscalationSettingsComeFromTheEnvironment(t *testing.T) {
	t.Setenv("CLICKUP_WORKSPACE_ID", fakeWorkspaceID)
	t.Setenv("CLICKUP_ESCALATION_LIST_ID", fakeEscalationID)
	t.Setenv("CLICKUP_ESCALATION_MANAGER_EMAIL", fakeManagerEmail)
	t.Setenv("CLICKUP_BLOCKED_STATUS", "")
	settings, err := loadEscalationSettings()
	require.NoError(t, err)
	require.Equal(t, escalateblocked.EscalationSettings{
		WorkspaceID: fakeWorkspaceID, EscalationListID: fakeEscalationID, ManagerEmail: fakeManagerEmail, BlockedStatus: defaultBlockedStatus,
	}, settings)

	t.Setenv("CLICKUP_BLOCKED_STATUS", "on hold")
	settings, err = loadEscalationSettings()
	require.NoError(t, err)
	require.Equal(t, "on hold", settings.BlockedStatus)

	t.Setenv("CLICKUP_ESCALATION_LIST_ID", "")
	_, err = loadEscalationSettings()
	require.ErrorContains(t, err, "CLICKUP_ESCALATION_LIST_ID")
}
