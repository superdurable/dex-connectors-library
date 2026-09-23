// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestReleaseWorkflowIsGeneratedFromSortedCatalog(t *testing.T) {
	root := filepath.Join("..", "..", "connectors")
	entries, err := connectorReleaseCatalog(root)
	require.NoError(t, err)
	require.Equal(t, []string{"http", "openai"}, []string{entries[0].Slug, entries[1].Slug})
	output := filepath.Join(t.TempDir(), "release-connector.yml")
	require.NoError(t, releaseWorkflow([]string{root, output}))
	require.NoError(t, releaseWorkflow([]string{"--check", root, output}))
	content, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Contains(t, string(content), "          - http\n          - openai\n")
	require.Contains(t, string(content), "connectors/${{ inputs.connector }}")
	require.NoError(t, os.WriteFile(output, []byte("stale"), 0o600))
	require.ErrorContains(t, releaseWorkflow([]string{"--check", root, output}), "stale")
}

func TestReleaseArtifactIsDeterministicAndVersioned(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "connector-release.json")
	firstDigest := first + ".sha256"
	second := filepath.Join(directory, "connector-release-copy.json")
	secondDigest := second + ".sha256"
	manifest := filepath.Join("..", "..", "connectors", "openai", "connector.yaml")
	arguments := func(output, digest string) []string {
		return []string{
			"--manifest", manifest,
			"--module-path", "github.com/superdurable/dex-connectors-library/connectors/openai",
			"--version", "v0.1.0",
			"--tag", "connectors/openai/v0.1.0",
			"--source-sha", strings.Repeat("a", 40),
			"--output", output,
			"--digest-output", digest,
		}
	}
	require.NoError(t, releaseArtifact(arguments(first, firstDigest)))
	require.NoError(t, releaseArtifact(arguments(second, secondDigest)))
	firstContent, err := os.ReadFile(first)
	require.NoError(t, err)
	secondContent, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, firstContent, secondContent)
	require.Contains(t, string(firstContent), `"version": "v0.1.0"`)
	require.Contains(t, string(firstContent), `"tag": "connectors/openai/v0.1.0"`)
	digest, err := os.ReadFile(firstDigest)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x  connector-release.json\n", sha256.Sum256(firstContent)), string(digest))
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
