// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
	"gopkg.in/yaml.v3"
)

func TestDecodeManifest(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata:
  name: mock-provider
  displayName: Mock Provider
  description: deterministic test provider
  company: Example
  version: v0.1.0
spec:
  provider: mock
  codegen: {go: {package: mockprovider}}
  configuration:
    fields:
      - {name: endpoint, goName: Endpoint, type: url, description: Provider endpoint., required: true}
  auth: {type: none, connectionKind: none, fields: []}
  triggers:
    - name: messageCreated
      goName: MessageCreated
      eventType: MessageCreatedEvent
      configurationType: MessageCreatedTriggerConfiguration
      description: start a flow for one message
  operations:
    - name: getProfile
      goName: GetProfile
      inputType: GetProfileInput
      outputType: GetProfileOutput
      kind: query
      description: read profile
      idempotency: none
      branches:
        - {id: found, goName: Found, description: profile found}
        - {id: defect, goName: Defect, description: local defect, optional: true}
      execution: &execution
        executeMethodTimeout: 30s
        durability: sync
        retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: billedOperation, explanation: Provider bills each profile query.}}
    - name: grantCredit
      goName: GrantCredit
      inputType: GrantCreditInput
      outputType: GrantCreditOutput
      kind: mutation
      description: grant credit
      idempotency: required
      branches:
        - {id: granted, goName: Granted, description: credit granted}
        - {id: defect, goName: Defect, description: local defect, optional: true}
      progress: [text, structured]
      execution: *execution
