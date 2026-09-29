// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen_test

import (
	"bytes"
	"flag"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/codegen"
	"github.com/superdurable/dex-connectors-library/schema"
)

var shouldUpdateGoldenFiles = flag.Bool("update-golden", false, "rewrite code generation golden files")

const (
	multipleAuthSelectionManifestPath = "../../schema/testdata/multiple-auth-selection.yaml"
	multipleAuthSelectionFixturePath  = "testdata/multipleauthselection"
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

func TestGenerateRejectsInvalidExecutionDuration(t *testing.T) {
	file, err := os.Open("../../schema/testdata/google-oauth.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	manifest.Spec.Operations[0].Execution.ExecuteMethodTimeout = "eventually"

	_, err = codegen.Generate(manifest)

	require.ErrorContains(t, err, "execute method timeout")
}

func TestGenerateValidatesOnlyTheSelectedAuthMethod(t *testing.T) {
	file, err := os.Open("../../schema/testdata/multi-auth.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	generated, err := codegen.Generate(manifest)
	require.NoError(t, err)
	text := string(generated)
	require.Contains(t, text, "AuthMethodID")
	require.Contains(t, text, "`json:\"auth_method\"`")
	require.Contains(t, text, `case "google-oauth":`)
	require.Contains(t, text, `case "workspace-service-account":`)
	require.Contains(t, text, "credential access_token is required")
	require.Contains(t, text, "credential service_account_key is required")
	require.Contains(t, text, "credential auth_method is invalid")
	require.Contains(t, text, "localconfig.NewRefreshingCredentialProvider")
	require.Contains(t, text, "func encodeLocalCredentials(credentials Credentials) (json.RawMessage, error)")
	require.Contains(t, text, "credentials.OAuthClientSecret.Reveal()")
	require.Contains(t, text, "credentials.RefreshToken.Reveal()")
	require.NotContains(t, text, "AuthMethodIDs")
	require.NotContains(t, text, "auth_methods")
	require.NotContains(t, text, "HasAuthMethod")
	_, err = parser.ParseFile(token.NewFileSet(), "zz_generated_connector.go", strings.NewReader(text), parser.AllErrors)
	require.NoError(t, err)
}

func TestGenerateSingleSelectionAddsMethodConfigurationAsOptionalConfig(t *testing.T) {
	contents, err := os.ReadFile("../../schema/testdata/multi-auth.yaml")
	require.NoError(t, err)
	manifest, err := schema.Decode(strings.NewReader(strings.Replace(string(contents), `        guide:
          startURL: https://console.cloud.google.com/iam-admin/serviceaccounts`, `        configuration:
          fields:
            - {name: tokenEndpoint, goName: TokenEndpoint, type: url, description: Service-account token endpoint., required: true}
        guide:
          startURL: https://console.cloud.google.com/iam-admin/serviceaccounts`, 1)))
	require.NoError(t, err)

	generated, err := codegen.Generate(manifest)

	require.NoError(t, err)
	text := string(generated)
	require.Contains(t, text, "\"net/url\"")
	require.Contains(t, text, "TokenEndpoint string `json:\"tokenEndpoint,omitempty\" yaml:\"tokenEndpoint,omitempty\"`")
	require.Contains(t, text, "configuration tokenEndpoint must be an absolute URL")
	require.NotContains(t, text, "configuration tokenEndpoint is required")
	require.Contains(t, text, "switch credentials.AuthMethodID {")
	require.NotContains(t, text, "AuthMethodIDs")
}

func TestGenerateMultipleAuthSelectionMatchesGolden(t *testing.T) {
	generated := generateMultipleAuthSelectionFixture(t)
	goldenPath := filepath.Join(multipleAuthSelectionFixturePath, codegen.OutputFile)
	if *shouldUpdateGoldenFiles {
		require.NoError(t, os.WriteFile(goldenPath, generated, 0o644))
	}
	golden, err := os.ReadFile(goldenPath)
	require.NoError(t, err)
	require.Equal(t, string(golden), string(generated), "run go test ./internal/codegen -run MatchesGolden -update-golden")
}

func TestGenerateMultipleAuthSelectionCredentialsAndConfig(t *testing.T) {
	text := string(generateMultipleAuthSelectionFixture(t))

	require.Contains(t, text, "\tAuthMethodIDs   []string\n")
	require.Contains(t, text, "AuthMethodIDs   []string `json:\"auth_methods\"`")
	require.Contains(t, text, "AuthMethodIDs:   fields.AuthMethodIDs,")
	require.NotContains(t, text, "AuthMethodID ")
	require.NotContains(t, text, `"auth_method"`)
	require.Contains(t, text, "func (credentials Credentials) HasAuthMethod(id string) bool {")
	require.Contains(t, text, "credential auth_methods is required")
	require.Contains(t, text, "credential auth_methods must be unique")
	require.Contains(t, text, "credential auth_methods contains an undeclared auth method")
	require.Contains(t, text, `case "anthropic":`)
	require.Contains(t, text, "credential anthropic_api_key is required")
	require.Contains(t, text, "OpenAIProjectID      string           `json:\"openaiProjectId,omitempty\" yaml:\"openaiProjectId,omitempty\"`")
	require.Contains(t, text, "AnthropicWorkspaceID string           `json:\"anthropicWorkspaceId,omitempty\" yaml:\"anthropicWorkspaceId,omitempty\"`")
	require.Contains(t, text, `GeminiAPIVersionV1beta GeminiAPIVersion = "v1beta"`)
	require.Contains(t, text, "configuration geminiApiVersion is invalid")
	require.NotContains(t, text, "configuration openaiProjectId is required")
	require.Contains(t, text, "localconfig.NewCredentialProvider")
	require.NotContains(t, text, "encodeLocalCredentials")
}

// TestGeneratedMultipleAuthSelectionConnectorBehaves compiles the generated code
// against the local SDK source and runs the fixture's behavior tests.
func TestGeneratedMultipleAuthSelectionConnectorBehaves(t *testing.T) {
	sdkDirectory, err := filepath.Abs("../../sdkgo")
	require.NoError(t, err)
	sdkModule, err := os.ReadFile(filepath.Join(sdkDirectory, "go.mod"))
	require.NoError(t, err)
	sdkSums, err := os.ReadFile(filepath.Join(sdkDirectory, "go.sum"))
	require.NoError(t, err)
	const sdkModulePath = "github.com/superdurable/dex-connectors-library/sdkgo"
	require.True(t, bytes.HasPrefix(sdkModule, []byte("module "+sdkModulePath+"\n")))
	fixtureModule := "module example.com/multipleauthselection\n" +
		strings.TrimPrefix(string(sdkModule), "module "+sdkModulePath+"\n") +
		"\nrequire " + sdkModulePath + " v0.0.0\n\nreplace " + sdkModulePath + " => " + sdkDirectory + "\n"

	moduleDirectory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, "go.mod"), []byte(fixtureModule), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, "go.sum"), sdkSums, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, codegen.OutputFile), generateMultipleAuthSelectionFixture(t), 0o600))
	for _, name := range []string{"client.go", "connector_test.go"} {
		contents, readErr := os.ReadFile(filepath.Join(multipleAuthSelectionFixturePath, name))
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(moduleDirectory, name), contents, 0o600))
	}

	command := exec.Command("go", "test", "-count=1", "./...")
	command.Dir = moduleDirectory
	command.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	output, err := command.CombinedOutput()

	require.NoError(t, err, string(output))
}

func generateMultipleAuthSelectionFixture(t *testing.T) []byte {
	t.Helper()
	file, err := os.Open(multipleAuthSelectionManifestPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	generated, err := codegen.Generate(manifest)
	require.NoError(t, err)
	return generated
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
