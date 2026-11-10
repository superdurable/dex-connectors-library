// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type connectorsMCPFixtureRelease struct {
	directory     string
	connectorID   string
	sdkVersion    string
	hasAgentTools bool
	exposed       []string
	declined      []string
}

func TestConnectorsMCPInventorySelectsLatestCompleteReleasesWithAgentTools(t *testing.T) {
	repositoryRoot := t.TempDir()
	runConnectorsMCPFixtureGit(t, repositoryRoot, "init", "-q", "-b", "main")
	directories := []string{"connectors/acme/chat", "connectors/acme/legacy", "connectors/acme/notes", "connectors/beta"}
	writeConnectorsMCPFixtureFile(t, repositoryRoot, "catalog.yaml", "apiVersion: connectors.dex.dev/catalog-source/v1alpha1\n"+
		"kind: ConnectorCatalogSource\ndirectories:\n  - connectors/acme/chat\n  - connectors/acme/legacy\n  - connectors/acme/notes\n  - connectors/beta\n")
	writeConnectorsMCPFixtureFile(t, repositoryRoot, "connectorsmcp/release.yaml", "series: v0.1\nwithdrawnConnectors: [old-tool]\n")
	commitConnectorsMCPFixtureReleases(t, repositoryRoot, "v0.1.0", []connectorsMCPFixtureRelease{
		{directory: directories[0], connectorID: "acme-chat", sdkVersion: "v0.35.0", hasAgentTools: true, exposed: []string{"listStoryIDs"}, declined: []string{"deleteMessage"}},
		{directory: directories[1], connectorID: "acme-legacy", sdkVersion: "v0.34.0", exposed: []string{"getItem"}},
		{directory: directories[2], connectorID: "acme-notes", sdkVersion: "v0.35.0", declined: []string{"postNote"}},
		{directory: directories[3], connectorID: "beta", sdkVersion: "v0.35.0", hasAgentTools: true, exposed: []string{"getItem"}},
	})
	// A newer chat release is tagged but its release is incomplete, so v0.1.0 stays the latest complete one.
	commitConnectorsMCPFixtureReleases(t, repositoryRoot, "v0.2.0", []connectorsMCPFixtureRelease{
		{directory: directories[0], connectorID: "acme-chat", sdkVersion: "v0.36.0", hasAgentTools: true, exposed: []string{"listStoryIDs", "postMessage"}},
		{directory: directories[3], connectorID: "beta", sdkVersion: "v0.35.0", hasAgentTools: true, exposed: []string{"getItem", "putItem"}},
	})
	// A tag that main cannot reach, such as a release candidate branch, is never linked.
	runConnectorsMCPFixtureGit(t, repositoryRoot, "checkout", "-q", "-b", "candidate")
	commitConnectorsMCPFixtureReleases(t, repositoryRoot, "v0.3.0", []connectorsMCPFixtureRelease{
		{directory: directories[3], connectorID: "beta", sdkVersion: "v0.35.0", hasAgentTools: true, exposed: []string{"getItem"}},
	})
	runConnectorsMCPFixtureGit(t, repositoryRoot, "checkout", "-q", "main")
	completed := map[string][]string{
		directories[0] + "/go/v0.1.0": {"connector-release.complete"},
		directories[0] + "/go/v0.2.0": {"connector-release.json"},
		directories[1] + "/go/v0.1.0": {"connector-release.complete"},
		directories[2] + "/go/v0.1.0": {"connector-release.complete"},
		directories[3] + "/go/v0.2.0": {"connector-release.complete"},
		directories[3] + "/go/v0.3.0": {"connector-release.complete"},
	}

	inventory, err := buildConnectorsMCPInventory(filepath.Join(repositoryRoot, "catalog.yaml"),
		writeConnectorsMCPFixtureInventory(t, completed), filepath.Join(repositoryRoot, "connectorsmcp", "release.yaml"))

	require.NoError(t, err)
	require.Equal(t, "v0.1", inventory.Series)
	require.Equal(t, []string{"old-tool"}, inventory.WithdrawnConnectors)
	require.Equal(t, runConnectorsMCPFixtureGit(t, repositoryRoot, "rev-parse", "HEAD"), inventory.SourceCommit)
	require.Equal(t, runConnectorsMCPFixtureGit(t, repositoryRoot, "rev-parse", "HEAD:connectorsmcp"), inventory.ServerSourceTree)
	require.Equal(t, []connectorsMCPCandidate{
		{
			ConnectorID: "acme-chat", DisplayName: "Acme Chat Display", Directory: directories[0],
			Module: "example.com/" + directories[0] + "/go", Version: "v0.1.0", Tag: directories[0] + "/go/v0.1.0",
			PackageName: "acmechat", Tools: []string{"acme-chat_list_story_ids"},
		},
		{
			ConnectorID: "beta", DisplayName: "Beta Display", Directory: directories[3],
			Module: "example.com/" + directories[3] + "/go", Version: "v0.2.0", Tag: directories[3] + "/go/v0.2.0",
			PackageName: "beta", Tools: []string{"beta_get_item", "beta_put_item"},
		},
	}, inventory.Connectors)
}