`))
	require.NoError(t, err)
	require.Equal(t, "mock-provider", manifest.Metadata.Name)
	require.Equal(t, "none", manifest.Spec.Operations[0].Authorization)
	require.Equal(t, []string{"structured", "text"}, manifest.Spec.Operations[1].Progress)
	require.Equal(t, "messageCreated", manifest.Spec.Triggers[0].Name)
	require.Equal(t, "MessageCreatedEvent", manifest.Spec.Triggers[0].EventType)
}

func TestLanguageManifestVersionAndPackageValidation(t *testing.T) {
	fixture, err := os.ReadFile("testdata/google-oauth.yaml")
	require.NoError(t, err)
	for _, testCase := range []struct {
		name          string
		languageBlock string
		problem       string
	}{
		{name: "valid independent versions", languageBlock: "  languages:\n    python: {version: v0.2.0, package: dex-connector-google-sheets, operations: [appendRows], triggers: []}\n    typescript: {version: v1.0.0, package: '@superdurable/connector-google-sheets', operations: [appendRows], triggers: []}\n"},
		{name: "invalid Python version", languageBlock: "  languages:\n    python: {version: 0.2.0}\n", problem: "spec.languages.python.version"},
		{name: "invalid npm name", languageBlock: "  languages:\n    typescript: {version: v0.2.0, package: invalid, operations: [appendRows], triggers: []}\n", problem: "spec.languages.typescript.package"},
		{name: "missing lists", languageBlock: "  languages:\n    python: {version: v0.2.0}\n", problem: "spec.languages.python.operations is required"},
		{name: "unknown operation", languageBlock: "  languages:\n    python: {version: v0.2.0, operations: [missing], triggers: []}\n", problem: "spec.languages.python.operations names unknown missing"},
		{name: "duplicate operation", languageBlock: "  languages:\n    python: {version: v0.2.0, operations: [appendRows, appendRows], triggers: []}\n", problem: "spec.languages.python.operations has duplicate name appendRows"},
		{name: "unknown Trigger", languageBlock: "  languages:\n    python: {version: v0.2.0, operations: [], triggers: [missing]}\n", problem: "spec.languages.python.triggers names unknown missing"},
		{name: "unsupported Trigger", languageBlock: "  languages:\n    typescript: {version: v0.2.0, operations: [], triggers: [messageReceived]}\n", problem: "spec.languages.typescript.triggers must be empty until language Trigger factories are supported"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			contents := strings.Replace(string(fixture), "spec:\n", "spec:\n"+testCase.languageBlock, 1)
			manifest, err := schema.Decode(strings.NewReader(contents))
			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "v0.2.0", manifest.Spec.Languages.Python.Version)
			require.Equal(t, "v1.0.0", manifest.Spec.Languages.TypeScript.Version)
		})
	}
}

func TestLanguageCapabilityJSONSchemaRequiresListsWithoutDuplicates(t *testing.T) {
	contents, err := os.ReadFile("connector-manifest.schema.json")
	require.NoError(t, err)
	var definition any
	require.NoError(t, json.Unmarshal(contents, &definition))
	compiler := jsonschema.NewCompiler()
	languageDefinition := definition.(map[string]any)["$defs"].(map[string]any)["pythonLanguage"]
	require.NoError(t, compiler.AddResource("https://connectors.dex.dev/python-language.schema.json", languageDefinition))
	compiled, err := compiler.Compile("https://connectors.dex.dev/python-language.schema.json")
	require.NoError(t, err)
	language := schema.LanguagePackage{Version: "v0.1.0", Operations: []string{"appendRows"}, Triggers: []string{}}
	validate := func() error {
		contents, err := json.Marshal(language)
		if err != nil {
			return err
		}
		var document any
		if err := json.Unmarshal(contents, &document); err != nil {
			return err
		}
		return compiled.Validate(document)
	}
	require.NoError(t, validate())
	language.Operations = nil
	require.ErrorContains(t, validate(), "operations")
	language.Operations = []string{"appendRows", "appendRows"}
	require.ErrorContains(t, validate(), "operations")
	language.Operations = []string{"appendRows"}
	language.Triggers = []string{"messageReceived"}
	require.ErrorContains(t, validate(), "triggers")
}

func TestExecutionRetryPolicyValidation(t *testing.T) {
	file, err := os.Open("testdata/api-key-methods.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	baseline := *manifest.Spec.Operations[0].Execution.Retry
	for _, test := range []struct {
		name                 string
		policy               *schema.Retry
		executeMethodTimeout string
		problem              string
	}{
		{name: "omitted policy"},
		{name: "unlimited attempts", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", TotalDuration: "1h"}},
		{name: "longer policy", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", TotalDuration: "65m"}},
		{name: "billed attempts", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "billedOperation", Explanation: "Every provider call is billed."}}},
		{name: "person waiting", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", TotalDuration: "5m", Reason: &schema.RetryReason{Kind: "personWaiting", Explanation: "A person awaits this result."}}},
		{name: "duplicate write", policy: &baseline},
		{name: "missing reason", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h"}, problem: "getThing: retry limits attempts or runs under 1h; add reason.kind"},
		{name: "stale reason", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "personWaiting", Explanation: "A person awaits this result."}}, problem: "getThing: remove retry reason"},
		{name: "unknown reason", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "other", Explanation: "Every provider call is billed."}}, problem: "getThing: retry reason.kind must be billedOperation, personWaiting, or duplicateWriteRisk"},
		{name: "empty explanation", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "billedOperation"}}, problem: "getThing: retry reason.explanation must contain non-whitespace text"},
		{name: "whitespace-only explanation", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "billedOperation", Explanation: " \n\t"}}, problem: "getThing: retry reason.explanation must contain non-whitespace text"},
		{name: "single-word explanation", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "billedOperation", Explanation: "Billed."}}},
		{name: "missing punctuation", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "billedOperation", Explanation: "Every call is billed"}}},
		{name: "trailing whitespace", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "1h", Reason: &schema.RetryReason{Kind: "billedOperation", Explanation: "Every call is billed. \n"}}},
		{name: "explicit duration shorter than timeout", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "10s", Reason: &schema.RetryReason{Kind: "billedOperation", Explanation: "Every call is billed."}}, problem: "getThing: executeMethodTimeout must be shorter than retry totalDuration"},
		{name: "explicit duration equals timeout", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: 3, TotalDuration: "30s", Reason: &schema.RetryReason{Kind: "billedOperation", Explanation: "Every call is billed."}}, problem: "getThing: executeMethodTimeout must be shorter than retry totalDuration"},
		{name: "omitted duration shorter than timeout", executeMethodTimeout: "90m", problem: "getThing: executeMethodTimeout must be shorter than the omitted retry policy's 1h totalDuration"},
		{name: "omitted duration equals timeout", executeMethodTimeout: "1h", problem: "getThing: executeMethodTimeout must be shorter than the omitted retry policy's 1h totalDuration"},
		{name: "zero duration", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", TotalDuration: "0"}, problem: "getThing: retry totalDuration must be a positive duration"},
		{name: "negative attempts", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, MaximumInterval: "5m", MaximumAttempts: -1, TotalDuration: "1h"}, problem: "getThing: retry maximumAttempts must be non-negative"},
		{name: "small backoff", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 0.5, MaximumInterval: "5m", TotalDuration: "1h"}, problem: "getThing: retry backoffCoefficient must be at least 1"},
		{name: "missing initial interval", policy: &schema.Retry{BackoffCoefficient: 2, MaximumInterval: "5m", TotalDuration: "1h"}, problem: "getThing: retry initialInterval must be a positive duration"},
		{name: "missing maximum interval", policy: &schema.Retry{InitialInterval: "1s", BackoffCoefficient: 2, TotalDuration: "1h"}, problem: "getThing: retry maximumInterval must be a positive duration"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := manifest
			candidate.Spec.Operations = append([]schema.Operation(nil), manifest.Spec.Operations...)
			candidate.Spec.Operations[0].Execution.Retry = test.policy
			if test.executeMethodTimeout != "" {
				candidate.Spec.Operations[0].Execution.ExecuteMethodTimeout = test.executeMethodTimeout
			}
			err := candidate.Validate()
			if test.problem == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.problem)
			}
		})
	}
}

func TestDispatchFenceRequiresSyncDurability(t *testing.T) {
	file, err := os.Open("testdata/api-key-methods.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	manifest.Spec.Operations[0].Execution.DispatchFence = true
	manifest.Spec.Operations[0].Execution.Durability = "async"
	require.ErrorContains(t, manifest.Validate(), "dispatchFence requires sync execution durability")
	manifest.Spec.Operations[0].Execution.Durability = "sync"
	require.NoError(t, manifest.Validate())
}

func TestDecodeOAuthUserScopesAndCredentialMappings(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: slack, displayName: Slack, description: Slack connector, company: Slack, version: v0.1.0}
spec:
  provider: slack
  codegen: {go: {package: slack}}
  configuration: {fields: []}
  auth:
    type: oauth2
    connectionKind: slack-oauth
    fields:
      - {name: bot_token, goName: BotToken, type: secretString, description: Bot token., required: true}
      - {name: user_token, goName: UserToken, type: secretString, description: User token., required: true}
      - {name: app_token, goName: AppToken, type: secretString, description: App token., required: true}
    guide:
      startURL: https://api.slack.com/apps
      steps: [Create or select an app and copy its credentials.]
    oauth2:
      authorizationEndpoint: https://slack.com/oauth/v2/authorize
      tokenEndpoint: https://slack.com/api/oauth.v2.access
      scopes: [chat:write]
      userScopes: [channels:history]
      credentialMappings:
        - {credential: bot_token, source: access_token}
        - {credential: user_token, source: authed_user.access_token}
      credentialDerivations:
        - {credential: app_token, endpoint: https://slack.com/api/auth.test, source: url, verifiedBy: ok}
      pkce: true
  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.2.0 <0.3.0"
      backendCapabilities: [oauth.connection.manage, slack.channels-list]
      mockScenarios: [ready]
      icon: icon.svg
    commands:
      - id: listChannels
        capability: slack.channels-list
        request:
          method: GET
          url: https://slack.com/api/conversations.list
          credential: {field: bot_token, scheme: bearer}
          fixedQuery: {limit: "200"}
          parameters:
            - {name: cursor, location: query, target: cursor}
  operations:
    - name: getThing
      goName: GetThing
      inputType: GetThingInput
      outputType: GetThingOutput
      kind: query
      description: get thing
      idempotency: none
      branches:
        - {id: defect, goName: Defect, description: defect, optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`))
	require.NoError(t, err)
	require.Equal(t, []string{"channels:history"}, manifest.Spec.Auth.OAuth2.UserScopes)
	require.Equal(t, "authed_user.access_token", manifest.Spec.Auth.OAuth2.CredentialMappings[1].Source)
	require.Equal(t, schema.ValueSource{Value: "https://slack.com/api/auth.test"}, manifest.Spec.Auth.OAuth2.CredentialDerivations[0].Endpoint)
	require.Equal(t, "listChannels", manifest.Spec.Studio.Commands[0].ID)
	require.Equal(t, "bot_token", manifest.Spec.Studio.Commands[0].Request.Credential.Field)
}

