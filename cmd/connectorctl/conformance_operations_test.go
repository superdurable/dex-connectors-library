// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConformanceOptionalInputFields(t *testing.T) {
	manifest := filepath.Join("..", "..", "connectors", "newsapi", "connector.yaml")
	fields, err := conformanceOptionalInputFields(manifest, "SearchArticlesInput")
	require.NoError(t, err)
	require.Contains(t, fields, "pageSize")
	require.Contains(t, fields, "searchIn")
	require.NotContains(t, fields, "doesNotExist")
	stripe := filepath.Join("..", "..", "connectors", "stripe", "connector.yaml")
	cardFields, err := conformanceOptionalInputFields(stripe, "CreateCardCheckoutSessionInput")
	require.NoError(t, err)
	require.Contains(t, cardFields, "expiresAt")
}

func TestConformanceOptionalInputFieldsResolvesImportedAlias(t *testing.T) {
	manifest := filepath.Join("testdata", "conformance_alias", "connector.yaml")
	fields, err := conformanceOptionalInputFields(manifest, "OperationInput")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"optional", "zero"}, fields)
}