func TestConnectorsMCPInventoryRefusesAnUnavailableReleaseInventory(t *testing.T) {
	repositoryRoot := t.TempDir()
	writeConnectorsMCPFixtureFile(t, repositoryRoot, "release.yaml", "series: v0.1\n")
	inventoryPath := filepath.Join(t.TempDir(), "inventory.json")
	require.NoError(t, os.WriteFile(inventoryPath, []byte(`{"_inventory_unavailable":true}`), 0o600))

	_, err := buildConnectorsMCPInventory(filepath.Join(repositoryRoot, "catalog.yaml"), inventoryPath, filepath.Join(repositoryRoot, "release.yaml"))

	require.ErrorContains(t, err, "release inventory is unavailable")
}

func TestConnectorsMCPReleaseConfigValidation(t *testing.T) {
	testCases := []struct {
		name     string
		contents string
		message  string
	}{
		{"series with patch", "series: v0.1.0\n", "must be vMAJOR.MINOR"},
		{"missing series", "withdrawnConnectors: []\n", "must be vMAJOR.MINOR"},
		{"unknown field", "series: v0.1\nlaunchContract: 1\n", "field launchContract not found"},
		{"invalid withdrawn ID", "series: v0.1\nwithdrawnConnectors: [Slack]\n", "is invalid"},
		{"repeated withdrawn ID", "series: v0.1\nwithdrawnConnectors: [slack, slack]\n", "listed twice"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "release.yaml")
			require.NoError(t, os.WriteFile(path, []byte(testCase.contents), 0o600))
			_, err := readConnectorsMCPReleaseConfig(path)
			require.ErrorContains(t, err, testCase.message)
		})
	}
	path := filepath.Join(t.TempDir(), "release.yaml")
	require.NoError(t, os.WriteFile(path, []byte("series: v1.0\n"), 0o600))
	config, err := readConnectorsMCPReleaseConfig(path)
	require.NoError(t, err)
	require.Equal(t, connectorsMCPReleaseConfig{Series: "v1.0", WithdrawnConnectors: []string{}}, config)
}

func commitConnectorsMCPFixtureReleases(t *testing.T, repositoryRoot, version string, releases []connectorsMCPFixtureRelease) {
	t.Helper()
	for _, release := range releases {
		packageName := filepath.Base(release.directory)
		if release.connectorID == "acme-chat" {
			packageName = "acmechat"
		}
		operations := ""
		for _, operation := range release.exposed {
			operations += fmt.Sprintf("    - name: %s\n      agent:\n        exposed: true\n", operation)
		}
		for _, operation := range release.declined {
			operations += fmt.Sprintf("    - name: %s\n      agent:\n        exposed: false\n", operation)
		}
		operations += "    - name: unreviewedOperation\n"
		writeConnectorsMCPFixtureFile(t, repositoryRoot, release.directory+"/connector.yaml", fmt.Sprintf(
			"metadata:\n  name: %s\n  displayName: %s\n  futureField: kept by newer manifests\nspec:\n  operations:\n%s",
			release.connectorID, map[string]string{"acme-chat": "Acme Chat Display", "beta": "Beta Display"}[release.connectorID], operations))
		writeConnectorsMCPFixtureFile(t, repositoryRoot, release.directory+"/go/go.mod", fmt.Sprintf(
			"module example.com/%s/go\n\ngo 1.24.0\n\nrequire github.com/superdurable/dex-connectors-library/sdkgo %s\n", release.directory, release.sdkVersion))
		agentToolsPath := filepath.Join(repositoryRoot, release.directory, "go", connectorsMCPAgentToolsFile)
		if release.hasAgentTools {
			writeConnectorsMCPFixtureFile(t, repositoryRoot, release.directory+"/go/"+connectorsMCPAgentToolsFile,
				"// Code generated by connectorctl generate; DO NOT EDIT.\n\npackage "+packageName+"\n")
		} else {
			require.NoError(t, os.RemoveAll(agentToolsPath))
		}
	}
	runConnectorsMCPFixtureGit(t, repositoryRoot, "add", "-A")
	runConnectorsMCPFixtureGit(t, repositoryRoot, "commit", "-q", "-m", "release "+version)
	for _, release := range releases {
		runConnectorsMCPFixtureGit(t, repositoryRoot, "tag", release.directory+"/go/"+version)
	}
}

func writeConnectorsMCPFixtureInventory(t *testing.T, inventory map[string][]string) string {
	t.Helper()
	encoded, err := json.Marshal(inventory)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "release-inventory.json")
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}

func writeConnectorsMCPFixtureFile(t *testing.T, repositoryRoot, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(repositoryRoot, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func runConnectorsMCPFixtureGit(t *testing.T, repositoryRoot string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = repositoryRoot
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "git %v: %s", arguments, output)
	return string(output[:len(output)-min(len(output), 1)])
}