func TestDecodeOAuthManifestFixture(t *testing.T) {
	file, err := os.Open("testdata/google-oauth.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	require.Equal(t, "google-sheets-oauth", manifest.Spec.Auth.ConnectionKind)
	require.Equal(t, "oauth2", manifest.Spec.Auth.OAuth2.Protocol)
	require.True(t, manifest.Spec.Auth.OAuth2.PKCE)
	require.Equal(t, []string{"https://www.googleapis.com/auth/drive.file"}, manifest.Spec.Auth.OAuth2.Scopes.List)
	require.Equal(t, "secretString", manifest.Spec.Auth.Fields[0].Type)
	require.Equal(t, "required", manifest.Spec.Operations[0].Authorization)
	require.Len(t, manifest.Spec.Auth.Fields, 1)
}

func TestDecodeOIDCManifestFixture(t *testing.T) {
	file, err := os.Open("testdata/linkedin-oidc.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	require.Equal(t, "linkedin-oidc", manifest.Spec.Auth.ConnectionKind)
	require.Equal(t, "oidc", manifest.Spec.Auth.OAuth2.Protocol)
	require.Equal(t, schema.ValueSource{Value: "https://www.linkedin.com"}, manifest.Spec.Auth.OAuth2.OIDC.Issuer)
	require.True(t, manifest.Spec.Auth.OAuth2.OIDC.NonceRequired)
	require.Equal(t, "required", manifest.Spec.Operations[0].Authorization)
}

func TestDecodeMultiAuthManifestFixture(t *testing.T) {
	file, err := os.Open("testdata/multi-auth.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	require.Equal(t, "google-oauth", manifest.Spec.Auth.DefaultMethod)
	require.Len(t, manifest.Spec.Auth.Methods, 2)
	require.True(t, manifest.Spec.Auth.Methods[0].Recommended)
	require.Equal(t, "serviceAccount", manifest.Spec.Auth.Methods[1].Type)
	require.Equal(t, "oauth2", manifest.Spec.Auth.Methods[0].OAuth2.Protocol)
	require.Equal(t, "offline", manifest.Spec.Auth.Methods[0].OAuth2.AuthorizationParameters["access_type"])
	require.Equal(t, []string{"oauth_client_id", "oauth_client_secret", "access_token", "refresh_token", "service_account_key", "delegated_user"}, []string{
		manifest.Spec.Auth.Fields[0].Name,
		manifest.Spec.Auth.Fields[1].Name,
		manifest.Spec.Auth.Fields[2].Name,
		manifest.Spec.Auth.Fields[3].Name,
		manifest.Spec.Auth.Fields[4].Name,
		manifest.Spec.Auth.Fields[5].Name,
	})
	for _, field := range manifest.Spec.Auth.Fields {
		require.False(t, field.Required, "flattened union field %s must be method-optional", field.Name)
	}
}

func TestDecodeAPIKeyMethodsFixture(t *testing.T) {
	file, err := os.Open("testdata/api-key-methods.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	manifest, err := schema.Decode(file)

	require.NoError(t, err)
	auth := manifest.Spec.Auth
	require.Equal(t, "Provider", auth.MethodLabel)
	require.Equal(t, []string{"openai", "anthropic", "gemini"}, []string{auth.Methods[0].ID, auth.Methods[1].ID, auth.Methods[2].ID})
	require.Equal(t, []schema.Field{{
		Name: "openaiProjectId", GoName: "OpenAIProjectID", Type: "string", Description: "OpenAI project ID that owns usage.", Required: true,
	}}, auth.Methods[0].Configuration.Fields)
	require.Equal(t, "anthropicWorkspaceId", auth.Methods[1].Configuration.Fields[0].Name)
	require.Equal(t, &schema.FieldStudioUnit{Unit: "modelPicker", Port: "model"}, manifest.Spec.Configuration.Fields[0].StudioUnit)
	require.Len(t, manifest.Spec.Configuration.Fields, 1, "method configuration stays on its method")
	require.Equal(t, []string{"openai_api_key", "anthropic_api_key", "gemini_api_key"}, []string{auth.Fields[0].Name, auth.Fields[1].Name, auth.Fields[2].Name})
}

func TestDecodeMethodLabelAndMethodConfiguration(t *testing.T) {
	contents := readManifestFixture(t, "testdata/multi-auth.yaml")
	contents = replaceOnce(t, contents, "    defaultMethod: google-oauth\n", "    methodLabel: Sign-in method\n    defaultMethod: google-oauth\n")
	contents = replaceOnce(t, contents, serviceAccountGuide, `        configuration:
          fields:
            - {name: tokenEndpoint, goName: TokenEndpoint, type: url, description: Service-account token endpoint., required: true}
`+serviceAccountGuide)

	manifest, err := schema.Decode(strings.NewReader(contents))

	require.NoError(t, err)
	require.Equal(t, "Sign-in method", manifest.Spec.Auth.MethodLabel)
	require.True(t, manifest.Spec.Auth.Methods[1].Configuration.Fields[0].Required)
}

func TestDecodeAuthMethodLabelLengthBoundary(t *testing.T) {
	contents := replaceOnce(t, readManifestFixture(t, "testdata/api-key-methods.yaml"), "methodLabel: Provider", "methodLabel: "+strings.Repeat("é", 32))

	manifest, err := schema.Decode(strings.NewReader(contents))

	require.NoError(t, err)
	require.Equal(t, strings.Repeat("é", 32), manifest.Spec.Auth.MethodLabel)
}

const serviceAccountGuide = `        guide:
          startURL: https://console.cloud.google.com/iam-admin/serviceaccounts
`

func TestRejectInvalidAuthMethodManifests(t *testing.T) {
	type manifestReplacement struct{ old, new string }
	testCases := []struct {
		name         string
		fixture      string
		replacements []manifestReplacement
		message      string
	}{
		{
			name: "method label without methods", fixture: "testdata/google-oauth.yaml",
			replacements: []manifestReplacement{{"    type: oauth2\n", "    methodLabel: Account\n    type: oauth2\n"}},
			message:      "spec.auth.methodLabel requires spec.auth methods",
		},
		{
			name: "removed selection key", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"    methodLabel: Provider\n", "    selection: multiple\n    methodLabel: Provider\n"}},
			message:      "field selection not found in type schema.Auth",
		},
		{
			name: "several methods still need a default method", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"    defaultMethod: openai\n", ""}},
			message:      "spec.auth.defaultMethod must identify a declared auth method",
		},
		{
			name: "method label too long", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"methodLabel: Provider", "methodLabel: " + strings.Repeat("p", 33)}},
			message:      "spec.auth.methodLabel must be 1-32 characters without surrounding whitespace",
		},
		{
			name: "method label with surrounding whitespace", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"methodLabel: Provider", `methodLabel: " Provider"`}},
			message:      "spec.auth.methodLabel must be 1-32 characters without surrounding whitespace",
		},
		{
			name: "secret method configuration", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"goName: AnthropicWorkspaceID, type: string", "goName: AnthropicWorkspaceID, type: secretString"}},
			message:      "spec.auth method anthropic configuration fields require goName, supported type, and description",
		},
		{
			name: "method configuration repeats spec configuration", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"{name: anthropicWorkspaceId, goName: AnthropicWorkspaceID,", "{name: model, goName: AnthropicWorkspaceID,"}},
			message:      "configuration field model must be unique across spec.configuration and auth method configuration",
		},
		{
			name: "method configuration repeats another method", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"{name: anthropicWorkspaceId, goName: AnthropicWorkspaceID,", "{name: openaiProjectId, goName: AnthropicWorkspaceID,"}},
			message:      "configuration field openaiProjectId must be unique across spec.configuration and auth method configuration",
		},
		{
			name: "method configuration repeats a Go name", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"goName: AnthropicWorkspaceID,", "goName: Model,"}},
			message:      "configuration field goName Model must be unique across spec.configuration and auth method configuration",
		},
		{
			name: "method configuration repeats a credential field", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"{name: anthropicWorkspaceId,", "{name: gemini_api_key,"}},
			message:      "auth method anthropic configuration field gemini_api_key cannot repeat a credential field name",
		},
		{
			name: "reserved auth_method credential name", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"{name: gemini_api_key,", "{name: auth_method,"}},
			message:      "auth_method and AuthMethodID are reserved for the selected auth method",
		},
		{
			name: "reserved AuthMethodID credential Go name", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"goName: GeminiAPIKey,", "goName: AuthMethodID,"}},
			message:      "auth_method and AuthMethodID are reserved for the selected auth method",
		},
		{
			name: "Studio unit on a credential field", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"description: Secret Gemini API key., required: true}", "description: Secret Gemini API key., required: true, studioUnit: {unit: modelPicker, port: model}}"}},
			message:      "spec.auth method gemini field gemini_api_key: studioUnit is allowed only on spec.configuration fields",
		},
		{
			name: "Studio unit on a method configuration field", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"description: OpenAI project ID that owns usage., required: true}", "description: OpenAI project ID that owns usage., required: true, studioUnit: {unit: modelPicker, port: model}}"}},
			message:      "spec.auth method openai configuration field openaiProjectId: studioUnit is allowed only on spec.configuration fields",
		},
		{
			name: "Studio unit on a legacy credential field", fixture: "testdata/google-oauth.yaml",
			replacements: []manifestReplacement{{"        description: OAuth access token.\n", "        description: OAuth access token.\n        studioUnit: {unit: modelPicker, port: model}\n"}},
			message:      "spec.auth field access_token: studioUnit is allowed only on spec.configuration fields",
		},
		{
			name: "Studio unit without spec.studio", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{apiKeyMethodsStudio, ""}},
			message:      "configuration field model studioUnit must name a declared spec.studio unit",
		},
		{
			name: "Studio unit is undeclared", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"studioUnit: {unit: modelPicker, port: model}", "studioUnit: {unit: modelChooser, port: model}"}},
			message:      "configuration field model studioUnit must name a declared spec.studio unit",
		},
		{
			name: "Studio unit port is an input", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{
				{"studioUnit: {unit: modelPicker, port: model}", "studioUnit: {unit: modelPicker, port: provider}"},
				{"        outputs:\n          - {name: model,", "        inputs:\n          - {name: provider, goName: Provider, type: string}\n        outputs:\n          - {name: model,"},
			},
			message: "configuration field model studioUnit port must name an output port of studio unit modelPicker",
		},
		{
			name: "Studio unit port type differs", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"- {name: model, goName: Model, type: string}", "- {name: model, goName: Model, type: stringList}"}},
			message:      "configuration field model studioUnit port model type stringList must equal field type string",
		},
		{
			name: "Studio unit declares an unknown key", fixture: "testdata/api-key-methods.yaml",
			replacements: []manifestReplacement{{"studioUnit: {unit: modelPicker, port: model}", "studioUnit: {unit: modelPicker, port: model, label: Model}"}},
			message:      "field label not found",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents := readManifestFixture(t, testCase.fixture)
			for _, replacement := range testCase.replacements {
				contents = replaceOnce(t, contents, replacement.old, replacement.new)
			}

			_, err := schema.Decode(strings.NewReader(contents))

			require.ErrorContains(t, err, testCase.message)
		})
	}
}

