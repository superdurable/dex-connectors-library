// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
)

func TestGenerateEmitsSendCapsOnlyWhenRequested(t *testing.T) {
	manifest, operations := loadAgentToolsFixture(t)
	connector, err := codegen.Generate(manifest)
	require.NoError(t, err)
	agentTools, err := codegen.GenerateAgentTools(manifest, operations)
	require.NoError(t, err)
	for _, generated := range [][]byte{connector, agentTools} {
		require.NotContains(t, string(generated), "RepeatOnUnknown")
		require.NotContains(t, string(generated), "MaxSends")
	}

	connector, err = codegen.Generate(manifest, codegen.WithRepeatOnUnknownSendCaps())
	require.NoError(t, err)
	require.Contains(t, string(connector), "RepeatOnUnknown: sdkgo.RepeatOnUnknownSafe,", "agent.retry safe resolves createWidget to safe")
	require.NotContains(t, string(connector), "MaxSends", "a safe operation has no send cap or override")
	require.Equal(t, 1, strings.Count(string(connector), "RepeatOnUnknown:"), "only the Mutation carries a tier")

	manifest.Spec.Operations[2].Agent.Retry = ""
	manifest.Spec.Operations[2].Execution.MaxSends = 4
	require.NoError(t, manifest.Validate())
	connector, err = codegen.Generate(manifest, codegen.WithRepeatOnUnknownSendCaps())
	require.NoError(t, err)
	require.Contains(t, string(connector), "RepeatOnUnknown: sdkgo.RepeatOnUnknownLimited,")
	require.Contains(t, string(connector), "MaxSends:        4,")
	require.Regexp(t, "MaxSends +int32 +`connector:\"maxSends\"`", string(connector))
	require.Contains(t, string(connector), "MaxSends:            config.MaxSends,")

	manifest.Spec.Operations[2].Execution.MaxSends = 0
	manifest.Spec.Operations[2].Execution.RepeatOnUnknown = "unsafe"
	require.NoError(t, manifest.Validate())
	connector, err = codegen.Generate(manifest, codegen.WithRepeatOnUnknownSendCaps())
	require.NoError(t, err)
	require.Contains(t, string(connector), "RepeatOnUnknown: sdkgo.RepeatOnUnknownUnsafe,")
	require.NotContains(t, string(connector), "MaxSends", "an unsafe operation rejects the override at compile time")
}

// TestGeneratedSendCapsBehave compiles against local sdkgo, whose factories and agent tool constructors check the generated values.
func TestGeneratedSendCapsBehave(t *testing.T) {
	manifest, operations := loadAgentToolsFixture(t)
	manifest.Spec.Operations[2].Agent.Retry = ""
	manifest.Spec.Operations[2].Execution.MaxSends = 4
	require.NoError(t, manifest.Validate())
	operations[1].Operation = manifest.Spec.Operations[2]
	connector, err := codegen.Generate(manifest, codegen.WithRepeatOnUnknownSendCaps())
	require.NoError(t, err)
	agentTools, err := codegen.GenerateAgentTools(manifest, operations, codegen.WithRepeatOnUnknownSendCaps())
	require.NoError(t, err)
	source, test, err := codegen.GenerateMockPackage(manifest, mocksModulePath)
	require.NoError(t, err)
	client, err := os.ReadFile(filepath.Join(mocksFixturePath, "client.go"))
	require.NoError(t, err)
	files := map[string][]byte{
		codegen.OutputFile:           connector,
		codegen.AgentToolsOutputFile: agentTools,
		"client.go":                  client,
		filepath.Join(codegen.MockPackageName(manifest), codegen.MockOutputFile):     source,
		filepath.Join(codegen.MockPackageName(manifest), codegen.MockTestOutputFile): test,
		"send_caps_test.go": []byte(`// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mockfixture_test

import (
	"testing"

	"example.com/mockfixture"
	"example.com/mockfixture/mockfixturemock"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGeneratedSendCaps(t *testing.T) {
	require.Equal(t, sdkgo.RepeatOnUnknownLimited, mockfixture.CreateWidgetDefinition.RepeatOnUnknown)
	require.Equal(t, int32(4), mockfixture.CreateWidgetDefinition.MaxSends)
	connection := mockfixturemock.New(t, "primary").Connection()
	config := mockfixture.CreateWidgetStepConfig[mockfixture.CreateWidgetInput]{
		StepType: "CreateWidget", Annotations: sdkgo.StepAnnotations{GroupID: "widgets", GroupLabel: "Widgets", Explanation: "Create a widget."},
		Connection: connection, ConnectionName: "primary",
		MapToOperationInput: func(input mockfixture.CreateWidgetInput) mockfixture.CreateWidgetInput { return input },
		Created:             sdkgo.GoTo(sdkgo.StepRef[mockfixture.CreateWidgetResult]("Done")),
	}
	require.Equal(t, int32(5), mockfixture.NewCreateWidgetStep(config).GetStepOptions().ExecuteRetry.MaximumAttempts)
	config.MaxSends = 6
	require.Equal(t, int32(7), mockfixture.NewCreateWidgetStep(config).GetStepOptions().ExecuteRetry.MaximumAttempts)

	tools, err := connection.AgentTools()
	require.NoError(t, err)
	getWidget, createWidget := tools[0].Metadata(), tools[1].Metadata()
	require.Empty(t, getWidget.RepeatOnUnknown)
	require.Zero(t, getWidget.MaxSends)
	require.Equal(t, sdkgo.RepeatOnUnknownLimited, createWidget.RepeatOnUnknown)
	require.Equal(t, int32(4), createWidget.MaxSends)
	require.False(t, createWidget.IsRetrySafe)
	require.Equal(t, sdkgo.AgentToolEffectNonIdempotentWrite, createWidget.Effect())
}
`),
	}

	output, err := runFixtureModuleTests(t, mocksModulePath, files)

	require.NoError(t, err, output)
	require.Contains(t, output, "ok  \t"+mocksModulePath+"\t")
}
