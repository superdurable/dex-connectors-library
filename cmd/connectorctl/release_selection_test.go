// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests feed real release-matrix output to the release workflow's selector, language_publish.py select.

func TestScheduledRunRepairsIncompleteTaggedGoReleaseBeforeItsLanguages(t *testing.T) {
	repositoryRoot := t.TempDir()
	const directory = "connectors/acme/chat"
	writeConnectorFixture(t, repositoryRoot, directory, "example.com/"+directory)
	manifestPath := filepath.Join(repositoryRoot, directory, "connector.yaml")
	replaceInFile(t, manifestPath, "spec:\n", "spec:\n  languages:\n    python: {version: v0.1.0, operations: [appendRows], triggers: []}\n")
	require.NoError(t, os.MkdirAll(filepath.Join(repositoryRoot, directory, "python"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, directory, "python", "pyproject.toml"),
		[]byte("[project]\nname = \"dex-connector-acme-chat\"\nversion = \"0.1.0\"\n"), 0o600))
	catalogPath := writeFixtureCatalog(t, repositoryRoot, []string{directory})
	// An earlier run created the Go tag and release, then failed before uploading connector-release.complete.
	initializeConnectorReleaseRepository(t, repositoryRoot, directory+"/go/v0.1.0")
	const goTag = directory + "/go/v0.1.0"
	const pythonTag = directory + "/python/v0.1.0"
	scheduledRun := func(inventory string) (connectorReleaseMatrix, map[string]string, map[string]any) {
		t.Helper()
		inventoryPath := filepath.Join(t.TempDir(), "release-inventory.json")
		require.NoError(t, os.WriteFile(inventoryPath, []byte(inventory), 0o600))
		matrixOutput := filepath.Join(t.TempDir(), "release-matrix-output")
		require.NoError(t, releaseMatrixCommand([]string{"--catalog", catalogPath, "--include-published-languages",
			"--release-inventory", inventoryPath, "--github-output", matrixOutput}))
		outputs, selection := runReleaseSelection(t, repositoryRoot, matrixOutput, inventoryPath, 15)
		return readReleaseMatrixOutput(t, matrixOutput), outputs, selection
	}

	unavailable, outputs, _ := scheduledRun(`{"_inventory_unavailable":true}`)
	require.Empty(t, unavailable.Include, "an unknown release state waits for a run that can read the inventory")
	require.Equal(t, "0", outputs["count"])

	candidates, outputs, selection := scheduledRun(fmt.Sprintf(`{%q:["connector-release.json","connector-release.json.sha256"]}`, goTag))
	require.Len(t, candidates.Include, 1)
	require.Equal(t, goReleaseStateTagged, candidates.Include[0].ReleaseState)
	selectedGoReleases := readOutputMatrix(t, outputs["matrix"])
	require.Len(t, selectedGoReleases, 1)
	require.Equal(t, goTag, selectedGoReleases[0]["tag_prefix"].(string)+selectedGoReleases[0]["version"].(string))
	require.NotContains(t, selectedGoReleases[0], "release_state")
	require.Equal(t, "0", outputs["go_deferred_count"])
	languageBuilds := readOutputMatrix(t, outputs["language_matrix"])
	require.Len(t, languageBuilds, 1, "the language package builds after this run repairs its Go release")
	require.Equal(t, pythonTag, languageBuilds[0]["tag_prefix"].(string)+languageBuilds[0]["version"].(string))
	require.Empty(t, selection["deferred"])

	// The repaired run completed the Go release, then shipped the Python package GitHub-first.
	tagCommand := exec.Command("git", "tag", pythonTag)
	tagCommand.Dir = repositoryRoot
	tagOutput, err := tagCommand.CombinedOutput()
	require.NoError(t, err, "%s", tagOutput)
	completed, outputs, _ := scheduledRun(fmt.Sprintf(`{%q:["connector-release.complete"],%q:["language-release.complete"]}`, goTag, pythonTag))
	require.Empty(t, completed.Include, "a completed Go release is never selected again")
	require.Equal(t, "0", outputs["count"])
	require.Equal(t, "0", outputs["language_count"])
	require.Equal(t, "1", outputs["pypi_pending_count"], "only the PyPI upload of the completed release remains")
}