const apiKeyMethodsStudio = `  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.2.0 <0.3.0"
      backendCapabilities: [use.configuration.write, example.models-list]
      mockScenarios: [connected]
      icon: icon.svg
    units:
      - id: modelPicker
        goName: ModelPicker
        description: Choose one model of the selected provider.
        backendCapabilities: [example.models-list]
        outputs:
          - {name: model, goName: Model, type: string}
`

func readManifestFixture(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}

// replaceOnce fails when a fixture edit would silently not apply.
func replaceOnce(t *testing.T, contents string, old string, replacement string) string {
	t.Helper()
	require.Equal(t, 1, strings.Count(contents, old), "fixture must contain exactly one %q", old)
	return strings.Replace(contents, old, replacement, 1)
}

func TestRejectInvalidOIDCAndUnauthorizedOperation(t *testing.T) {
	contents, err := os.ReadFile("testdata/linkedin-oidc.yaml")
	require.NoError(t, err)

	_, err = schema.Decode(strings.NewReader(strings.Replace(string(contents), "nonceRequired: true", "nonceRequired: false", 1)))
	require.ErrorContains(t, err, "oidc auth requires HTTPS issuer, discovery, UserInfo, and nonce")

	_, err = schema.Decode(strings.NewReader(strings.Replace(string(contents), "https://api.linkedin.com/v2/userinfo", "http://api.linkedin.com/v2/userinfo", 1)))
	require.ErrorContains(t, err, "oidc auth requires HTTPS issuer, discovery, UserInfo, and nonce")

	_, err = schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: public-provider, displayName: Public, description: public, company: Example, version: v0.1.0}
