// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorTestSelectionAcceptsRelativeCatalogPath(t *testing.T) {
	selected, err := selectConnectorTestDirectories(
		filepath.Join("..", "..", "catalog.yaml"),
		[]string{"connectors/example"},
		"changed",
		"HEAD",
		"HEAD",
	)
	require.NoError(t, err)
	require.Empty(t, selected)
}

func TestConnectorTestSelectionUsesLocalAndCatalogChanges(t *testing.T) {
	directories := []string{
		"connectors/acme/chat",
		"connectors/acme/mail",
		"connectors/other/payments",
	}
	testCases := []struct {
		name     string
		previous []string
		paths    []string
		expected []string
	}{
		{
			name: "one connector", previous: directories,
			paths: []string{"connectors/acme/mail/client.go"}, expected: []string{"connectors/acme/mail"},
		},
		{
			name: "one connector conformance fixture", previous: directories,
			paths: []string{"conformance/connectors/acme/mail/scenarios.json"}, expected: []string{"connectors/acme/mail"},
		},
		{
			name: "catalog addition", previous: directories[:2],
			paths: []string{"catalog.yaml"}, expected: []string{"connectors/other/payments"},
		},
		{
			name: "deleted connector", previous: append(directories, "connectors/removed/chat"),
			paths: []string{"catalog.yaml", "connectors/removed/chat/client.go"}, expected: []string{},
		},
		{
			name: "shared source", previous: directories,
			paths: []string{"schema/manifest.go"}, expected: directories,
		},
		{
			name: "shared React", previous: directories,
			paths: []string{"sdk/react/src/index.ts"}, expected: directories,
		},
		{
			name: "catalog tooling", previous: directories,
			paths: []string{"cmd/connectorctl/catalog.go"}, expected: directories,
		},
		{
			name: "conformance runner", previous: directories,
			paths: []string{"script/connector_conformance.py"}, expected: directories,
		},
		{
			name: "workflow", previous: directories,
			paths: []string{".github/workflows/ci.yml"}, expected: directories,
		},
		{
			name: "documentation", previous: directories,
			paths: []string{"docs/architecture.md", "connectors/acme/logo.svg"}, expected: []string{},
		},
		{
			name: "Connectors MCP server", previous: directories,
			paths: []string{"connectorsmcp/go.mod", "connectorsmcp/internal/server/server.go"}, expected: []string{},
		},
		{
			name: "directory site and agent rules", previous: directories,
			paths: []string{"site/src/App.tsx", "AGENTS.md", ".cursor/rules/project-core.mdc"}, expected: []string{},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			selected := selectConnectorDirectoriesForPaths(directories, testCase.previous, testCase.paths, "catalog.yaml")
			require.Equal(t, testCase.expected, selected)
		})
	}
}

func TestConnectorTestSelectionUsesLongestRegisteredDirectory(t *testing.T) {
	directories := []string{"connectors/acme", "connectors/acme/chat"}
	selected := selectConnectorDirectoriesForPaths(
		directories,
		directories,
		[]string{"connectors/acme/chat/client.go"},
		"catalog.yaml",
	)
	require.Equal(t, []string{"connectors/acme/chat"}, selected)
}

func TestConnectorTestMatrixCoversEveryDirectoryOnce(t *testing.T) {
	for _, connectorCount := range []int{1, 256, 513} {
		t.Run(fmt.Sprintf("%d connectors", connectorCount), func(t *testing.T) {
			directories := make([]string, 0, connectorCount)
			for index := range connectorCount {
				directories = append(directories, fmt.Sprintf("connectors/company/connector-%04d", index))
			}
			matrix := buildConnectorTestMatrix(directories, maximumGitHubActionsMatrixJobs)
			require.LessOrEqual(t, len(matrix.Include), maximumGitHubActionsMatrixJobs)
			expectedShardCount := min(connectorCount, maximumGitHubActionsMatrixJobs)
			seen := map[string]bool{}
			for _, shard := range matrix.Include {
				require.Equal(t, expectedShardCount, shard.Count)
				for _, directory := range directoriesInConnectorTestShard(directories, shard.Index, shard.Count) {
					require.False(t, seen[directory], directory)
					seen[directory] = true
				}
			}
			require.Len(t, seen, len(directories))
			require.Equal(t, matrix, buildConnectorTestMatrix(directories, maximumGitHubActionsMatrixJobs))
		})
	}
}

