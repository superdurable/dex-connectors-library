// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
)

func TestConformanceOptionalInputFields(t *testing.T) {
	manifest := filepath.Join("..", "..", "connectors", "newsapi", "connector.yaml")
	fields, err := conformanceOptionalInputFields(newConnectorGoPackage(manifest), "SearchArticlesInput")
	require.NoError(t, err)
	require.Contains(t, fields, "pageSize")
	require.Contains(t, fields, "searchIn")
	require.NotContains(t, fields, "doesNotExist")
	stripe := filepath.Join("..", "..", "connectors", "stripe", "connector.yaml")
	cardFields, err := conformanceOptionalInputFields(newConnectorGoPackage(stripe), "CreateCardCheckoutSessionInput")
	require.NoError(t, err)
	require.Contains(t, cardFields, "expiresAt")
}

func TestConformanceOptionalInputFieldsResolvesImportedAlias(t *testing.T) {
	manifest := filepath.Join("testdata", "conformance_alias", "connector.yaml")
	fields, err := conformanceOptionalInputFields(newConnectorGoPackage(manifest), "OperationInput")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"optional", "zero"}, fields)
}

func TestConformanceOptionalInputFieldsReusesLoadedPackage(t *testing.T) {
	goPackage := newConnectorGoPackage(filepath.Join("..", "..", "connectors", "stripe", "connector.yaml"))
	_, err := conformanceOptionalInputFields(goPackage, "CreateCardCheckoutSessionInput")
	require.NoError(t, err)
	firstLoad := goPackage.loaded
	require.NotNil(t, firstLoad)
	_, err = conformanceOptionalInputFields(goPackage, "CreateRefundInput")
	require.NoError(t, err)
	require.Same(t, firstLoad, goPackage.loaded)
}

func TestConformanceOperationsCommandMatchesPerOperationPackageLoads(t *testing.T) {
	for _, connector := range []string{"stripe", "newsapi", filepath.Join("microsoft", "entra-id")} {
		t.Run(connector, func(t *testing.T) {
			manifestPath := filepath.Join("..", "..", "connectors", connector, "connector.yaml")
			var output bytes.Buffer
			require.NoError(t, conformanceOperationsCommand(manifestPath, &output))
			manifest, err := load(manifestPath)
			require.NoError(t, err)
			require.NotEmpty(t, manifest.Spec.Operations)
			var decoded map[string]struct {
				OptionalInputFields []string `json:"optionalInputFields"`
			}
			require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
			require.Len(t, decoded, len(manifest.Spec.Operations))
			for _, operation := range manifest.Spec.Operations {
				graph, err := gomodel.Load(filepath.Join(filepath.Dir(manifestPath), "go"), []string{operation.InputType})
				require.NoError(t, err)
				expected := []string{}
				for _, model := range graph.Models {
					if model.Name != operation.InputType {
						continue
					}
					for _, field := range model.Fields {
						if field.Optional {
							expected = append(expected, field.Name)
						}
					}
				}
				require.Equal(t, expected, decoded[operation.Name].OptionalInputFields, operation.Name)
			}
		})
	}
}