func TestPendingGoReleasesBeyondTheGitHubJobLimitDrainThroughTheCap(t *testing.T) {
	repositoryRoot := t.TempDir()
	directories := make([]string, 0, maximumGitHubActionsMatrixJobs+1)
	for index := range maximumGitHubActionsMatrixJobs + 1 {
		directory := fmt.Sprintf("connectors/acme/product-%03d", index)
		writeConnectorFixture(t, repositoryRoot, directory, "example.com/"+directory)
		replaceInFile(t, filepath.Join(repositoryRoot, filepath.FromSlash(directory), "connector.yaml"),
			"name: fixture-connector", fmt.Sprintf("name: acme-product-%03d", index))
		directories = append(directories, directory)
	}
	catalogPath := writeFixtureCatalog(t, repositoryRoot, directories)
	initializeConnectorReleaseRepository(t, repositoryRoot, "fixture-baseline")
	inventoryPath := filepath.Join(t.TempDir(), "release-inventory.json")
	require.NoError(t, os.WriteFile(inventoryPath, []byte("{}"), 0o600))
	matrixOutput := filepath.Join(t.TempDir(), "release-matrix-output")
	require.NoError(t, releaseMatrixCommand([]string{"--catalog", catalogPath, "--include-published-languages",
		"--release-inventory", inventoryPath, "--github-output", matrixOutput}))
	require.Len(t, readReleaseMatrixOutput(t, matrixOutput).Include, maximumGitHubActionsMatrixJobs+1)

	outputs, selection := runReleaseSelection(t, repositoryRoot, matrixOutput, inventoryPath, 15)
	require.Equal(t, "15", outputs["count"])
	require.Equal(t, "242", outputs["go_deferred_count"])
	selectedGoReleases := readOutputMatrix(t, outputs["matrix"])
	require.Equal(t, "connectors/acme/product-000", selectedGoReleases[0]["directory"])
	require.Equal(t, "connectors/acme/product-014", selectedGoReleases[14]["directory"])
	require.Len(t, selection["go_deferred"], 242)
}

func writeFixtureCatalog(t *testing.T, repositoryRoot string, directories []string) string {
	t.Helper()
	catalogPath := filepath.Join(repositoryRoot, "catalog.yaml")
	require.NoError(t, os.WriteFile(catalogPath, []byte("apiVersion: connectors.dex.dev/catalog-source/v1alpha1\n"+
		"kind: ConnectorCatalogSource\ndirectories:\n  - "+strings.Join(directories, "\n  - ")+"\n"), 0o600))
	return catalogPath
}

// runReleaseSelection runs the selector in repositoryRoot with every registry answering 404, so the test stays offline.
func runReleaseSelection(t *testing.T, repositoryRoot, matrixOutput, inventoryPath string, goReleaseLimit int) (map[string]string, map[string]any) {
	t.Helper()
	scriptDirectory, err := filepath.Abs(filepath.Join("..", "..", "script", "release"))
	require.NoError(t, err)
	workDirectory := t.TempDir()
	githubOutput := filepath.Join(workDirectory, "output")
	selectionPath := filepath.Join(workDirectory, "selection.json")
	command := exec.Command("python3", "-c", `import sys
sys.path.insert(0, sys.argv[1])
import language_publish
language_publish.request_registry = lambda *_arguments, **_options: None
sys.argv = ["language_publish.py", *sys.argv[2:]]
raise SystemExit(language_publish.main())`, scriptDirectory, "select", "--matrix-output", matrixOutput,
		"--github-output", githubOutput, "--go-release-limit", fmt.Sprint(goReleaseLimit),
		"--first-publish-limit", "3", "--github-release-limit", "15")
	command.Dir = repositoryRoot
	command.Env = append(os.Environ(), "GH_RELEASE_INVENTORY_PATH="+inventoryPath,
		"GH_RELEASE_CREATED_AT_PATH=", "LANGUAGE_SELECTION_PATH="+selectionPath, "GITHUB_STEP_SUMMARY=")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	contents, err := os.ReadFile(githubOutput)
	require.NoError(t, err)
	outputs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		name, value, isFound := strings.Cut(line, "=")
		require.True(t, isFound, line)
		outputs[name] = value
	}
	selectionContents, err := os.ReadFile(selectionPath)
	require.NoError(t, err)
	var selection map[string]any
	require.NoError(t, json.Unmarshal(selectionContents, &selection))
	return outputs, selection
}

func readOutputMatrix(t *testing.T, encoded string) []map[string]any {
	t.Helper()
	var matrix struct {
		Include []map[string]any `json:"include"`
	}
	require.NoError(t, json.Unmarshal([]byte(encoded), &matrix))
	return matrix.Include
}
