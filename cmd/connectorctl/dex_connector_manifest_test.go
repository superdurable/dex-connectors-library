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
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestOperationAndBranchNamesBecomePythonKeywords(t *testing.T) {
	require.Equal(t, "get_http_response", lowerCamelToSnake("getHTTPResponse"))
	require.Equal(t, "not_found", lowerCamelToSnake("notFound"))
	// An acronym followed by a word is one Python identifier segment.
	require.Equal(t, "read_id_token", lowerCamelToSnake("readIDToken"))
	require.Equal(t, "list_story_ids", lowerCamelToSnake("listStoryIDs"))
	require.Equal(t, "read_ids_from_cache", lowerCamelToSnake("readIDsFromCache"))
	require.Equal(t, "get_api_settings", lowerCamelToSnake("getAPISettings"))
	require.Equal(t, "acme_data_api", pythonImportPackageName("connectors/acme/data-api"))
}

// sdkgo mirrors lowerCamelToSnake to name agent tools; both read the same vectors.
func TestLowerCamelToSnakeMatchesSDKAgentToolNameVectors(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "sdkgo", "testdata", "agent_tool_names.json"))
	require.NoError(t, err)
	var document struct {
		Vectors []struct {
			OperationID string `json:"operationId"`
			SnakeCase   string `json:"snakeCase"`
		} `json:"vectors"`
	}
	require.NoError(t, json.Unmarshal(contents, &document))
	require.NotEmpty(t, document.Vectors)
	for _, vector := range document.Vectors {
		require.Equal(t, vector.SnakeCase, lowerCamelToSnake(vector.OperationID), vector.OperationID)
	}
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

func TestGeneratedTypeScriptManifestDescribesTriggerBindingFactories(t *testing.T) {
	repositoryRoot := t.TempDir()
	connectorDirectory := filepath.Join(repositoryRoot, "connectors", "google", "sheets")
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "typescript"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(connectorDirectory, "go"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repositoryRoot, "catalog.yaml"), []byte("connectors: []\n"), 0o600))
	fixture, err := os.ReadFile(filepath.Join("..", "..", "schema", "testdata", "google-oauth.yaml"))
	require.NoError(t, err)
	manifestSource := strings.Replace(string(fixture), "spec:\n", "spec:\n  languages:\n    typescript: {version: v0.3.0, operations: [appendRows], triggers: [rowChanged]}\n", 1)
	manifestSource = strings.Replace(manifestSource, "  operations:\n", "  triggers:\n    - {name: rowChanged, goName: RowChanged, eventType: RowChangedEvent, configurationType: RowChangedConfiguration, description: Receive a changed row.}\n  operations:\n", 1)
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestSource), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(connectorDirectory, "go", "go.mod"), []byte("module example.com/sheets\n\ngo 1.24\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(connectorDirectory, "typescript", "package.json"), []byte(`{"name":"@superdurable/connector-google-sheets","version":"0.3.0"}`), 0o600))
	require.NoError(t, generate([]string{manifestPath}))
	require.NoError(t, generate([]string{"--check", manifestPath}))
	contents, err := os.ReadFile(filepath.Join(connectorDirectory, "typescript", "dex-connector.json"))
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(contents, &document))
	require.NoError(t, compileJSONSchemaFile(t, filepath.Join("..", "..", "schema", "dex-connector.schema.json")).Validate(document))
	factories := document["factories"].([]any)
	require.Len(t, factories, 2)
	require.Equal(t, "appendRows", factories[0].(map[string]any)["operationId"])
	require.Equal(t, map[string]any{
		"name": "defineRowChangedTriggerBinding", "kind": "trigger", "triggerName": "rowChanged",
		"fields": map[string]any{"connectionName": "connectionName", "bindingName": "bindingName", "configurationUI": "configurationUI"},
	}, factories[1])
	unknownTrigger := strings.Replace(manifestSource, "triggers: [rowChanged]}", "triggers: [rowChanged, rowRemoved]}", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(unknownTrigger), 0o600))
	require.ErrorContains(t, generate([]string{manifestPath}), "spec.languages.typescript.triggers names unknown rowRemoved")
}

