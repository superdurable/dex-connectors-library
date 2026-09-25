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

func TestCatalogLoadsRepositoryDirectoryRegistry(t *testing.T) {
	registry := filepath.Join("..", "..", "connectors.yaml")
	entries, err := loadConnectorDirectoryEntries(registry)
	require.NoError(t, err)
	require.Len(t, entries, 6)
	require.Equal(t, []string{"github", "gmail", "google-sheets", "linkedin", "openai", "slack"}, []string{
		entries[0].Manifest.Metadata.Name, entries[1].Manifest.Metadata.Name, entries[2].Manifest.Metadata.Name,
		entries[3].Manifest.Metadata.Name, entries[4].Manifest.Metadata.Name, entries[5].Manifest.Metadata.Name,
	})
	require.Equal(t, []string{"v0.5.0", "v0.8.0", "v0.5.0", "v0.5.0", "v0.5.0", "v0.7.0"}, []string{
		entries[0].Manifest.Metadata.Version, entries[1].Manifest.Metadata.Version, entries[2].Manifest.Metadata.Version,
		entries[3].Manifest.Metadata.Version, entries[4].Manifest.Metadata.Version, entries[5].Manifest.Metadata.Version,
	})
}

func TestCatalogCommandWritesDeterministicYAML(t *testing.T) {
	registry := filepath.Join("..", "..", "connectors.yaml")
	require.NoError(t, catalogCommand([]string{"--check", "--registry", registry}))
	first := filepath.Join(t.TempDir(), "catalog.yaml")
	second := filepath.Join(t.TempDir(), "catalog.yaml")
	require.NoError(t, catalogCommand([]string{"--registry", registry, "--output", first}))
	require.NoError(t, catalogCommand([]string{"--registry", registry, "--output", second}))
	firstContent, err := os.ReadFile(first)
	require.NoError(t, err)
	secondContent, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, firstContent, secondContent)
	require.Contains(t, string(firstContent), "apiVersion: connectors.dex.dev/catalog/v1alpha1")
	require.Contains(t, string(firstContent), "directory: connectors/google/gmail")
	require.Contains(t, string(firstContent), "version: v0.8.0")
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
			"--version", "v0.5.0",
			"--tag", "connectors/openai/v0.5.0",
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
	require.Contains(t, string(firstContent), `"version": "v0.5.0"`)
	require.Contains(t, string(firstContent), `"tag": "connectors/openai/v0.5.0"`)
	digest, err := os.ReadFile(firstDigest)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x  connector-release.json\n", sha256.Sum256(firstContent)), string(digest))
	mismatchedArguments := arguments(filepath.Join(directory, "mismatch.json"), filepath.Join(directory, "mismatch.json.sha256"))
	mismatchedArguments[5] = "v0.5.1"
	require.ErrorContains(t, releaseArtifact(mismatchedArguments), "does not match manifest version")
}

func TestGeneratedConnectorsAreCurrent(t *testing.T) {
	for _, manifest := range []string{"github", "google/gmail", "google/spreadsheet", "linkedin", "openai", "slack"} {
		path := filepath.Join("..", "..", "connectors", filepath.FromSlash(manifest), "connector.yaml")
		require.NoError(t, generate([]string{"--check", path}))
	}
}

func TestStudioUIArtifactIsDeterministicAndIncludedInRelease(t *testing.T) {
	directory := t.TempDir()
	uiRoot := filepath.Join(directory, "dist")
	require.NoError(t, os.MkdirAll(uiRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "index.html"), []byte("<main>Sheets</main>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "icon.svg"), []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"), 0o600))
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	fixture = []byte(strings.Replace(string(fixture), "  operations:\n", `  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.1.0 <0.2.0"
      backendCapabilities: [configuration.write]
      mockScenarios: [not-configured, ready]
      icon: icon.svg
  operations:
`, 1))
	manifest := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifest, fixture, 0o600))
	first := filepath.Join(directory, "connector-ui.tgz")
	firstDigest := first + ".sha256"
	second := filepath.Join(directory, "connector-ui-copy.tgz")
	secondDigest := second + ".sha256"
	require.NoError(t, uiArtifact([]string{"--manifest", manifest, "--ui-root", uiRoot, "--output", first, "--digest-output", firstDigest}))
	require.NoError(t, uiArtifact([]string{"--manifest", manifest, "--ui-root", uiRoot, "--output", second, "--digest-output", secondDigest}))
	firstBytes, err := os.ReadFile(first)
	require.NoError(t, err)
	secondBytes, err := os.ReadFile(second)
	require.NoError(t, err)
	require.Equal(t, firstBytes, secondBytes)

	release := filepath.Join(directory, "connector-release.json")
	releaseDigest := release + ".sha256"
	require.NoError(t, releaseArtifact([]string{
		"--manifest", manifest,
		"--module-path", "github.com/superdurable/dex-connectors-library/connectors/google-fixture",
		"--version", "v0.1.0", "--tag", "connectors/google-fixture/v0.1.0",
		"--source-sha", strings.Repeat("a", 40), "--ui-artifact", first, "--ui-digest", firstDigest,
		"--output", release, "--digest-output", releaseDigest,
	}))
	releaseBytes, err := os.ReadFile(release)
	require.NoError(t, err)
	require.Contains(t, string(releaseBytes), `"artifact": "connector-ui.tgz"`)
	require.Contains(t, string(releaseBytes), `"entrypoint": "index.html"`)
}

func TestStudioUIArtifactRejectsActiveSVG(t *testing.T) {
	directory := t.TempDir()
	uiRoot := filepath.Join(directory, "dist")
	require.NoError(t, os.MkdirAll(uiRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "index.html"), []byte("<main>Fixture</main>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(uiRoot, "icon.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><image onerror = "alert(1)"/></svg>`), 0o600))
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	fixture = []byte(strings.Replace(string(fixture), "  operations:\n", `  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.1.0 <0.2.0"
      backendCapabilities: [configuration.write]
      mockScenarios: [ready]
      icon: icon.svg
  operations:
`, 1))
	manifest := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifest, fixture, 0o600))
	err = uiArtifact([]string{
		"--manifest", manifest, "--ui-root", uiRoot,
		"--output", filepath.Join(directory, "ui.tgz"),
		"--digest-output", filepath.Join(directory, "ui.tgz.sha256"),
	})
	require.ErrorContains(t, err, "prohibited active content")
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
