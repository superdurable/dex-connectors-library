// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

// Agent tool names share the Python factory snake case and the vectors sdkgo checks ToolName against.
func TestAgentToolNamesMatchPythonFactoriesAndSDKVectors(t *testing.T) {
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
	manifest := schema.Manifest{Metadata: schema.Metadata{Name: "hacker-news-daily"}}
	for _, vector := range document.Vectors {
		toolName := agentToolName(manifest, schema.Operation{Name: vector.OperationID})
		require.Equal(t, "hacker-news-daily_"+vector.SnakeCase, toolName, vector.OperationID)
		require.Equal(t, toolName, "hacker-news-daily_"+pythonFactoryOperationName(t, pythonStepFactoryName(vector.OperationID)), vector.OperationID)
	}
}

// Every exposed operation in the catalog, whether or not its Go module has adopted agent tools, carries the
// same reviewed metadata in Go as in the Python and TypeScript dex-connector.json that list it.
func TestCatalogAgentToolMetadataMatchesLanguagePackageManifests(t *testing.T) {
	comparedFactories := 0
	for _, directory := range registeredConnectorDirectories(t) {
		manifestPath := filepath.Join("..", "..", filepath.FromSlash(directory), "connector.yaml")
		manifest, err := load(manifestPath)
		require.NoError(t, err)
		if len(codegen.ExposedAgentOperations(manifest)) == 0 {
			continue
		}
		operations, err := agentToolOperations(manifestPath, manifest)
		require.NoError(t, err, directory)
		for path, contents := range publishedLanguageManifests(t, directory) {
			comparedFactories += requireAgentToolsMatchLanguageManifest(t, path, contents, manifest, operations)
		}
		hasAdopted, err := hasAdoptedGoAgentTools(manifestPath, manifest)
		require.NoError(t, err, directory)
		_, statErr := os.Stat(agentToolsOutputPath(manifestPath))
		require.Equal(t, hasAdopted, statErr == nil, "%s: the agent tools file exists exactly when the Go module has adopted them", directory)
	}
	require.Positive(t, comparedFactories, "the catalog exposes operations that the language packages list")
}

func TestGenerateAgentToolsOnlyAfterTheModuleRequiresTheSDKRelease(t *testing.T) {
	manifestPath, packageManifestPath := copyTypeScriptGoldenConnector(t)
	goDirectory := filepath.Join(filepath.Dir(manifestPath), "go")
	agentToolsPath := filepath.Join(goDirectory, codegen.AgentToolsOutputFile)
	source, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	exposed := strings.Replace(string(source), "      inputType: ListStoryIDsInput\n",
		"      inputType: ListStoryIDsInput\n      agent: {exposed: true, branches: {providerRejected: rejected, invalidResponse: rejected, defect: rejected}, description: List Hacker News story IDs.}\n", 1)
	require.NotEqual(t, string(source), exposed)
	require.NoError(t, os.WriteFile(manifestPath, []byte(exposed), 0o600))

	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, agentToolsPath, "a module that requires sdkgo v0.32.0 cannot compile agent tools")
	require.NoError(t, generate([]string{"--check", manifestPath}))
	require.NoError(t, run([]string{"validate", manifestPath}))
	require.NoError(t, os.WriteFile(agentToolsPath, []byte("package hackernews\n"), 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated agent tools are stale")
	require.ErrorContains(t, run([]string{"validate", manifestPath}), "github.com/superdurable/dex-connectors-library/sdkgo v0.35.0 or later in go/go.mod")
	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, agentToolsPath)

	requireLocalSDKRelease(t, goDirectory, codegen.AgentToolsMinimumSDKVersion)
	require.NoError(t, generate([]string{manifestPath}))
	require.FileExists(t, agentToolsPath)
	require.NoError(t, generate([]string{"--check", manifestPath}))
	require.NoError(t, run([]string{"validate", manifestPath}))
	generated, err := os.ReadFile(agentToolsPath)
	require.NoError(t, err)
	require.Contains(t, string(generated), `ToolName:              "hacker-news-daily_list_story_ids",`)
	require.Contains(t, string(generated), `Description:           "List Hacker News story IDs.",`)
	require.Regexp(t, `ListStoryIDsBranchInvalidResponse: +sdkgo\.AgentBranchOutcomeRejected,`, string(generated))
	require.NotContains(t, string(generated), "getItemAgentToolMetadata", "an unreviewed operation is not an agent tool")
	manifest, err := load(manifestPath)
	require.NoError(t, err)
	operations, err := agentToolOperations(manifestPath, manifest)
	require.NoError(t, err)
	packageManifest, err := os.ReadFile(packageManifestPath)
	require.NoError(t, err)
	require.Equal(t, 1, requireAgentToolsMatchLanguageManifest(t, packageManifestPath, packageManifest, manifest, operations))
	vet := exec.Command("go", "vet", "./...")
	vet.Dir = goDirectory
	output, err := vet.CombinedOutput()
	require.NoError(t, err, string(output))

	require.NoError(t, os.WriteFile(manifestPath, source, 0o600))
	require.ErrorContains(t, generate([]string{"--check", manifestPath}), "generated agent tools are stale")
	require.NoError(t, generate([]string{manifestPath}))
	require.NoFileExists(t, agentToolsPath, "withdrawing the last exposure removes the file")
}

