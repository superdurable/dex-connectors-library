// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
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
	provider := newFakeIntercom(t)
	setup := newExampleSetup(t, provider, "127.0.0.1:1")
	store, err := localconfig.LoadFile(setup.configPath)
	require.NoError(t, err)
	var output bytes.Buffer
	loaded, err := loadReplyConfiguration(store, slog.New(slog.NewTextHandler(&output, nil)))
	require.NoError(t, err)
	require.Equal(t, fakeAdminID, loaded.Value.AdminID)
	require.Empty(t, output.String())

	unconfigured := newEmptyUseConfigurationStore(t, setup.configPath)
	loaded, err = loadReplyConfiguration(unconfigured, slog.New(slog.NewTextHandler(&output, nil)))
	require.NoError(t, err)
	require.Empty(t, loaded.Value.AdminID, "an unsaved adminPicker fails each Flow with guidance instead of stopping the Worker")
	require.Contains(t, output.String(), "the replying admin is not configured")
}