spec:
  provider: public
  codegen: {go: {package: publicprovider}}
  configuration: {fields: []}
  auth: {type: none, fields: []}
  operations:
    - name: getThing
      goName: GetThing
      inputType: GetThingInput
      outputType: GetThingOutput
      kind: query
      description: get thing
      idempotency: none
      authorization: required
      branches:
        - {id: defect, goName: Defect, description: defect, optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`))
	require.ErrorContains(t, err, "required authorization needs connector auth")
}

func TestDecodeStudioSetupMetadata(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: studio-fixture, displayName: Studio Fixture, description: setup UI fixture, company: Example, version: v0.1.0}
spec:
  provider: fixture
  codegen: {go: {package: studiofixture}}
  configuration: {fields: []}
  auth: {type: none, connectionKind: none, fields: []}
  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.1.0 <0.2.0"
      backendCapabilities: [configuration.write, channels.list]
      mockScenarios: [not-configured, ready]
      icon: icon.svg
    units:
      - id: channelPicker
        goName: ChannelPicker
        description: Select a provider channel.
        backendCapabilities: [channels.list]
        outputs:
          - {name: channelId, goName: ChannelID, type: string}
  operations:
    - name: getThing
      goName: GetThing
      inputType: GetThingInput
      outputType: GetThingOutput
      kind: query
      description: get thing
      idempotency: none
      branches:
        - {id: defect, goName: Defect, description: defect, optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`))
	require.NoError(t, err)
	require.NotNil(t, manifest.Spec.Studio)
	require.Equal(t, "index.html", manifest.Spec.Studio.Setup.Entrypoint)
	require.Contains(t, manifest.Spec.Studio.Setup.BackendCapabilities, "configuration.write")
	require.Equal(t, "channelPicker", manifest.Spec.Studio.Units[0].ID)
	require.Equal(t, "channelId", manifest.Spec.Studio.Units[0].Outputs[0].Name)
}

const studioCommandManifestTemplate = `
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: model-provider, displayName: Model Provider, description: model listing fixture, company: Example, version: v0.1.0}
spec:
  provider: models
  codegen: {go: {package: modelprovider}}
  configuration: {fields: []}
  auth:
    type: apiKey
    connectionKind: model-provider-api-key
    fields:
      - {name: api_key, goName: APIKey, type: secretString, description: API key., required: true}
      - {name: region, goName: Region, type: string, description: Region., required: false}
    guide:
      startURL: https://example.com/keys
      steps: [Create and copy an API key.]
  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.2.0 <0.3.0"
      backendCapabilities: [models.list]
      mockScenarios: [connected]
      icon: icon.svg
    commands:
      - id: listModels
        capability: models.list
        request:
          method: GET
          url: https://api.example.com/v1/models
          credential: STUDIO_COMMAND_CREDENTIAL
          fixedHeaders: STUDIO_COMMAND_FIXED_HEADERS
  operations:
    - name: getThing
      goName: GetThing
      inputType: GetThingInput
      outputType: GetThingOutput
      kind: query
      description: get thing
      idempotency: none
      branches:
        - {id: defect, goName: Defect, description: defect, optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`

func decodeStudioCommandManifest(credential string, fixedHeaders string) (schema.Manifest, error) {
	contents := strings.NewReplacer(
		"STUDIO_COMMAND_CREDENTIAL", credential,
		"STUDIO_COMMAND_FIXED_HEADERS", fixedHeaders,
	).Replace(studioCommandManifestTemplate)
	return schema.Decode(strings.NewReader(contents))
}

