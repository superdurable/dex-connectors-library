// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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

// Exposed catalog operations match each dex-connector.json listing them, whether or not Go adopted agent tools.
func TestCatalogAgentToolMetadataMatchesLanguagePackageManifests(t *testing.T) {
	var comparedFactories atomic.Int64
	// The group returns only after its parallel connector subtests finish.
	t.Run("connectors", func(t *testing.T) {
		for _, directory := range registeredConnectorDirectories(t) {
			t.Run(directory, func(t *testing.T) {
				t.Parallel()
				manifestPath := filepath.Join("..", "..", filepath.FromSlash(directory), "connector.yaml")
				manifest, err := load(manifestPath)
				require.NoError(t, err)
				if len(codegen.ExposedAgentOperations(manifest)) == 0 {
					return
				}
				operations, err := agentToolOperations(manifest, newConnectorGoPackage(manifestPath))
				require.NoError(t, err, directory)
				for path, contents := range publishedLanguageManifests(t, directory) {
					comparedFactories.Add(int64(requireAgentToolsMatchLanguageManifest(t, path, contents, manifest, operations)))
				}
				hasAdopted, err := hasAdoptedGoAgentTools(manifestPath, manifest)
				require.NoError(t, err, directory)
				_, statErr := os.Stat(agentToolsOutputPath(manifestPath))
				require.Equal(t, hasAdopted, statErr == nil, "%s: the agent tools file exists exactly when the Go module has adopted them", directory)
			})
		}
	})
	require.Positive(t, comparedFactories.Load(), "the catalog exposes operations that the language packages list")
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
	operations, err := agentToolOperations(manifest, newConnectorGoPackage(manifestPath))
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

// The gate trusts only a released sdkgo requirement; AGENTS.md forbids replace and non-release versions anyway.
func TestAgentToolsRejectAReplacedOrUnreleasedSDK(t *testing.T) {
	for name, test := range map[string]struct {
		version string
		replace string
		problem string
	}{
		"replace by path":    {version: "v0.35.0", replace: " => ../../../sdkgo", problem: "replaces github.com/superdurable/dex-connectors-library/sdkgo"},
		"replace by version": {version: "v0.35.0", replace: " => " + connectorSDKModulePath + " v0.30.0", problem: "replaces github.com/superdurable/dex-connectors-library/sdkgo"},
		"pseudo-version":     {version: "v0.35.1-0.20261101000000-abcdefabcdef", problem: "is not a released version"},
		"prerelease":         {version: "v0.35.0-rc.1", problem: "is not a released version"},
	} {
		t.Run(name, func(t *testing.T) {
			manifestPath, _ := copyTypeScriptGoldenConnector(t)
			goDirectory := filepath.Join(filepath.Dir(manifestPath), "go")
			source, err := os.ReadFile(manifestPath)
			require.NoError(t, err)
			exposed := strings.Replace(string(source), "      inputType: ListStoryIDsInput\n", "      inputType: ListStoryIDsInput\n      agent: {exposed: true}\n", 1)
			require.NoError(t, os.WriteFile(manifestPath, []byte(exposed), 0o600))
			setConnectorSDKRequirement(t, goDirectory, test.version)
			if test.replace != "" {
				goModulePath := filepath.Join(goDirectory, "go.mod")
				contents, err := os.ReadFile(goModulePath)
				require.NoError(t, err)
				contents = append(contents, []byte("\nreplace "+connectorSDKModulePath+test.replace+"\n")...)
				require.NoError(t, os.WriteFile(goModulePath, contents, 0o600))
			}

			require.ErrorContains(t, generate([]string{manifestPath}), test.problem)
			require.ErrorContains(t, run([]string{"validate", manifestPath}), test.problem)
			require.NoFileExists(t, filepath.Join(goDirectory, codegen.AgentToolsOutputFile))
		})
	}
}

// A Go-only exposed operation must not rename the models that language-listed operations publish.
func TestGoOnlyAgentToolKeepsLanguageInputSchemas(t *testing.T) {
	t.Setenv("GOWORK", "off")
	connectorDirectory := t.TempDir()
	goDirectory := filepath.Join(connectorDirectory, "go")
	for path, contents := range map[string]string{
		"go.mod": "module example.com/acme\n\ngo 1.24.0\n",
		"models.go": "package acme\n\nimport (\n\tfirst \"example.com/acme/first/provider\"\n\tsecond \"example.com/acme/second/provider\"\n)\n\n" +
			"// ListedInput is listed by the language packages.\ntype ListedInput struct {\n\tItem first.Item `json:\"item\"`\n}\n\n" +
			"// GoOnlyInput is exposed only from Go.\ntype GoOnlyInput struct {\n\tItem second.Item `json:\"item\"`\n}\n",
		filepath.Join("first", "provider", "item.go"):  "package provider\n\n// Item is the first provider item.\ntype Item struct {\n\tName string `json:\"name\"`\n}\n",
		filepath.Join("second", "provider", "item.go"): "package provider\n\n// Item is the second provider item.\ntype Item struct {\n\tCount int `json:\"count\"`\n}\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(goDirectory, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(goDirectory, path), []byte(contents), 0o600))
	}
	exposed := true
	operation := func(name, inputType string) schema.Operation {
		return schema.Operation{Name: name, GoName: "X" + name, InputType: inputType, Kind: "query", Description: "Read " + name + ".", Agent: &schema.OperationAgent{Exposed: &exposed}}
	}
	manifest := schema.Manifest{Metadata: schema.Metadata{Name: "acme"}}
	manifest.Spec.Operations = []schema.Operation{operation("getListed", "ListedInput"), operation("getGoOnly", "GoOnlyInput")}
	manifest.Spec.Languages = &schema.Languages{Python: &schema.LanguagePackage{Operations: []string{"getListed"}}}
	manifestPath := filepath.Join(connectorDirectory, "connector.yaml")
	// One loaded package serves every root set, as in generate; each graph must still be independent.
	goPackage := newConnectorGoPackage(manifestPath)

	operations, err := agentToolOperations(manifest, goPackage)

	require.NoError(t, err)
	require.Len(t, operations, 2)
	languageModels, err := languageOperationInputModels(manifest, goPackage)
	require.NoError(t, err)
	languageSchema, err := languageModels.JSONSchema("ListedInput")
	require.NoError(t, err)
	encodedLanguageSchema, err := encodeCompactJSON(languageSchema)
	require.NoError(t, err)
	require.Equal(t, string(encodedLanguageSchema), string(operations[0].InputSchema))
	require.Contains(t, string(operations[0].InputSchema), `"$defs":{"providerItem":`, "the language schema keeps the short model name")
	combinedModels, err := loadOperationInputModels(goPackage, manifest, map[string]bool{"getListed": true, "getGoOnly": true})
	require.NoError(t, err)
	combinedSchema, err := combinedModels.JSONSchema("ListedInput")
	require.NoError(t, err)
	encodedCombinedSchema, err := encodeCompactJSON(combinedSchema)
	require.NoError(t, err)
	require.NotEqual(t, string(encodedLanguageSchema), string(encodedCombinedSchema), "the fixture's model names collide when both are roots")
	require.Contains(t, string(operations[1].InputSchema), `"count"`)
	require.NotContains(t, string(operations[1].InputSchema), `"name"`)
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

// requireLocalSDKRelease requires an unpublished sdkgo version and serves it through a temporary go.work replace.
func requireLocalSDKRelease(t *testing.T, goDirectory, version string) {
	t.Helper()
	sdkDirectory, err := filepath.Abs(filepath.Join("..", "..", "sdkgo"))
	require.NoError(t, err)
	setConnectorSDKRequirement(t, goDirectory, version)
	workspacePath := filepath.Join(t.TempDir(), "go.work")
	workspace := "go 1.24.0\n\nuse " + strconv.Quote(goDirectory) + "\n\nreplace " + connectorSDKModulePath + " => " + strconv.Quote(sdkDirectory) + "\n"
	require.NoError(t, os.WriteFile(workspacePath, []byte(workspace), 0o600))
	t.Setenv("GOWORK", workspacePath)
	t.Setenv("GOFLAGS", "")
}

func setConnectorSDKRequirement(t *testing.T, goDirectory, version string) {
	t.Helper()
	goModulePath := filepath.Join(goDirectory, "go.mod")
	contents, err := os.ReadFile(goModulePath)
	require.NoError(t, err)
	required, err := requiredConnectorSDKVersion(goModulePath)
	require.NoError(t, err)
	updated := strings.Replace(string(contents), connectorSDKModulePath+" "+required, connectorSDKModulePath+" "+version, 1)
	require.NoError(t, os.WriteFile(goModulePath, []byte(updated), 0o600))
}
