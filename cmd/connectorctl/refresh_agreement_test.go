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

func TestGenerateWritesChecksAndRemovesTheRefreshAgreementTest(t *testing.T) {
	directory := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "token-grants.yaml"))
	require.NoError(t, err)
	manifestPath := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, fixture, 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "go"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go", "go.mod"), []byte("module example.com/tokengrants\n\ngo 1.24\n"), 0o600))
	agreementPath := filepath.Join(directory, "go", codegen.RefreshAgreementTestOutputFile)

	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated connector is missing")
	require.NoError(t, generate([]string{manifestPath}))
	require.FileExists(t, agreementPath)
	require.NoError(t, generate([]string{"--check", manifestPath}))

	require.NoError(t, os.MkdirAll(filepath.Join(directory, "go", "testdata"), 0o755))
	samplesPath := filepath.Join(directory, "go", "testdata", codegen.RefreshAgreementSamplesFile)
	require.NoError(t, os.WriteFile(samplesPath, []byte(`{"*": {"response": {"instance_url": "https://acme.example.com"}}}`), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale", "samples are generation input")
	require.NoError(t, os.WriteFile(samplesPath, []byte(`{"*": {"responses": {}}}`), 0o600))
	require.ErrorContains(t, generate([]string{manifestPath}), `unknown field "responses"`)
	require.NoError(t, os.Remove(samplesPath))

	withoutGrants, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, withoutGrants, 0o600))
	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, agreementPath, "a manifest without token grants keeps no agreement test")
	require.NoError(t, os.WriteFile(agreementPath, []byte("package tokengrants_test\n"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "credential refresh agreement test is stale")
}