func TestDecodeStudioCommandHeaderCredentialAndFixedHeaders(t *testing.T) {
	manifest, err := decodeStudioCommandManifest(`{field: api_key, scheme: header, header: x-api-key}`, `{anthropic-version: "2023-06-01"}`)
	require.NoError(t, err)
	request := manifest.Spec.Studio.Commands[0].Request
	require.Equal(t, schema.StudioCommandCredential{Field: "api_key", Scheme: "header", Header: "x-api-key"}, request.Credential)
	require.Equal(t, map[string]string{"anthropic-version": "2023-06-01"}, request.FixedHeaders)

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: header, header: X-Goog-Api-Key}`, `{}`)
	require.NoError(t, err)

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: header, header: x_api_key}`, `{x_api_version: "1"}`)
	require.NoError(t, err)

	manifest, err = decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{X-Api-Version: "`+strings.Repeat("v", 256)+`", x-trace-tag: "a b~!"}`)
	require.NoError(t, err)
	require.Empty(t, manifest.Spec.Studio.Commands[0].Request.Credential.Header)
	require.Len(t, manifest.Spec.Studio.Commands[0].Request.FixedHeaders, 2)
}

func TestRejectStudioCommandReservedHeaderNames(t *testing.T) {
	reservedNames := []string{
		"Host", "Content-Length", "Transfer-Encoding", "Connection", "Keep-Alive",
		"Proxy-Authorization", "Proxy-Connection", "TE", "Trailer", "Upgrade",
		"Cookie", "Set-Cookie", "Origin", "Referer", "Forwarded",
		"X-Forwarded-For", "X-Forwarded-Host", "Accept", "hOST", "x-forwarded-proto",
		"X_Forwarded_For", "x_forwarded_host", "Proxy_Authorization", "Content_Length",
		"Transfer_Encoding", "Keep_Alive", "Set_Cookie", "X-HTTP-Method-Override",
		"x-http-method", "X_Method_Override",
	}
	for _, name := range reservedNames {
		t.Run(name, func(t *testing.T) {
			_, err := decodeStudioCommandManifest(`{field: api_key, scheme: header, header: `+name+`}`, `{}`)
			require.ErrorContains(t, err, `credential header "`+name+`" is reserved`)

			_, err = decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{`+name+`: "1"}`)
			require.ErrorContains(t, err, `fixed header "`+name+`" is reserved`)
		})
	}

	_, err := decodeStudioCommandManifest(`{field: api_key, scheme: header, header: Authorization}`, `{}`)
	require.ErrorContains(t, err, `credential header "Authorization" cannot be Authorization; use the bearer credential scheme`)

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{authorization: "Basic abc"}`)
	require.ErrorContains(t, err, `fixed header "authorization" cannot be Authorization`)
}

func TestRejectInvalidStudioCommandCredentialHeader(t *testing.T) {
	_, err := decodeStudioCommandManifest(`{field: api_key, scheme: header}`, `{}`)
	require.ErrorContains(t, err, "header credential scheme requires a header name")

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: bearer, header: x-api-key}`, `{}`)
	require.ErrorContains(t, err, "credential header is allowed only with the header scheme")

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: basic}`, `{}`)
	require.ErrorContains(t, err, "credential must name a secretString auth field with bearer or header scheme")

	_, err = decodeStudioCommandManifest(`{field: region, scheme: header, header: x-api-key}`, `{}`)
	require.ErrorContains(t, err, "credential must name a secretString auth field with bearer or header scheme")

	for _, name := range []string{`"x api key"`, `"x-api-key:"`, `"x-api-key\r\nX-Injected"`, `"x-ápi-key"`} {
		_, err = decodeStudioCommandManifest(`{field: api_key, scheme: header, header: `+name+`}`, `{}`)
		require.ErrorContains(t, err, "must be an RFC 7230 token", name)
	}
}

func TestRejectInvalidStudioCommandFixedHeaders(t *testing.T) {
	_, err := decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{Anthropic-Version: "2023-06-01", anthropic-version: "2023-06-01"}`)
	require.ErrorContains(t, err, "fixed header names must be unique ignoring case and treating _ as -")

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{anthropic-version: "2023-06-01", anthropic_version: "2023-06-01"}`)
	require.ErrorContains(t, err, "fixed header names must be unique ignoring case and treating _ as -")

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: header, header: x-api-key}`, `{X-API-Key: "public"}`)
	require.ErrorContains(t, err, "fixed headers cannot repeat the credential header")

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: header, header: x-api-key}`, `{X_API_Key: "public"}`)
	require.ErrorContains(t, err, "fixed headers cannot repeat the credential header")

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{"x trace": "1"}`)
	require.ErrorContains(t, err, `fixed header "x trace" must be an RFC 7230 token`)

	_, err = decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{"x-trace\u0000": "1"}`)
	require.ErrorContains(t, err, `fixed header "x-trace\x00" must be an RFC 7230 token`)

	invalidValues := map[string]string{
		"empty":             `""`,
		"spaces only":       `"   "`,
		"leading space":     `" 2023-06-01"`,
		"trailing space":    `"2023-06-01 "`,
		"control character": `"2023\u000106"`,
		"tab":               `"2023\t06"`,
		"line break":        `"2023-06-01\r\nX-Injected: 1"`,
		"delete":            `"2023\u007F06"`,
		"non-ASCII":         `"2023-06-01é"`,
		"too long":          `"` + strings.Repeat("v", 257) + `"`,
	}
	for description, value := range invalidValues {
		t.Run(description, func(t *testing.T) {
			_, err := decodeStudioCommandManifest(`{field: api_key, scheme: bearer}`, `{anthropic-version: `+value+`}`)
			require.ErrorContains(t, err, `fixed header "anthropic-version" value must be 1-256 printable ASCII characters without surrounding spaces`)
		})
	}
}

func TestRejectProviderIdempotencyAndInvalidProgress(t *testing.T) {
	_, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad, company: Example, version: v0.1.0}
spec:
  provider: bad
  codegen: {go: {package: badprovider}}
  configuration: {fields: []}
  auth: {type: none, fields: []}
  operations:
    - name: mutateThing
      goName: MutateThing
      inputType: MutateThingInput
      outputType: MutateThingOutput
      kind: mutation
      description: unsafe
      idempotency: provider
      branches:
        - {id: uncertain, goName: Uncertain, description: uncertain}
        - {id: defect, goName: Defect, description: defect, optional: true}
      progress: [video]
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`))
	require.ErrorContains(t, err, "invalid idempotency")
	require.ErrorContains(t, err, "progress must contain only")
}

func TestRejectMutationWithoutIdempotency(t *testing.T) {
	_, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad, company: Example, version: v0.1.0}
spec:
  provider: bad
  codegen: {go: {package: badprovider}}
  configuration: {fields: []}
  auth: {type: none, fields: []}
  operations:
    - name: mutateThing
      goName: MutateThing
      inputType: MutateThingInput
      outputType: MutateThingOutput
      kind: mutation
      description: unsafe
      idempotency: none
      branches:
        - {id: uncertain, goName: Uncertain, description: uncertain}
        - {id: defect, goName: Defect, description: defect, optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`))
	require.ErrorContains(t, err, "mutations must declare")
}

func TestRejectMissingManifestMetadataAndOperationGoTypes(t *testing.T) {
	_, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad}
spec:
  provider: bad
  codegen: {go: {package: badprovider}}
  configuration: {fields: []}
  auth: {type: none, fields: []}
  operations: []
`))
	require.ErrorContains(t, err, "metadata.company is required")
	require.ErrorContains(t, err, "metadata.version must be")

	_, err = schema.Decode(strings.NewReader(strings.Replace(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad, company: Example, version: VERSION}
spec:
  provider: bad
  codegen: {go: {package: badprovider}}
  configuration: {fields: []}
  auth: {type: none, fields: []}
  operations: []
`, "VERSION", "1.0", 1)))
	require.ErrorContains(t, err, "metadata.version must be")

	_, err = schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad, company: Example, version: v0.1.0}
spec:
  provider: bad
  codegen: {go: {package: badprovider}}
  configuration: {fields: []}
  auth: {type: none, fields: []}
  operations:
    - name: getThing
      goName: GetThing
      kind: query
      description: get thing
      idempotency: none
      branches:
        - {id: defect, goName: Defect, description: defect, optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`))
	require.ErrorContains(t, err, "inputType and outputType")
}

