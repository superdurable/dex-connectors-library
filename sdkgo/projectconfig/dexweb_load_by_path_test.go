// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestLoadDexWebConnectionsFileMatchesEnvironmentLoad(t *testing.T) {
	directory := t.TempDir()
	future := time.Now().Add(time.Hour)
	path := writeDexWebFiles(t, directory, "access-1", &future)
	t.Setenv(projectconfig.LocalConnectionsFileEnvironment, "")

	byPath, err := projectconfig.LoadDexWebConnectionsFile(context.Background(), path)
	require.NoError(t, err)
	byEnvironment := loadLocal(t, path)
	require.Equal(t, byEnvironment.Configuration, byPath.Configuration)

	var settings localSettings
	require.NoError(t, byPath.Configuration.DecodeConnectionConfiguration(localKey, &settings))
	require.Equal(t, "acme", settings.Workspace)
	var operation map[string]string
	require.NoError(t, byPath.Configuration.DecodeOperationConfiguration("example", "main", "send", "Notify", "Send", &operation))
	require.Equal(t, "fast", operation["model"])
	credentials, err := localResolver(t, byPath).Resolve(context.Background())
	require.NoError(t, err)
	require.Equal(t, localCredentials{AccessToken: "access-1", RefreshToken: "refresh-1"}, credentials)
}

func TestLoadDexWebConnectionsFileIgnoresProjectEnvironment(t *testing.T) {
	directory := t.TempDir()
	future := time.Now().Add(time.Hour)
	path := writeDexWebFiles(t, directory, "access-1", &future)
	other := filepath.Join(t.TempDir(), "missing.json")
	t.Setenv(projectconfig.LocalConnectionsFileEnvironment, other)
	t.Setenv("DEX_PROJECT_ID", "project-1")

	project, err := projectconfig.LoadDexWebConnectionsFile(context.Background(), path)
	require.NoError(t, err)
	credentials, err := localResolver(t, project).Resolve(context.Background())
	require.NoError(t, err)
	require.Equal(t, "access-1", credentials.AccessToken)
}

func TestLoadDexWebConnectionsFileResolvesRelativePath(t *testing.T) {
	directory := t.TempDir()
	future := time.Now().Add(time.Hour)
	writeDexWebFiles(t, directory, "access-1", &future)
	t.Chdir(directory)

	project, err := projectconfig.LoadDexWebConnectionsFile(context.Background(), "connections.json")
	require.NoError(t, err)
	credentials, err := localResolver(t, project).Resolve(context.Background())
	require.NoError(t, err)
	require.Equal(t, "access-1", credentials.AccessToken)
}

func TestLoadDexWebConnectionsFileRejectsInvalidPaths(t *testing.T) {
	_, err := projectconfig.LoadDexWebConnectionsFile(context.Background(), "")
	require.ErrorContains(t, err, "path is required")
	_, err = projectconfig.LoadDexWebConnectionsFile(context.Background(), "  ")
	require.ErrorContains(t, err, "path is required")

	_, err = projectconfig.LoadDexWebConnectionsFile(context.Background(), filepath.Join(t.TempDir(), "connections.json"))
	require.Error(t, err)

	directory := t.TempDir()
	future := time.Now().Add(time.Hour)
	path := writeDexWebFiles(t, directory, "access-1", &future)
	require.NoError(t, os.Chmod(path, 0o644))
	_, err = projectconfig.LoadDexWebConnectionsFile(context.Background(), path)
	require.Error(t, err)
}
