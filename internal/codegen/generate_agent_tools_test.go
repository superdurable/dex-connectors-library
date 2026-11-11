// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

const agentToolsFixturePath = "testdata/agenttools"

func TestGenerateAgentToolsMatchesGolden(t *testing.T) {
	manifest, operations := loadAgentToolsFixture(t)
	generated, err := codegen.GenerateAgentTools(manifest, operations)
	require.NoError(t, err)

	goldenPath := filepath.Join(agentToolsFixturePath, codegen.AgentToolsOutputFile)
	if *shouldUpdateGoldenFiles {
		require.NoError(t, os.WriteFile(goldenPath, generated, 0o644))
	}
	golden, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, string(golden), string(generated), "run go test ./internal/codegen -run MatchesGolden -update-golden")
}

// TestGeneratedAgentToolsBehave compiles against local sdkgo, whose constructors check every generated metadata value.
func TestGeneratedAgentToolsBehave(t *testing.T) {
	manifest, operations := loadAgentToolsFixture(t)
	agentTools, err := codegen.GenerateAgentTools(manifest, operations)
	require.NoError(t, err)
	files := generateMocksFixture(t)
	files[codegen.AgentToolsOutputFile] = agentTools
	for source, target := range map[string]string{
		filepath.Join(mocksFixturePath, "client.go"):                "client.go",
		filepath.Join(mocksFixturePath, "mock_test.go"):             "mock_test.go",
		filepath.Join(agentToolsFixturePath, "agent_tools_test.go"): "agent_tools_test.go",
	} {
		contents, readErr := os.ReadFile(source)
		require.NoError(t, readErr)
		files[target] = contents
	}

	output, err := runFixtureModuleTests(t, mocksModulePath, files)

	require.NoError(t, err, output)
	require.Contains(t, output, "ok  \t"+mocksModulePath+"\t")
}

func TestGenerateAgentToolsEmitsBranchOutcomesInManifestOrder(t *testing.T) {
	manifest, operations := loadAgentToolsFixture(t)
	for range 20 {
		generated, err := codegen.GenerateAgentTools(manifest, operations)
		require.NoError(t, err)
		text := string(generated)
		require.Less(t, strings.Index(text, "CreateWidgetBranchUncertain: sdkgo.AgentBranchOutcomeUnknown"), strings.Index(text, "CreateWidgetBranchDefect:    sdkgo.AgentBranchOutcomeRejected"))
	}
}

func TestGenerateAgentToolsRejectsOperationsThatDoNotMatchTheManifest(t *testing.T) {
	manifest, operations := loadAgentToolsFixture(t)
	for name, test := range map[string]struct {
		operations []codegen.AgentToolOperation
		problem    string
	}{
		"missing operation":  {operations[:1], "exposes 2 operations to agents, but 1 agent tools were given"},
		"reordered":          {[]codegen.AgentToolOperation{operations[1], operations[0]}, `agent tool 0 is "createWidget", but the manifest exposes "getWidget" there`},
		"schema not object":  {[]codegen.AgentToolOperation{operations[0], withInputSchema(operations[1], `[]`)}, "createWidget input schema must be a JSON object"},
		"blank description":  {[]codegen.AgentToolOperation{operations[0], withDescription(operations[1], " ")}, "createWidget requires a tool name and a description"},
		"no exposed in list": {nil, "exposes 2 operations to agents, but 0 agent tools were given"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := codegen.GenerateAgentTools(manifest, test.operations)
			require.ErrorContains(t, err, test.problem)
		})
	}
	unexposed := loadMocksManifest(t)
	_, err := codegen.GenerateAgentTools(unexposed, nil)
	require.ErrorContains(t, err, "mock-fixture exposes no operation to agents")
}

func TestExposedAgentOperationsSkipsDeclinedReviews(t *testing.T) {
	manifest, _ := loadAgentToolsFixture(t)
	declined := false
	manifest.Spec.Operations[1].Agent = &schema.OperationAgent{Exposed: &declined}

	exposed := codegen.ExposedAgentOperations(manifest)

	require.Len(t, exposed, 2)
	require.Equal(t, "getWidget", exposed[0].Name)
	require.Equal(t, "createWidget", exposed[1].Name)
}

// loadAgentToolsFixture exposes getWidget and createWidget of the mocks fixture; listWidgets stays unreviewed.
func loadAgentToolsFixture(t *testing.T) (schema.Manifest, []codegen.AgentToolOperation) {
	t.Helper()
	manifest := loadMocksManifest(t)
	exposed := true
	manifest.Spec.Operations[0].Agent = &schema.OperationAgent{
		Exposed: &exposed, Description: "Read one widget by its ID.",
		Branches: map[string]string{"notFound": schema.OperationAgentBranchRejected, "defect": schema.OperationAgentBranchRejected},
	}
	manifest.Spec.Operations[2].Agent = &schema.OperationAgent{
		Exposed: &exposed, Approval: schema.OperationAgentApprovalRecommended, Retry: schema.OperationAgentRetrySafe,
		Branches: map[string]string{"defect": schema.OperationAgentBranchRejected, "uncertain": schema.OperationAgentBranchUnknown},
	}
	require.NoError(t, manifest.Validate())
	return manifest, []codegen.AgentToolOperation{
		{
			Operation: manifest.Spec.Operations[0], ToolName: "mock-fixture_get_widget", Description: "Read one widget by its ID.",
			InputSchema: json.RawMessage(`{"additionalProperties":false,"description":"Use <ID> and ` + "`backticks`" + `.","properties":{"id":{"type":"string"}},"required":["id"],"type":"object"}`),
		},
		{
			Operation: manifest.Spec.Operations[2], ToolName: "mock-fixture_create_widget", Description: "Create one widget.",
			InputSchema: json.RawMessage(`{"additionalProperties":false,"properties":{"name":{"type":"string"}},"required":["name"],"type":"object"}`),
		},
	}
}

func withInputSchema(operation codegen.AgentToolOperation, inputSchema string) codegen.AgentToolOperation {
	operation.InputSchema = json.RawMessage(inputSchema)
	return operation
}

func withDescription(operation codegen.AgentToolOperation, description string) codegen.AgentToolOperation {
	operation.Description = description
	return operation
}
