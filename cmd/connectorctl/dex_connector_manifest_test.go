// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func TestOperationAndBranchNamesBecomePythonKeywords(t *testing.T) {
	require.Equal(t, "get_http_response", lowerCamelToSnake("getHTTPResponse"))
	require.Equal(t, "not_found", lowerCamelToSnake("notFound"))
	// An acronym followed by a word is one Python identifier segment.
	require.Equal(t, "read_id_token", lowerCamelToSnake("readIDToken"))
	require.Equal(t, "acme_data_api", pythonImportPackageName("connectors/acme/data-api"))
}

func TestGenerateLanguagePackageManifestsAndDetectDrift(t *testing.T) {
	repositoryRoot := t.TempDir()
	connectorDirectory := filepath.Join(repositoryRoot, "connectors", "google", "sheets")
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "python"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "typescript"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, "catalog.yaml"), []byte("connectors: []\n"), 0o600))
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	manifestSource := strings.Replace(string(fixture), "spec:\n", "spec:\n  languages:\n    python: {version: v0.2.0, package: dex-connector-custom-sheets, operations: [appendRows], triggers: []}\n    typescript: {version: v0.3.0, operations: [appendRows], triggers: []}\n", 1)
	manifestSource = strings.Replace(manifestSource, "  operations:\n", `  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.1.0 <0.2.0"
      backendCapabilities: [configuration.write]
      mockScenarios: [ready]
      icon: icon.svg
    units:
      - id: sheetPicker
        goName: SheetPicker
        description: Select a sheet.
        outputs:
          - {name: sheetId, goName: SheetID, type: string}
  triggers:
    - {name: rowChanged, goName: RowChanged, eventType: RowChangedEvent, configurationType: RowChangedConfiguration, description: Receive a changed row.}
  operations:
`, 1)
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestSource), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "go"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(connectorDirectory, "go", "go.mod"), []byte("module example.com/sheets\n\ngo 1.24\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(connectorDirectory, "python", "pyproject.toml"), []byte("[project]\nname = \"dex-connector-custom-sheets\"\nversion = \"0.2.0\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(connectorDirectory, "typescript", "package.json"), []byte(`{"name":"@superdurable/connector-google-sheets","version":"0.3.0"}`), 0o600))
	require.NoError(t, generate([]string{manifestPath}))
	require.NoError(t, generate([]string{"--check", manifestPath}))
	jsonSchemaBytes, err := os.ReadFile(filepath.Join("..", "..", "schema", "dex-connector.schema.json"))
	require.NoError(t, err)
	var jsonSchema any
	require.NoError(t, json.Unmarshal(jsonSchemaBytes, &jsonSchema))
	compiler := jsonschema.NewCompiler()
	require.NoError(t, compiler.AddResource("https://connectors.dex.dev/dex-connector.schema.json", jsonSchema))
	compiledSchema, err := compiler.Compile("https://connectors.dex.dev/dex-connector.schema.json")
	require.NoError(t, err)
	for _, testCase := range []struct {
		path, name, ecosystem, version, factory, stepType, connectionName, branchField string
	}{
		{filepath.Join(connectorDirectory, "python", "src", "dex_connectors", "google_sheets", "dex-connector.json"), "dex-connector-custom-sheets", "pypi", "0.2.0", "new_append_rows_step", "step_type", "connection_name", "appended"},
		{filepath.Join(connectorDirectory, "typescript", "dex-connector.json"), "@superdurable/connector-google-sheets", "npm", "0.3.0", "newAppendRowsStep", "stepType", "connectionName", "appended"},
	} {
		contents, err := os.ReadFile(testCase.path)
		require.NoError(t, err)
		var document map[string]any
		require.NoError(t, json.Unmarshal(contents, &document))
		require.NoError(t, compiledSchema.Validate(document))
		require.Equal(t, "google-sheets-fixture", document["connectorId"])
		require.Equal(t, "connectors/google/sheets/go/v0.1.0", document["releaseTag"])
		require.Equal(t, map[string]any{"ecosystem": testCase.ecosystem, "name": testCase.name, "version": testCase.version}, document["package"])
		factories := document["factories"].([]any)
		require.Len(t, factories, 1)
		factory := factories[0].(map[string]any)
		require.Equal(t, testCase.factory, factory["name"])
		require.Equal(t, "appendRows", factory["operationId"])
		fields := factory["fields"].(map[string]any)
		require.Equal(t, testCase.stepType, fields["stepType"])
		require.Equal(t, testCase.connectionName, fields["connectionName"])
		require.NotContains(t, fields, "configurationUI")
		require.NotContains(t, fields, "progressStream")
		require.NotContains(t, fields, "textStream")
		branches := factory["branches"].([]any)
		require.Equal(t, testCase.branchField, branches[0].(map[string]any)["field"])
		require.Equal(t, true, branches[2].(map[string]any)["optional"])
		require.NoError(t, os.WriteFile(testCase.path, []byte("stale"), 0o600))
		require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale")
		require.NoError(t, generate([]string{manifestPath}))
	}
	pythonOnly := strings.Replace(manifestSource, "python: {version: v0.2.0, package: dex-connector-custom-sheets, operations: [appendRows]", "python: {version: v0.2.0, package: dex-connector-custom-sheets, operations: []", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(pythonOnly), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale")
	require.NoError(t, generate([]string{manifestPath}))
	pythonManifest, err := os.ReadFile(filepath.Join(connectorDirectory, "python", "src", "dex_connectors", "google_sheets", "dex-connector.json"))
	require.NoError(t, err)
	require.Contains(t, string(pythonManifest), `"factories": []`)
	require.NoError(t, generate([]string{"--check", manifestPath}))
	manifestSource = strings.Replace(manifestSource, "  languages:\n    python: {version: v0.2.0, package: dex-connector-custom-sheets, operations: [appendRows], triggers: []}\n    typescript: {version: v0.3.0, operations: [appendRows], triggers: []}\n", "", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestSource), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated language package manifest is stale")
	require.NoError(t, generate([]string{manifestPath}))
	require.NoError(t, generate([]string{"--check", manifestPath}))
	manifestSource = strings.Replace(manifestSource, "spec:\n", "spec:\n  languages:\n    python: {version: v0.2.0, package: dex-connector-custom-sheets, operations: [appendRows], triggers: []}\n    typescript: {version: v0.3.0, operations: [appendRows], triggers: []}\n", 1)
	manifestSource = strings.Replace(manifestSource, "id: appended", "id: annotations", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestSource), 0o600))
	require.ErrorContains(t, generate([]string{manifestPath}), `factory maps more than one field to "annotations"`)
}
