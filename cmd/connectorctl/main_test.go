// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogLoadsRepositoryManifests(t *testing.T) {
	manifests, err := catalog(filepath.Join("..", "..", "connectors"))
	require.NoError(t, err)
	require.Len(t, manifests, 2)
	require.Equal(t, "http", manifests[0].Metadata.Name)
	require.Equal(t, "openai", manifests[1].Metadata.Name)
	require.Equal(t, []string{"structured", "text"}, manifests[1].Spec.Operations[0].Progress)
}

func TestGeneratedConnectorsAreCurrent(t *testing.T) {
	for _, manifest := range []string{"http", "openai"} {
		path := filepath.Join("..", "..", "connectors", manifest, "connector.yaml")
		require.NoError(t, generate([]string{"--check", path}))
	}
}

func TestGenerateCheckDetectsDrift(t *testing.T) {
	source := filepath.Join("..", "..", "connectors", "openai", "connector.yaml")
	content, err := os.ReadFile(source)
	require.NoError(t, err)
	directory := t.TempDir()
	manifest := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifest, content, 0o600))
	require.NoError(t, generate([]string{manifest}))
	require.NoError(t, generate([]string{"--check", manifest}))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "zz_generated_connector.go"), []byte("stale"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifest}), "stale")
}