func TestEmptyConnectorTestMatrixHasNoJobs(t *testing.T) {
	require.Equal(t, connectorTestMatrix{Include: []connectorTestShard{}}, buildConnectorTestMatrix(nil, 256))
}

func TestCIJobsSelectOnlyChangedInputs(t *testing.T) {
	allJobs := ciJobSelection{Common: true, SDKPython: true, SDKTypeScript: true, SDKGoIntegration: true, React: true, Site: true, ConnectorsMCP: true}
	testCases := []struct {
		name     string
		path     string
		expected ciJobSelection
	}{
		{"Makefile", "Makefile", allJobs},
		{"script", "script/x.py", allJobs},
		{"workflow", ".github/workflows/ci.yml", allJobs},
		{"schema", "schema/x", allJobs},
		{"internal", "internal/x.go", allJobs},
		{"command", "cmd/connectorctl/x.go", allJobs},
		{"root Go module", "go.mod", allJobs},
		{"agent rules", "AGENTS.md", ciJobSelection{Common: true}},
		{"Claude rules", "CLAUDE.md", ciJobSelection{Common: true}},
		{"Cursor rules", ".cursor/rules/x.mdc", ciJobSelection{Common: true}},
		{"Codex rules", ".codex/rules/x.md", ciJobSelection{Common: true}},
		{"documentation", "docs/x.md", ciJobSelection{Common: true}},
		{"root conformance", "conformance/x.json", allJobs},
		{"connector conformance", "conformance/connectors/x/scenarios.json", ciJobSelection{}},
		{"Go SDK", "sdkgo/x.go", ciJobSelection{Common: true, SDKPython: true, SDKTypeScript: true, SDKGoIntegration: true, ConnectorsMCP: true}},
		{"Connectors MCP server", "connectorsmcp/internal/server/server.go", ciJobSelection{ConnectorsMCP: true}},
		{"Connectors MCP release config", "connectorsmcp/release.yaml", ciJobSelection{ConnectorsMCP: true}},
		{"Python SDK", "sdkpython/src/sdk.py", ciJobSelection{SDKPython: true}},
		{"TypeScript SDK", "sdkts/src/sdk.ts", ciJobSelection{SDKTypeScript: true}},
		{"React SDK", "sdk/react/src/index.ts", ciJobSelection{React: true}},
		{"site", "site/src/App.tsx", ciJobSelection{Site: true}},
		{"catalog", "catalog.yaml", ciJobSelection{Site: true}},
		{"Stripe TypeScript", "connectors/stripe/typescript/x.ts", ciJobSelection{Common: true}},
		{"connector Go", "connectors/foo/go/x.go", ciJobSelection{}},
		{"README", "README.md", ciJobSelection{Common: true}},
		{"gitignore", ".gitignore", ciJobSelection{}},
		{"code of conduct", "CODE_OF_CONDUCT.md", ciJobSelection{}},
		{"contributing", "CONTRIBUTING.md", ciJobSelection{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, selectCIJobsForPaths("changed", []string{testCase.path}))
		})
	}
	require.Equal(t, allJobs, selectCIJobsForPaths("all", nil))
	require.Equal(t, ciJobSelection{Common: true, Site: true}, selectCIJobsForPaths("changed", []string{"AGENTS.md", "catalog.yaml"}))
}

func TestConnectorTestSelectionIncludesTransitiveDependents(t *testing.T) {
	repositoryRoot := t.TempDir()
	directories := []string{"connectors/acme/base", "connectors/acme/first", "connectors/acme/second", "connectors/acme/unrelated"}
	for _, directory := range directories {
		moduleDirectory := filepath.Join(repositoryRoot, directory, "go")
		require.NoError(t, os.MkdirAll(moduleDirectory, 0o755))
		module := "module github.com/superdurable/dex-connectors-library/" + directory + "/go\n"
		switch directory {
		case "connectors/acme/first":
			module += "require github.com/superdurable/dex-connectors-library/connectors/acme/base/go v0.1.0\n"
		case "connectors/acme/second":
			module += "require github.com/superdurable/dex-connectors-library/connectors/acme/first/go v0.1.0\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, "go.mod"), []byte(module), 0o644))
	}
	selected, err := includeConnectorDependents(repositoryRoot, directories, directories[:1])
	require.NoError(t, err)
	require.Equal(t, directories[:3], selected)
}
