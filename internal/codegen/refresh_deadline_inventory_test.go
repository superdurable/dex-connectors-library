// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestSuppliedClientDeadlineInventoryMatchesProductionDrivers(t *testing.T) {
	connectorRoot := filepath.Join("..", "..", "connectors")
	visited := map[string]bool{}
	err := filepath.WalkDir(connectorRoot, func(path string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.Name() != "credential_refresh.go" {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		manifestFile, err := os.Open(filepath.Join(filepath.Dir(filepath.Dir(path)), "connector.yaml"))
		if err != nil {
			return err
		}
		manifest, decodeError := schema.Decode(manifestFile)
		closeError := manifestFile.Close()
		if decodeError != nil {
			return decodeError
		}
		if closeError != nil {
			return closeError
		}
		visited[manifest.Metadata.Name] = true
		require.Equal(t, strings.Contains(string(contents), "context.WithTimeout("), suppliedClientDeadlineConnectors[manifest.Metadata.Name], path)
		return nil
	})
	require.NoError(t, err)
	for connectorName := range suppliedClientDeadlineConnectors {
		require.True(t, visited[connectorName], "deadline inventory names missing driver %s", connectorName)
	}
}
