// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCatalogLoadsRepositoryDirectoryRegistry(t *testing.T) {
	registry := filepath.Join("..", "..", "connectors.yaml")
	entries, err := loadConnectorDirectoryEntries(registry)
	require.NoError(t, err)
	directories := registeredConnectorDirectories(t)
	require.Len(t, entries, len(directories))
	names := map[string]bool{}
	for index, entry := range entries {
		require.Equal(t, directories[index], entry.Directory)
		identity := readManifestIdentity(t, entry.Directory)
		require.Equal(t, identity.Name, entry.Manifest.Metadata.Name, entry.Directory)
		require.Equal(t, identity.Version, entry.Manifest.Metadata.Version, entry.Directory)
		require.Falsef(t, names[identity.Name], "duplicate connector ID %s", identity.Name)
		names[identity.Name] = true
	}
	require.True(t, names["github"] && names["slack"], "the registry lists the long-standing connectors")
}

func TestRegisteredOperationsKeepOnlyHappyPathBranchesRequired(t *testing.T) {
	happyBranchByOperation := map[string]string{
		"generateContent":          "generated",
		"generateText":             "generated",
		"getAuthenticatedProfile":  "profileLoaded",
		"listPublicRepositories":   "repositoriesLoaded",
		"listMergedPullRequests":   "listed",
		"listPullRequestFiles":     "listed",
		"listCommits":              "listed",
		"getMessage":               "read",
		"sendMessage":              "sent",
		"replyToMessage":           "sent",
		"getValues":                "read",
		"findRow":                  "found",
		"upsertRow":                "upserted",
		"createResponse":           "completed",
		"retrieveResponse":         "found",
		"listThreadMessages":       "read",
		"getThreadReply":           "found",
		"postChannelMessage":       "sent",
		"postThreadReply":          "sent",
		"createACHCheckoutSession": "created",
		"getCheckoutSession":       "found",
	}
	entries, err := loadConnectorDirectoryEntries(filepath.Join("..", "..", "connectors.yaml"))
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, entry := range entries {
		for _, operation := range entry.Manifest.Spec.Operations {
			expected, ok := happyBranchByOperation[operation.Name]
			require.Truef(t, ok, "unexpected operation %s", operation.Name)
			seen[operation.Name] = true
			required := []string{}
			for _, branch := range operation.Branches {
				if !branch.Optional {
					required = append(required, branch.ID)
				}
			}
			require.Equal(t, []string{expected}, required, operation.Name)
			switch operation.Name {
			case "createResponse":
				require.Equal(t, "sync", operation.Execution.Durability)
				require.Equal(t, "150s", operation.Execution.ExecuteMethodTimeout)
			case "generateContent":
				// A non-streaming LLM generation is long and silent until the provider answers.
				require.Equal(t, "sync", operation.Execution.Durability)
				require.Equal(t, "300s", operation.Execution.ExecuteMethodTimeout)
				require.Equal(t, "300s", operation.Execution.HeartbeatTimeout)
			case "generateText":
				// Every text-generation connector shares the sdkgo/llm contract: a sync Query whose heartbeat
				// timeout covers the pipeline's 5-second heartbeat and whose Execute fits under 30 minutes.
				require.Equalf(t, "sync", operation.Execution.Durability, entry.Directory)
				heartbeatTimeout, err := time.ParseDuration(operation.Execution.HeartbeatTimeout)
				require.NoErrorf(t, err, entry.Directory)
				require.GreaterOrEqualf(t, heartbeatTimeout, 10*time.Second, entry.Directory)
				executeTimeout, err := time.ParseDuration(operation.Execution.ExecuteMethodTimeout)
				require.NoErrorf(t, err, entry.Directory)
				require.Greaterf(t, executeTimeout, heartbeatTimeout, entry.Directory)
				require.LessOrEqualf(t, executeTimeout, 30*time.Minute, entry.Directory)
			case "retrieveResponse":
				require.Equal(t, "async", operation.Execution.Durability)
				require.Equal(t, "30s", operation.Execution.ExecuteMethodTimeout)
			default:
				require.Equalf(t, "async", operation.Execution.Durability, operation.Name)
			}
		}
	}
	require.Len(t, seen, len(happyBranchByOperation))
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
	require.Contains(t, string(firstContent), "version: v0.10.2")
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
			"--version", "v0.6.0",
			"--tag", "connectors/openai/v0.6.0",
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
	require.Contains(t, string(firstContent), `"version": "v0.6.0"`)
	require.Contains(t, string(firstContent), `"tag": "connectors/openai/v0.6.0"`)
	digest, err := os.ReadFile(firstDigest)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x  connector-release.json\n", sha256.Sum256(firstContent)), string(digest))
	mismatchedArguments := arguments(filepath.Join(directory, "mismatch.json"), filepath.Join(directory, "mismatch.json.sha256"))
	mismatchedArguments[5] = "v0.6.1"
	require.ErrorContains(t, releaseArtifact(mismatchedArguments), "does not match manifest version")
}

func TestGeneratedConnectorsAreCurrent(t *testing.T) {
	for _, directory := range registeredConnectorDirectories(t) {
		path := filepath.Join("..", "..", filepath.FromSlash(directory), "connector.yaml")
		require.NoError(t, generate([]string{"--check", path}), directory)
	}
}

// registeredConnectorDirectories reads connectors.yaml directly, independent of the loader under test.
func registeredConnectorDirectories(t *testing.T) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "connectors.yaml"))
	require.NoError(t, err)
	var registry connectorDirectoryList
	require.NoError(t, yaml.Unmarshal(contents, &registry))
	require.NotEmpty(t, registry.Directories)
	return registry.Directories
}

type manifestIdentity struct {
	Name    string
	Version string
}

// readManifestIdentity reads one registered manifest's ID and release version directly from YAML.
func readManifestIdentity(t *testing.T, directory string) manifestIdentity {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(directory), "connector.yaml"))
	require.NoError(t, err)
	var manifest struct {
		Metadata manifestIdentity `yaml:"metadata"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	require.NotEmpty(t, manifest.Metadata.Name, directory)
	require.NotEmpty(t, manifest.Metadata.Version, directory)
	return manifest.Metadata
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
      backendCapabilities: [configuration.write, sheets.models-list]
      mockScenarios: [not-configured, ready]
      icon: icon.svg
    commands:
      - id: listModels
        capability: sheets.models-list
        request:
          method: GET
          url: https://models.example.com/v1/models
          credential: {field: access_token, scheme: header, header: x-goog-api-key}
          fixedHeaders: {x-api-version: "2023-06-01"}
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
	var releaseWire struct {
		Manifest struct {
			Spec struct {
				Studio struct {
					Commands []struct {
						Request struct {
							Credential   map[string]string `json:"credential"`
							FixedHeaders map[string]string `json:"fixedHeaders"`
						} `json:"request"`
					} `json:"commands"`
				} `json:"studio"`
			} `json:"spec"`
		} `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal(releaseBytes, &releaseWire))
	require.Len(t, releaseWire.Manifest.Spec.Studio.Commands, 1)
	commandRequest := releaseWire.Manifest.Spec.Studio.Commands[0].Request
	require.Equal(t, map[string]string{"field": "access_token", "scheme": "header", "header": "x-goog-api-key"}, commandRequest.Credential)
	require.Equal(t, map[string]string{"x-api-version": "2023-06-01"}, commandRequest.FixedHeaders)
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