func TestDecodeOAuthEndpointsAndScopesFromSettings(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(readManifestFixture(t, "testdata/oauth-from-settings.yaml")))
	require.NoError(t, err)

	oauth := manifest.Spec.Auth.OAuth2
	require.Equal(t, schema.ValueSource{FromSetting: "authorizationEndpoint"}, oauth.AuthorizationEndpoint)
	require.Equal(t, schema.ValueSource{FromSetting: "tokenEndpoint"}, oauth.TokenEndpoint)
	require.Equal(t, schema.ValueSource{FromSetting: "scopes"}, oauth.Scopes)
	require.Equal(t, schema.ValueSource{FromSetting: "issuer"}, oauth.OIDC.Issuer)
	require.Equal(t, schema.ValueSource{FromSetting: "discoveryEndpoint"}, oauth.OIDC.DiscoveryEndpoint)
	require.Equal(t, schema.ValueSource{FromSetting: "userInfoEndpoint"}, oauth.OIDC.UserInfoEndpoint)
	require.Equal(t, schema.ValueSource{FromSetting: "userInfoEndpoint"}, oauth.CredentialDerivations[0].Endpoint)
}

func TestFixedOAuthEndpointsAndScopesMarshalAsPlainValues(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(readManifestFixture(t, "testdata/google-oauth.yaml")))
	require.NoError(t, err)

	encoded, err := json.Marshal(manifest.Spec.Auth.OAuth2)

	require.NoError(t, err)
	require.Contains(t, string(encoded), `"authorizationEndpoint":"https://accounts.google.com/o/oauth2/v2/auth"`)
	require.Contains(t, string(encoded), `"tokenEndpoint":"https://oauth2.googleapis.com/token"`)
	require.Contains(t, string(encoded), `"scopes":["https://www.googleapis.com/auth/drive.file"]`)
}

