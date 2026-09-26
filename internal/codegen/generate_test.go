// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestGenerateIsDeterministicAndIncludesTypedOAuthCredentials(t *testing.T) {
	file, err := os.Open("../../schema/testdata/google-oauth.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	first, err := codegen.Generate(manifest)
	require.NoError(t, err)
	second, err := codegen.Generate(manifest)
	require.NoError(t, err)
	require.True(t, bytes.Equal(first, second))
	text := string(first)
	require.Contains(t, text, "type Config struct")
	require.Contains(t, text, "type Credentials struct")
	require.Contains(t, text, "AccessToken")
	require.Contains(t, text, "sdkgo.SecretString")
	require.Contains(t, text, "AppendRowsBranchUncertain")
	require.Contains(t, text, "type AppendRowsStepConfig[IN any] struct")
	require.Contains(t, text, "type AppendRowsResult = sdkgo.MutationResult[AppendRowsOutput]")
	require.Contains(t, text, "Annotations")
	require.Contains(t, text, "sdkgo.StepAnnotations")
	require.Contains(t, text, "`connector:\"annotations\"`")
	require.NotContains(t, text, ".HasStep()")
	require.Contains(t, text, "MapToOperationInput")
	require.Contains(t, text, "func(IN) AppendRowsInput")
	require.Contains(t, text, "`connector:\"mapToOperationInput\"`")
	require.Contains(t, text, "func NewAppendRowsStep[IN any]")
	require.Contains(t, text, "func NewLocalConnection(store *localconfig.Store, connectionName string")
	require.Contains(t, text, "ConnectionName")
	require.Contains(t, text, "`connector:\"connectionName\"`")
	require.Contains(t, text, "decodeLocalCredentials")
	require.Contains(t, text, "sdkgo.MutationFactoryConfigMarker")
	require.Contains(t, text, "`connector:\"connectorId=google-sheets-fixture\"`")
	require.Contains(t, text, "`connector:\"operationId=appendRows\"`")
	require.Contains(t, text, "type Environment string")
	require.Contains(t, text, `EnvironmentProduction Environment = "production"`)
	require.Contains(t, text, `[]string{"scope-b", "scope-a"}`)
	require.Contains(t, text, `map[string]string{"owner": "platform", "team": "onboarding"}`)
	require.NotContains(t, text, "access token default")
	require.NotContains(t, text, "ConnectorVersion")
	_, err = parser.ParseFile(token.NewFileSet(), "zz_generated_connector.go", strings.NewReader(text), parser.AllErrors)
	require.NoError(t, err)
}

func TestGenerateIncludesTypedProviderTriggers(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: messages, displayName: Messages, description: Message events., company: Example, version: v0.1.0}
spec:
  provider: messages
  codegen: {go: {package: messages}}
  configuration: {fields: []}
  auth: {type: none, connectionKind: none, fields: []}
  studio:
    setup: {entrypoint: index.html, hostApiRange: ">=0.2.0 <0.3.0", backendCapabilities: [channels.list], mockScenarios: [ready], icon: icon.svg}
    units:
      - id: channelPicker
        goName: ChannelPicker
        description: Select a channel.
        outputs: [{name: channelId, goName: ChannelID, type: string}]
  triggers:
    - {name: channelThreadCreated, goName: ChannelThreadCreated, eventType: MessageEvent, configurationType: ChannelThreadConfiguration, description: Receive a top-level channel message.}
    - {name: threadReplyCreated, goName: ThreadReplyCreated, eventType: MessageEvent, configurationType: ThreadReplyConfiguration, description: Receive a thread reply.}
  operations:
    - name: getMessage
      goName: GetMessage
      inputType: GetMessageInput
      outputType: GetMessageOutput
      kind: query
      description: Read a message.
      idempotency: none
      branches:
        - {id: read, goName: Read, description: The message was read.}
        - {id: defect, goName: Defect, description: Invalid input., optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.NoError(t, err)
	generated, err := codegen.Generate(manifest)
	require.NoError(t, err)
	text := string(generated)
	require.Contains(t, text, "`connector:\"branch=defect,optional\"`")
	require.Contains(t, text, "{ID: GetMessageBranchDefect, Description: \"Invalid input.\", Optional: true}")
	require.Contains(t, text, "if config.Defect.HasStep()")
	require.Contains(t, text, "if config.Read.HasStep()")
	require.Contains(t, text, "sdkgo.TriggerFactoryConfigMarker")
	require.Contains(t, text, "func NewChannelThreadCreatedTrigger")
	require.Contains(t, text, "func DefineChannelThreadCreatedTriggerBinding")
	require.Contains(t, text, "func NewLocalThreadReplyCreatedTrigger")
	require.NotContains(t, text, "triggerKind")
	require.Contains(t, text, "`connector:\"bindingName\"`")
	require.Contains(t, text, "UIUnitChannelPicker")
	require.Contains(t, text, `= "channelPicker"`)
	require.Contains(t, text, "UIChannelPickerPortChannelID")
	require.Contains(t, text, `= "channelId"`)
	require.Contains(t, text, "sdkgo.ConnectorConfigurationUI `connector:\"configurationUI\"`")
	require.Contains(t, text, "ConfigurationUI: &config.ConfigurationUI")
	require.Contains(t, text, "ConfigurationUI: config.ConfigurationUI")
	_, err = parser.ParseFile(token.NewFileSet(), "zz_generated_connector.go", strings.NewReader(text), parser.AllErrors)
	require.NoError(t, err)
}
