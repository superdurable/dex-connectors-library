// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestScaffoldWritesFailureFixturesAndKeepsTheRestOfTheManifest(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "connectors", "google", "gmail", "connector.yaml"))
	require.NoError(t, err)
	manifestPath := filepath.Join(t.TempDir(), "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, source, 0o600))
	var notes bytes.Buffer

	require.NoError(t, scaffoldMocks(manifestPath, &notes))

	scaffolded, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(scaffolded), strings.SplitN(string(source), "      mocks:", 2)[0][:200]))
	manifest, err := load(manifestPath)
	require.NoError(t, err)
	getMessage := manifest.Spec.Operations[0]
	require.Equal(t, []string{"notFoundFailure", "providerRejectedFailure", "invalidResponseFailure", "defectFailure"}, mockNames(getMessage))
	require.Equal(t, "NOT_FOUND", getMessage.Mocks[0].Failure.Kind)
	require.Equal(t, "The Gmail message does not exist.", getMessage.Mocks[0].Failure.Message)
	require.Equal(t, "VALIDATION", getMessage.Mocks[3].Failure.Kind)
	sendMessage := manifest.Spec.Operations[1]
	require.Contains(t, mockNames(sendMessage), "lostResponse")
	require.Equal(t, "TRANSPORT", sendMessage.Mocks[1].Uncertain.Failure.Kind)
	require.Contains(t, notes.String(), "getMessage: write the read mock output by hand")
	require.Contains(t, notes.String(), "sendMessage: write the sent mock output by hand")
	require.ErrorContains(t, manifest.ValidateMockCoverage(), "getMessage: branch read needs a mock")

	notes.Reset()
	require.NoError(t, scaffoldMocks(manifestPath, &notes))
	rescaffolded, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	require.Equal(t, string(scaffolded), string(rescaffolded), "a second scaffold adds nothing")
	require.Equal(t, string(source), strings.Join(removeScaffoldedLines(string(scaffolded)), ""))
}

func TestScaffoldAppendsToAnExistingMocksSequence(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "mocks.yaml"))
	require.NoError(t, err)
	trimmed := strings.Replace(string(source), "        - name: invalidPage\n          branch: defect\n          description: The page number is negative.\n          failure: {kind: VALIDATION, message: The page must not be negative.}\n", "", 1)
	manifestPath := filepath.Join(t.TempDir(), "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(trimmed), 0o600))

	require.NoError(t, scaffoldMocks(manifestPath, &bytes.Buffer{}))

	manifest, err := load(manifestPath)
	require.NoError(t, err)
	require.Equal(t, []string{"threePages", "defectFailure"}, mockNames(manifest.Spec.Operations[1]))
	require.NoError(t, manifest.ValidateMockCoverage())
}

func TestScaffoldRejectsMocksBeforeOtherOperationKeys(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "mocks.yaml"))
	require.NoError(t, err)
	moved := strings.Replace(string(source), "      pagination: {inputField: page, nextField: nextPage}\n      mocks:\n", "      mocks:\n", 1)
	moved = strings.Replace(moved, "    - name: createWidget\n", "      pagination: {inputField: page, nextField: nextPage}\n    - name: createWidget\n", 1)
	manifestPath := filepath.Join(t.TempDir(), "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(moved), 0o600))
	_, err = load(manifestPath)
	require.NoError(t, err)

	require.ErrorContains(t, scaffoldMocks(manifestPath, &bytes.Buffer{}), "operation 2: move mocks to the end of the operation before scaffolding")
}

func TestGenerateWritesAndChecksTheMockPackage(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeMockFixtureConnector(t, directory)

	require.NoError(t, generate([]string{manifestPath}))
	require.NoError(t, generate([]string{"--check", manifestPath}))
	mockSource := filepath.Join(directory, "mockfixturemock", "zz_generated_mock.go")
	require.FileExists(t, mockSource)
	require.FileExists(t, filepath.Join(directory, "mockfixturemock", "zz_generated_mock_test.go"))
	require.NoError(t, os.WriteFile(mockSource, []byte("stale"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale")

	withoutMocks := removeAllMocks(t, manifestPath)
	require.NoError(t, os.WriteFile(manifestPath, []byte(withoutMocks), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated mock package is stale because the manifest declares no mocks")
	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, mockSource)
	require.NoError(t, generate([]string{"--check", manifestPath}))
}

func TestValidateRequiresMockCoverage(t *testing.T) {
	manifestPath := writeMockFixtureConnector(t, t.TempDir())
	require.NoError(t, run([]string{"validate", manifestPath}))
	contents, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	incomplete := strings.Replace(string(contents), "          description: A widget with every field set.\n          default: true\n", "          description: A widget with every field set.\n", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(incomplete), 0o600))

	require.ErrorContains(t, run([]string{"validate", manifestPath}), "getWidget: a query needs exactly one default mock")
}

func TestCatalogListsMocksAndTheMockPackage(t *testing.T) {
	directory := t.TempDir()
	manifest, err := load(writeMockFixtureConnector(t, directory))
	require.NoError(t, err)
	withoutMocks := manifest
	withoutMocks.Spec.Operations = []schema.Operation{{Name: "getWidget", Kind: "query", Description: "Read one widget."}}

	encoded, err := encodeConnectorCatalog([]connectorDirectoryEntry{
		{Directory: "connectors/example/mock-fixture", ModulePath: "example.com/connectors/example/mock-fixture", Manifest: manifest},
		{Directory: "connectors/example/plain", ModulePath: "example.com/connectors/example/plain", Manifest: withoutMocks},
	})

	require.NoError(t, err)
	catalog := string(encoded)
	require.Contains(t, catalog, "      mockPackage: example.com/connectors/example/mock-fixture/mockfixturemock\n")
	require.Contains(t, catalog, "        - operation: getWidget\n          name: gear\n          branch: found\n          description: A widget with every field set.\n          default: true\n")
	require.Contains(t, catalog, "        - operation: createWidget\n          name: connectionLost\n          branch: uncertain\n")
	require.Contains(t, catalog, "        - operation: getWidget\n          name: throttled\n          description: The provider throttles the read once.\n")
	require.Equal(t, 1, strings.Count(catalog, "mockPackage:"), "a connector without mocks has no mock package")
}

func writeMockFixtureConnector(t *testing.T, directory string) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "mocks.yaml"))
	require.NoError(t, err)
	manifestPath := filepath.Join(directory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, source, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/mockfixture\n\ngo 1.24\n"), 0o600))
	return manifestPath
}

func removeAllMocks(t *testing.T, manifestPath string) string {
	t.Helper()
	contents, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var kept []string
	isInMocks := false
	for _, line := range strings.SplitAfter(string(contents), "\n") {
		switch {
		case line == "      mocks:\n":
			isInMocks = true
			continue
		case isInMocks && strings.HasPrefix(line, "        "):
			continue
		}
		isInMocks = false
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

// removeScaffoldedLines drops the mocks blocks scaffold inserted into a manifest that had none.
func removeScaffoldedLines(contents string) []string {
	var kept []string
	isInMocks := false
	for _, line := range strings.SplitAfter(contents, "\n") {
		if line == "      mocks:\n" {
			isInMocks = true
			continue
		}
		if isInMocks && strings.HasPrefix(line, "        ") {
			continue
		}
		isInMocks = false
		kept = append(kept, line)
	}
	return kept
}

func mockNames(operation schema.Operation) []string {
	names := make([]string, 0, len(operation.Mocks))
	for _, mock := range operation.Mocks {
		names = append(names, mock.Name)
	}
	return names
}
