// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

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
			name: "documentation", previous: directories,
			paths: []string{"docs/architecture.md", "connectors/acme/logo.svg"}, expected: []string{},
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
