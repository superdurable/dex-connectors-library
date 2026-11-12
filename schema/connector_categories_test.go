// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

func TestMetadataCategoriesValidation(t *testing.T) {
	fixture, err := os.ReadFile("testdata/google-oauth.yaml")
	require.NoError(t, err)
	categoriesLine := regexp.MustCompile(`(?m)^  categories: .*\n`)
	require.Regexp(t, categoriesLine, string(fixture))
	for _, testCase := range []struct {
		name           string
		categoriesLine string
		expected       []string
		problem        string
	}{
		{name: "one category", categoriesLine: "  categories: [productivity]\n", expected: []string{"productivity"}},
		{name: "primary and secondary", categoriesLine: "  categories: [productivity, files-documents]\n", expected: []string{"productivity", "files-documents"}},
		{name: "missing", categoriesLine: "", problem: "metadata.categories must list 1 or 2 category IDs"},
		{name: "empty", categoriesLine: "  categories: []\n", problem: "metadata.categories must list 1 or 2 category IDs"},
		{name: "more than two", categoriesLine: "  categories: [productivity, files-documents, ai]\n", problem: "metadata.categories must list 1 or 2 category IDs"},
		{name: "unknown", categoriesLine: "  categories: [spreadsheets]\n", problem: `metadata.categories names unknown category "spreadsheets"`},
		{name: "duplicate", categoriesLine: "  categories: [productivity, productivity]\n", problem: `metadata.categories has duplicate category "productivity"`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			contents := categoriesLine.ReplaceAllLiteralString(string(fixture), testCase.categoriesLine)
			manifest, err := schema.Decode(strings.NewReader(contents))
			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.expected, manifest.Metadata.Categories)
		})
	}
}

func TestConnectorCategoriesAreStableKebabCaseIDs(t *testing.T) {
	categories := schema.ConnectorCategories()
	require.NotEmpty(t, categories)
	seen := make(map[string]bool, len(categories))
	for _, category := range categories {
		require.Regexp(t, `^[a-z]+(-[a-z]+)*$`, category.ID)
		require.NotEmpty(t, strings.TrimSpace(category.DisplayName), category.ID)
		require.NotEmpty(t, strings.TrimSpace(category.Description), category.ID)
		require.False(t, seen[category.ID], "duplicate category %s", category.ID)
		seen[category.ID] = true
		require.True(t, schema.IsKnownConnectorCategory(category.ID))
	}
	require.False(t, schema.IsKnownConnectorCategory("unknown"))
	categories[0].ID = "mutated"
	require.NotEqual(t, "mutated", schema.ConnectorCategories()[0].ID, "callers own the returned slice")
}

func TestManifestJSONSchemaCategoriesMatchTaxonomy(t *testing.T) {
	contents, err := os.ReadFile("connector-manifest.schema.json")
	require.NoError(t, err)
	var definition map[string]any
	require.NoError(t, json.Unmarshal(contents, &definition))
	metadata := definition["properties"].(map[string]any)["metadata"].(map[string]any)
	require.Contains(t, metadata["required"], "categories")
	categoriesDefinition := metadata["properties"].(map[string]any)["categories"].(map[string]any)
	var enumeratedIDs []string
	for _, id := range categoriesDefinition["items"].(map[string]any)["enum"].([]any) {
		enumeratedIDs = append(enumeratedIDs, id.(string))
	}
	var taxonomyIDs []string
	for _, category := range schema.ConnectorCategories() {
		taxonomyIDs = append(taxonomyIDs, category.ID)
	}
	require.Equal(t, taxonomyIDs, enumeratedIDs, "schema/connector_categories.go owns the category list")

	compiler := jsonschema.NewCompiler()
	require.NoError(t, compiler.AddResource("https://connectors.dex.dev/categories.schema.json", categoriesDefinition))
	compiled, err := compiler.Compile("https://connectors.dex.dev/categories.schema.json")
	require.NoError(t, err)
	for _, testCase := range []struct {
		categories string
		isValid    bool
	}{
		{categories: "[ai]", isValid: true},
		{categories: "[ai, data-search]", isValid: true},
		{categories: "[]"},
		{categories: "[ai, data-search, analytics]"},
		{categories: "[spreadsheets]"},
		{categories: "[ai, ai]"},
	} {
		var document any
		require.NoError(t, yaml.Unmarshal([]byte(testCase.categories), &document))
		err := compiled.Validate(document)
		if testCase.isValid {
			require.NoError(t, err, testCase.categories)
		} else {
			require.Error(t, err, testCase.categories)
		}
	}
}
