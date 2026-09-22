// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
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
}