func TestSettingReferencesRoundTripThroughJSONAndYAML(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(readManifestFixture(t, "testdata/oauth-from-settings.yaml")))
	require.NoError(t, err)

	encoded, err := json.Marshal(manifest.Spec.Auth.OAuth2)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"tokenEndpoint":{"fromSetting":"tokenEndpoint"}`)
	require.Contains(t, string(encoded), `"scopes":{"fromSetting":"scopes"}`)
	var fromJSON schema.OAuth2
	require.NoError(t, json.Unmarshal(encoded, &fromJSON))
	require.Equal(t, *manifest.Spec.Auth.OAuth2, fromJSON)

	encodedYAML, err := yaml.Marshal(manifest.Spec.Auth.OAuth2)
	require.NoError(t, err)
	var fromYAML schema.OAuth2
	require.NoError(t, yaml.Unmarshal(encodedYAML, &fromYAML))
	require.Equal(t, *manifest.Spec.Auth.OAuth2, fromYAML)
}

func TestFixedAndSettingEndpointsDecodeFromJSON(t *testing.T) {
	var oauth schema.OAuth2

	require.NoError(t, json.Unmarshal([]byte(`{"authorizationEndpoint":"https://a.example.com","tokenEndpoint":{"fromSetting":"tokenEndpoint"},"scopes":["a","b"]}`), &oauth))

	require.Equal(t, schema.ValueSource{Value: "https://a.example.com"}, oauth.AuthorizationEndpoint)
	require.Equal(t, schema.ValueSource{FromSetting: "tokenEndpoint"}, oauth.TokenEndpoint)
	require.Equal(t, schema.ValueSource{List: []string{"a", "b"}}, oauth.Scopes)
	require.Error(t, json.Unmarshal([]byte(`{"tokenEndpoint":{"fromSetting":"x","default":"y"}}`), &oauth))
	require.Error(t, json.Unmarshal([]byte(`{"tokenEndpoint":{"fromSetting":""}}`), &oauth))
	require.Error(t, json.Unmarshal([]byte(`{"tokenEndpoint":7}`), &oauth))
	require.Error(t, json.Unmarshal([]byte(`{"scopes":{"fromSetting":"a","fromKey":"b"}}`), &oauth))
}

func TestRejectInvalidSettingReferences(t *testing.T) {
	testCases := []struct {
		name         string
		replacements [][2]string
		message      string
	}{
		{
			name:         "authorization endpoint names an unknown setting",
			replacements: [][2]string{{"authorizationEndpoint: {fromSetting: authorizationEndpoint}", "authorizationEndpoint: {fromSetting: missing}"}},
			message:      "oauth2 authorizationEndpoint fromSetting missing must name a configuration field of this auth method",
		},
		{
			name:         "token endpoint names a stringList setting",
			replacements: [][2]string{{"tokenEndpoint: {fromSetting: tokenEndpoint}", "tokenEndpoint: {fromSetting: scopes}"}},
			message:      "oauth2 tokenEndpoint fromSetting scopes must name a url configuration field",
		},
		{
			name:         "token endpoint names an optional setting",
			replacements: [][2]string{{"description: Token endpoint of the identity provider., required: true", "description: Token endpoint of the identity provider., required: false"}},
			message:      "oauth2 tokenEndpoint fromSetting tokenEndpoint must name a required configuration field",
		},
		{
			name:         "scopes name a url setting",
			replacements: [][2]string{{"scopes: {fromSetting: scopes}", "scopes: {fromSetting: tokenEndpoint}"}},
			message:      "oauth2 scopes fromSetting tokenEndpoint must name a stringList configuration field",
		},
		{
			name:         "scopes name an unknown setting",
			replacements: [][2]string{{"scopes: {fromSetting: scopes}", "scopes: {fromSetting: missing}"}},
			message:      "oauth2 scopes fromSetting missing must name a configuration field of this auth method",
		},
		{
			name:         "OIDC issuer names an unknown setting",
			replacements: [][2]string{{"issuer: {fromSetting: issuer}", "issuer: {fromSetting: missing}"}},
			message:      "oidc issuer fromSetting missing must name a configuration field of this auth method",
		},
		{
			name:         "OIDC UserInfo endpoint names an unknown setting",
			replacements: [][2]string{{"        userInfoEndpoint: {fromSetting: userInfoEndpoint}", "        userInfoEndpoint: {fromSetting: missing}"}},
			message:      "oidc userInfoEndpoint fromSetting missing must name a configuration field of this auth method",
		},
		{
			name:         "credential derivation endpoint names an unknown setting",
			replacements: [][2]string{{"endpoint: {fromSetting: userInfoEndpoint}, source: sub", "endpoint: {fromSetting: missing}, source: sub"}},
			message:      "oauth2 credential derivation endpoint fromSetting missing must name a configuration field of this auth method",
		},
		{
			name:         "setting reference with another key",
			replacements: [][2]string{{"tokenEndpoint: {fromSetting: tokenEndpoint}", "tokenEndpoint: {fromSetting: tokenEndpoint, default: x}"}},
			message:      "value source: field default is not supported",
		},
		{
			name:         "setting reference with another key name",
			replacements: [][2]string{{"tokenEndpoint: {fromSetting: tokenEndpoint}", "tokenEndpoint: {fromConfig: tokenEndpoint}"}},
			message:      "value source: field fromConfig is not supported",
		},
		{
			name:         "setting reference with a list value",
			replacements: [][2]string{{"tokenEndpoint: {fromSetting: tokenEndpoint}", "tokenEndpoint: {fromSetting: [tokenEndpoint]}"}},
			message:      "cannot unmarshal !!seq into string",
		},
		{
			name:         "endpoint is a list",
			replacements: [][2]string{{"tokenEndpoint: {fromSetting: tokenEndpoint}", "tokenEndpoint: [https://a.example.com]"}},
			message:      "oauth2 auth requires connectionKind, endpoints, and scopes",
		},
		{
			name:         "scopes are a string",
			replacements: [][2]string{{"scopes: {fromSetting: scopes}", "scopes: openid"}},
			message:      "oauth2 auth requires connectionKind, endpoints, and scopes",
		},
		{
			name:         "a fixed endpoint still must be HTTPS",
			replacements: [][2]string{{"tokenEndpoint: {fromSetting: tokenEndpoint}", "tokenEndpoint: http://identity.example.com/token"}},
			message:      "oauth2 auth requires connectionKind, endpoints, and scopes",
		},
		{
			name:         "a fixed OIDC UserInfo endpoint still must be HTTPS",
			replacements: [][2]string{{"        userInfoEndpoint: {fromSetting: userInfoEndpoint}", "        userInfoEndpoint: http://identity.example.com/userinfo"}},
			message:      "oidc auth requires HTTPS issuer, discovery, UserInfo, and nonce",
		},
		{
			name:         "a fixed derivation endpoint still must be HTTPS",
			replacements: [][2]string{{"endpoint: {fromSetting: userInfoEndpoint}, source: sub", "endpoint: http://identity.example.com/userinfo, source: sub"}},
			message:      "oauth2 credential derivations require unique credential fields, HTTPS endpoints, and dotted JSON response paths",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents := readManifestFixture(t, "testdata/oauth-from-settings.yaml")
			for _, replacement := range testCase.replacements {
				contents = replaceOnce(t, contents, replacement[0], replacement[1])
			}

			_, err := schema.Decode(strings.NewReader(contents))

			require.ErrorContains(t, err, testCase.message)
		})
	}
}

func TestAuthMethodSettingReferencesReadOnlyTheirOwnConfiguration(t *testing.T) {
	manifestFor := func(tokenEndpointOwner string) string {
		return `
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: two-oauth-methods, displayName: Two OAuth Methods, description: Two OAuth methods with their own endpoints., company: Example, version: v0.1.0}
spec:
  provider: example
  codegen: {go: {package: twooauthmethods}}
  configuration: {fields: []}
  auth:
    defaultMethod: first
    methods:
      - id: first
        displayName: First
        description: First method.
        type: oauth2
        connectionKind: first-oauth
        fields:
          - {name: first_token, goName: FirstToken, type: secretString, description: First token., required: true}
        configuration:
          fields:
            - {name: firstEndpoint, goName: FirstEndpoint, type: url, description: First endpoint., required: true}
            - {name: firstScopes, goName: FirstScopes, type: stringList, description: First scopes., required: true}
        guide: {startURL: "https://identity.example.com/first", steps: [Create a client.]}
        oauth2:
          authorizationEndpoint: {fromSetting: firstEndpoint}
          tokenEndpoint: {fromSetting: ` + tokenEndpointOwner + `}
          scopes: {fromSetting: firstScopes}
          credentialMappings: [{credential: first_token, source: access_token}]
          pkce: true
      - id: second
        displayName: Second
        description: Second method.
        type: oauth2
        connectionKind: second-oauth
        fields:
          - {name: second_token, goName: SecondToken, type: secretString, description: Second token., required: true}
        configuration:
          fields:
            - {name: secondEndpoint, goName: SecondEndpoint, type: url, description: Second endpoint., required: true}
        guide: {startURL: "https://identity.example.com/second", steps: [Create a client.]}
        oauth2:
          authorizationEndpoint: https://identity.example.com/authorize
          tokenEndpoint: {fromSetting: secondEndpoint}
          scopes: [openid]
          credentialMappings: [{credential: second_token, source: access_token}]
          pkce: true
  operations:
    - name: getThing
      goName: GetThing
      inputType: GetThingInput
      outputType: GetThingOutput
      kind: query
      description: Read one thing.
      idempotency: none
      branches:
        - {id: defect, goName: Defect, description: Local input is invalid., optional: true}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m, reason: {kind: duplicateWriteRisk, explanation: Explicit test policy bounds duplicate writes.}}}
`
	}

	manifest, err := schema.Decode(strings.NewReader(manifestFor("firstEndpoint")))
	require.NoError(t, err)
	require.Equal(t, schema.ValueSource{FromSetting: "firstEndpoint"}, manifest.Spec.Auth.Methods[0].OAuth2.TokenEndpoint)
	require.Equal(t, schema.ValueSource{Value: "https://identity.example.com/authorize"}, manifest.Spec.Auth.Methods[1].OAuth2.AuthorizationEndpoint)

	_, err = schema.Decode(strings.NewReader(manifestFor("secondEndpoint")))
	require.ErrorContains(t, err, "spec.auth method first oauth2 tokenEndpoint fromSetting secondEndpoint must name a configuration field of this auth method")
}
