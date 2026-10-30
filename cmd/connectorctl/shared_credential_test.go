// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
)

func TestGenerateWritesChecksAndRemovesTheSharedCredentialTest(t *testing.T) {
	directory := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "token-grants.yaml"))
	require.NoError(t, err)
	manifestPath := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, fixture, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "go"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go", "go.mod"), []byte("module example.com/tokengrants\n\ngo 1.24\n"), 0o600))
	sharedCredentialPath := filepath.Join(directory, "go", codegen.SharedCredentialTestOutputFile)

	require.NoError(t, generate([]string{manifestPath}))
	require.FileExists(t, sharedCredentialPath)
	require.NoError(t, generate([]string{"--check", manifestPath}))
	require.NoError(t, os.WriteFile(sharedCredentialPath, []byte("package tokengrants_test\n"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale")

	keepsNothing, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, keepsNothing, 0o600))
	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, sharedCredentialPath, "a manifest whose host keeps no credentials needs no shared credential test")
	require.NoError(t, os.WriteFile(sharedCredentialPath, []byte("package tokengrants_test\n"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "shared credential test is stale")
}