func TestAgentToolsRequireTheModuleToDeclareTheSDK(t *testing.T) {
	manifestPath, _ := copyTypeScriptGoldenConnector(t)
	source, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	exposed := strings.Replace(string(source), "      inputType: ListStoryIDsInput\n", "      inputType: ListStoryIDsInput\n      agent: {exposed: true}\n", 1)
	require.NoError(t, os.WriteFile(manifestPath, []byte(exposed), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(manifestPath), "go", "go.mod"), []byte("module example.com/hackernews\n\ngo 1.24.0\n"), 0o600))

	require.ErrorContains(t, generate([]string{manifestPath}), "exposes operations to agents but does not require github.com/superdurable/dex-connectors-library/sdkgo")
}

// requireAgentToolsMatchLanguageManifest compares every listed exposed operation and returns how many it compared.
func requireAgentToolsMatchLanguageManifest(t *testing.T, path string, contents []byte, manifest schema.Manifest, operations []codegen.AgentToolOperation) int {
	t.Helper()
	var document struct {
		ConnectorID string `json:"connectorId"`
		Factories   []struct {
			Name        string          `json:"name"`
			OperationID string          `json:"operationId"`
			InputType   string          `json:"inputType"`
			Description string          `json:"description"`
			Agent       json.RawMessage `json:"agent"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"factories"`
	}
	require.NoError(t, json.Unmarshal(contents, &document), path)
	require.Equal(t, manifest.Metadata.Name, document.ConnectorID, path)
	isPython := strings.Contains(filepath.ToSlash(path), "/python/")
	compared := 0
	for _, operation := range operations {
		for _, factory := range document.Factories {
			if factory.OperationID != operation.Operation.Name {
				continue
			}
			compared++
			require.Equal(t, factory.Description, operation.Description, "%s %s", path, factory.OperationID)
			require.Equal(t, factory.InputType, operation.Operation.InputType, "%s %s", path, factory.OperationID)
			require.JSONEq(t, string(factory.InputSchema), string(operation.InputSchema), "%s %s", path, factory.OperationID)
			expectedAgent, err := json.Marshal(dexConnectorOperationAgent(operation.Operation))
			require.NoError(t, err)
			require.JSONEq(t, string(factory.Agent), string(expectedAgent), "%s %s", path, factory.OperationID)
			var agent struct {
				Exposed bool `json:"exposed"`
			}
			require.NoError(t, json.Unmarshal(factory.Agent, &agent), path)
			require.True(t, agent.Exposed, "%s %s", path, factory.OperationID)
			if isPython {
				require.Equal(t, operation.ToolName, document.ConnectorID+"_"+pythonFactoryOperationName(t, factory.Name), "%s %s", path, factory.OperationID)
			}
		}
	}
	return compared
}

func pythonFactoryOperationName(t *testing.T, factoryName string) string {
	t.Helper()
	operationName, hasPrefix := strings.CutPrefix(factoryName, "new_")
	operationName, hasSuffix := strings.CutSuffix(operationName, "_step")
	require.True(t, hasPrefix && hasSuffix, factoryName)
	return operationName
}

// requireLocalSDKRelease makes a copied connector require version of sdkgo, served from the local source.
func requireLocalSDKRelease(t *testing.T, goDirectory, version string) {
	t.Helper()
	sdkDirectory, err := filepath.Abs(filepath.Join("..", "..", "sdkgo"))
	require.NoError(t, err)
	goModulePath := filepath.Join(goDirectory, "go.mod")
	contents, err := os.ReadFile(goModulePath)
	require.NoError(t, err)
	required, err := requiredConnectorSDKVersion(goModulePath)
	require.NoError(t, err)
	updated := strings.Replace(string(contents), connectorSDKModulePath+" "+required, connectorSDKModulePath+" "+version, 1)
	updated += "\nreplace " + connectorSDKModulePath + " => " + sdkDirectory + "\n"
	require.NoError(t, os.WriteFile(goModulePath, []byte(updated), 0o600))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
}
