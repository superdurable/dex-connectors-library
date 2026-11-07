// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

// Conformance inputs reach every language model, so they are independent evidence of the wire names.
func TestPublishedInputSchemasAcceptConformanceInputs(t *testing.T) {
	packageManifestSchema := compileJSONSchemaFile(t, filepath.Join("..", "..", "schema", "dex-connector.schema.json"))
	validatedInputs := 0
	for _, directory := range registeredConnectorDirectories(t) {
		manifests := publishedLanguageManifests(t, directory)
		require.NotEmpty(t, manifests, directory)
		inputSchemas := map[string]*jsonschema.Schema{}
		publishedSchemas := map[string]string{}
		for path, contents := range manifests {
			document := decodeJSONDocument(t, contents)
			require.NoError(t, packageManifestSchema.Validate(document), path)
			var manifest struct {
				Factories []struct {
					OperationID string          `json:"operationId"`
					InputType   string          `json:"inputType"`
					Description string          `json:"description"`
					InputSchema json.RawMessage `json:"inputSchema"`
				} `json:"factories"`
			}
			require.NoError(t, json.Unmarshal(contents, &manifest), path)
			for _, factory := range manifest.Factories {
				require.NotEmpty(t, factory.InputType, "%s %s", path, factory.OperationID)
				require.NotEmpty(t, factory.Description, "%s %s", path, factory.OperationID)
				if published, seen := publishedSchemas[factory.OperationID]; seen {
					require.JSONEq(t, published, string(factory.InputSchema), "Python and TypeScript differ for %s %s", directory, factory.OperationID)
					continue
				}
				publishedSchemas[factory.OperationID] = string(factory.InputSchema)
				inputSchemas[factory.OperationID] = compileJSONSchemaDocument(t, directory+"/"+factory.OperationID, factory.InputSchema)
			}
		}
		scenarioPath := filepath.Join("..", "..", "conformance", "connectors", conformanceDirectoryName(directory), "scenarios.json")
		contents, err := os.ReadFile(scenarioPath)
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		var fixture struct {
			Scenarios []struct {
				Name      string          `json:"name"`
				Operation string          `json:"operation"`
				Input     json.RawMessage `json:"input"`
				Expected  struct {
					Branch string `json:"branch"`
				} `json:"expected"`
			} `json:"scenarios"`
		}
		require.NoError(t, json.Unmarshal(contents, &fixture), scenarioPath)
		for _, scenario := range fixture.Scenarios {
			inputSchema := inputSchemas[scenario.Operation]
			// A defect scenario sends input that a connector must refuse, so a schema may refuse it too.
			if inputSchema == nil || scenario.Expected.Branch == "defect" {
				continue
			}
			t.Run(directory+"/"+scenario.Name, func(t *testing.T) {
				require.NoError(t, inputSchema.Validate(decodeJSONDocument(t, scenario.Input)))
			})
			validatedInputs++
		}
	}
	require.Greater(t, validatedInputs, 1000)
}

func TestFirstReviewedOperationsCarryTheirRetryClaims(t *testing.T) {
	for _, testCase := range []struct {
		directory, operation string
		agent                map[string]any
	}{
		{"connectors/stripe", "createRefund", map[string]any{"exposed": true, "approval": "recommended", "retry": "safe"}},
		{"connectors/slack", "postThreadReply", map[string]any{"exposed": true}},
	} {
		for path, contents := range publishedLanguageManifests(t, testCase.directory) {
			var manifest struct {
				Factories []map[string]any `json:"factories"`
			}
			require.NoError(t, json.Unmarshal(contents, &manifest), path)
			for _, factory := range manifest.Factories {
				if factory["operationId"] == testCase.operation {
					require.Equal(t, testCase.agent, factory["agent"], path)
				}
			}
		}
	}
}

// Dex analyzers ignore an unknown agent field, so this closed schema is what catches an author's typo.
func TestPackageManifestSchemaRejectsWhatTheAnalyzerRejectsAndAgentTypos(t *testing.T) {
	schemaDocument := compileJSONSchemaFile(t, filepath.Join("..", "..", "schema", "dex-connector.schema.json"))
	operationFactory := func(kind, agent string) any {
		return decodeJSONDocument(t, []byte(`{"connectorId":"sample","releaseTag":"connectors/sample/go/v1.0.0","package":{"ecosystem":"npm","name":"sample","version":"1.0.0"},
			"factories":[{"name":"newStep","kind":"`+kind+`","operationId":"run","inputType":"Input","inputSchema":{"type":"object"},"agent":`+agent+`,
			"fields":{"stepType":"stepType","annotations":"annotations","connectionName":"connectionName"},"branches":[{"id":"done","field":"done"}]}]}`))
	}
	require.NoError(t, schemaDocument.Validate(operationFactory("mutation", `{"exposed":true,"retry":"safe"}`)))
	for name, document := range map[string]any{
		"unknown agent field": operationFactory("mutation", `{"exposed":true,"retyr":"safe"}`),
		"retry on a query":    operationFactory("query", `{"exposed":true,"retry":"safe"}`),
		"retry when declined": operationFactory("mutation", `{"exposed":false,"retry":"safe"}`),
		"unknown retry value": operationFactory("mutation", `{"exposed":true,"retry":"always"}`),
		"null agent":          operationFactory("mutation", `null`),
	} {
		require.Error(t, schemaDocument.Validate(document), name)
	}
}

func publishedLanguageManifests(t *testing.T, directory string) map[string][]byte {
	t.Helper()
	root := filepath.Join("..", "..", filepath.FromSlash(directory))
	paths, err := filepath.Glob(filepath.Join(root, "python", "src", "dex_connectors", "*", "dex-connector.json"))
	require.NoError(t, err)
	paths = append(paths, filepath.Join(root, "typescript", "dex-connector.json"))
	manifests := make(map[string][]byte, len(paths))
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		manifests[path] = contents
	}
	return manifests
}

func conformanceDirectoryName(directory string) string {
	return directory[len("connectors/"):]
}

func compileJSONSchemaFile(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return compileJSONSchemaDocument(t, filepath.Base(path), contents)
}

// Compiling checks the document against the draft 2020-12 metaschema.
func compileJSONSchemaDocument(t *testing.T, name string, contents []byte) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	resource := "https://connectors.dex.dev/schema/" + name
	require.NoError(t, compiler.AddResource(resource, decodeJSONDocument(t, contents)), name)
	schema, err := compiler.Compile(resource)
	require.NoError(t, err, name)
	return schema
}

func decodeJSONDocument(t *testing.T, contents []byte) any {
	t.Helper()
	var document any
	require.NoError(t, json.Unmarshal(contents, &document))
	return document
}