func TestGeneratedLanguageManifestCarriesInputSchemaAndAgentReview(t *testing.T) {
	manifestPath, packageManifestPath := copyTypeScriptGoldenConnector(t)
	require.NoError(t, generate([]string{manifestPath}))
	factory := generatedFactory(t, packageManifestPath, "getItem")
	require.Equal(t, "GetItemInput", factory["inputType"])
	require.NotContains(t, factory, "agent", "an unreviewed operation is not offered to agents")
	inputSchema := factory["inputSchema"].(map[string]any)
	require.Equal(t, "object", inputSchema["type"])
	require.Contains(t, inputSchema["properties"], "id")
	operationDescription := factory["description"]
	require.NotEmpty(t, operationDescription, "the description falls back to the operation description")

	source, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	reviewed := strings.Replace(string(source), "      inputType: GetItemInput\n", "      inputType: GetItemInput\n      agent: {exposed: true, approval: recommended, branches: {notFound: rejected}, description: Read one <item>.}\n", 1)
	require.NotEqual(t, string(source), reviewed)
	require.ErrorContains(t, generateWithManifest(t, manifestPath, strings.Replace(reviewed, "exposed: true,", "exposed: true, retry: safe,", 1)), "agent.retry is only for mutations")
	require.ErrorContains(t, generateWithManifest(t, manifestPath, strings.Replace(reviewed, "{notFound: rejected}", "{notFuond: rejected}", 1)), "agent.branches.notFuond is not a branch of the operation")
	require.NoError(t, os.WriteFile(manifestPath, []byte(reviewed), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated file is stale", "drift in the agent block fails the check")
	require.NoError(t, generate([]string{manifestPath}))
	require.NoError(t, generate([]string{"--check", manifestPath}))
	factory = generatedFactory(t, packageManifestPath, "getItem")
	require.Equal(t, map[string]any{"exposed": true, "approval": "recommended", "branches": map[string]any{"notFound": "rejected"}}, factory["agent"])
	require.Equal(t, "Read one <item>.", factory["description"])
	contents, err := os.ReadFile(packageManifestPath)
	require.NoError(t, err)
	require.Contains(t, string(contents), "<item>", "descriptions keep their characters")

	declined := strings.Replace(string(source), "      inputType: GetItemInput\n", "      inputType: GetItemInput\n      agent: {exposed: false}\n", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(declined), 0o600))
	require.NoError(t, generate([]string{manifestPath}))
	factory = generatedFactory(t, packageManifestPath, "getItem")
	require.Equal(t, map[string]any{"exposed": false}, factory["agent"])
	require.Equal(t, operationDescription, factory["description"])
}

func TestGeneratedLanguageManifestRejectsInputModelThatGoLacks(t *testing.T) {
	manifestPath, _ := copyTypeScriptGoldenConnector(t)
	source, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	renamed := strings.Replace(string(source), "inputType: GetItemInput", "inputType: MissingInput", 1)
	require.NotEqual(t, string(source), renamed)
	require.NoError(t, os.WriteFile(manifestPath, []byte(renamed), 0o600))
	require.ErrorContains(t, generate([]string{manifestPath}), "MissingInput")
}

func TestDexConnectorOperationAgentCarriesRetry(t *testing.T) {
	exposed := true
	operation := schema.Operation{Agent: &schema.OperationAgent{Exposed: &exposed, Retry: schema.OperationAgentRetrySafe, Branches: map[string]string{"lost": "unknown"}}}
	require.Equal(t, &dexConnectorAgent{Exposed: true, Retry: "safe", Branches: map[string]string{"lost": "unknown"}}, dexConnectorOperationAgent(operation))
}

func generateWithManifest(t *testing.T, manifestPath, source string) error {
	t.Helper()
	require.NoError(t, os.WriteFile(manifestPath, []byte(source), 0o600))
	return generate([]string{manifestPath})
}

func copyTypeScriptGoldenConnector(t *testing.T) (string, string) {
	t.Helper()
	repository := t.TempDir()
	require.NoError(t, os.CopyFS(repository, os.DirFS(filepath.Join("testdata", "typescript-golden"))))
	connectorDirectory := filepath.Join(repository, "connectors", "hackernews")
	return filepath.Join(connectorDirectory, "connector.yaml"), filepath.Join(connectorDirectory, "typescript", "dex-connector.json")
}

func generatedFactory(t *testing.T, packageManifestPath, operationID string) map[string]any {
	t.Helper()
	contents, err := os.ReadFile(packageManifestPath)
	require.NoError(t, err)
	var document struct {
		Factories []map[string]any `json:"factories"`
	}
	require.NoError(t, json.Unmarshal(contents, &document))
	for _, factory := range document.Factories {
		if factory["operationId"] == operationID {
			return factory
		}
	}
	require.FailNow(t, "operation is missing", operationID)
	return nil
}
