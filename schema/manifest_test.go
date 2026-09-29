// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schema_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/schema"
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
        - {id: defect, goName: Defect, description: local defect}
      execution: &execution
        executeMethodTimeout: 30s
        durability: sync
        retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}
    - name: grantCredit
      goName: GrantCredit
      inputType: GrantCreditInput
      outputType: GrantCreditOutput
      kind: mutation
      description: grant credit
      idempotency: required
      branches:
        - {id: granted, goName: Granted, description: credit granted}
        - {id: defect, goName: Defect, description: local defect}
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
        - {id: defect, goName: Defect, description: defect}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.NoError(t, err)
	require.Equal(t, []string{"channels:history"}, manifest.Spec.Auth.OAuth2.UserScopes)
	require.Equal(t, "authed_user.access_token", manifest.Spec.Auth.OAuth2.CredentialMappings[1].Source)
	require.Equal(t, "https://slack.com/api/auth.test", manifest.Spec.Auth.OAuth2.CredentialDerivations[0].Endpoint)
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
	require.Equal(t, []string{"https://www.googleapis.com/auth/drive.file"}, manifest.Spec.Auth.OAuth2.Scopes)
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
	require.Equal(t, "https://www.linkedin.com", manifest.Spec.Auth.OAuth2.OIDC.Issuer)
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

func TestDecodeMultipleAuthSelectionFixture(t *testing.T) {
	file, err := os.Open("testdata/multiple-auth-selection.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	manifest, err := schema.Decode(file)

	require.NoError(t, err)
	auth := manifest.Spec.Auth
	require.Equal(t, schema.AuthSelectionMultiple, auth.Selection)
	require.True(t, auth.IsMultipleSelection())
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

func TestDecodeSingleSelectionAllowsMethodLabelAndMethodConfiguration(t *testing.T) {
	contents := readManifestFixture(t, "testdata/multi-auth.yaml")
	contents = replaceOnce(t, contents, "    defaultMethod: google-oauth\n", "    selection: single\n    methodLabel: Sign-in method\n    defaultMethod: google-oauth\n")
	contents = replaceOnce(t, contents, serviceAccountGuide, `        configuration:
          fields:
            - {name: tokenEndpoint, goName: TokenEndpoint, type: url, description: Service-account token endpoint., required: true}
`+serviceAccountGuide)

	manifest, err := schema.Decode(strings.NewReader(contents))

	require.NoError(t, err)
	require.False(t, manifest.Spec.Auth.IsMultipleSelection())
	require.Equal(t, "Sign-in method", manifest.Spec.Auth.MethodLabel)
	require.True(t, manifest.Spec.Auth.Methods[1].Configuration.Fields[0].Required)
}

func TestDecodeAuthMethodLabelLengthBoundary(t *testing.T) {
	contents := replaceOnce(t, readManifestFixture(t, "testdata/multiple-auth-selection.yaml"), "methodLabel: Provider", "methodLabel: "+strings.Repeat("é", 32))

	manifest, err := schema.Decode(strings.NewReader(contents))

	require.NoError(t, err)
	require.Equal(t, strings.Repeat("é", 32), manifest.Spec.Auth.MethodLabel)
}

const serviceAccountGuide = `        guide:
          startURL: https://console.cloud.google.com/iam-admin/serviceaccounts
`

func TestRejectInvalidAuthMethodSelectionManifests(t *testing.T) {
	type manifestReplacement struct{ old, new string }
	testCases := []struct {
		name         string
		fixture      string
		replacements []manifestReplacement
		message      string
	}{
		{
			name: "selection without methods", fixture: "testdata/google-oauth.yaml",
			replacements: []manifestReplacement{{"    type: oauth2\n", "    selection: single\n    type: oauth2\n"}},
			message:      "spec.auth.selection requires spec.auth methods",
		},
		{
			name: "method label without methods", fixture: "testdata/google-oauth.yaml",
			replacements: []manifestReplacement{{"    type: oauth2\n", "    methodLabel: Account\n    type: oauth2\n"}},
			message:      "spec.auth.methodLabel requires spec.auth methods",
		},
		{
			name: "unknown selection", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"selection: multiple", "selection: several"}},
			message:      "spec.auth.selection must be single or multiple",
		},
		{
			name: "multiple selection with one method", fixture: "testdata/multi-auth.yaml",
			replacements: []manifestReplacement{
				{"    defaultMethod: google-oauth\n", "    selection: multiple\n    defaultMethod: workspace-service-account\n"},
				{multiAuthGoogleOAuthMethod, ""},
			},
			message: "spec.auth selection multiple requires at least two auth methods",
		},
		{
			name: "multiple selection with OAuth", fixture: "testdata/multi-auth.yaml",
			replacements: []manifestReplacement{{"    defaultMethod: google-oauth\n", "    selection: multiple\n    defaultMethod: google-oauth\n"}},
			message:      "spec.auth selection multiple requires every auth method to use apiKey; google-oauth uses oauth2",
		},
		{
			name: "multiple selection with a service account", fixture: "testdata/multi-auth.yaml",
			replacements: []manifestReplacement{{"    defaultMethod: google-oauth\n", "    selection: multiple\n    defaultMethod: google-oauth\n"}},
			message:      "spec.auth selection multiple requires every auth method to use apiKey; workspace-service-account uses serviceAccount",
		},
		{
			name: "multiple selection still needs a default method", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"    defaultMethod: openai\n", ""}},
			message:      "spec.auth.defaultMethod must identify a declared auth method",
		},
		{
			name: "method label too long", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"methodLabel: Provider", "methodLabel: " + strings.Repeat("p", 33)}},
			message:      "spec.auth.methodLabel must be 1-32 characters without surrounding whitespace",
		},
		{
			name: "method label with surrounding whitespace", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"methodLabel: Provider", `methodLabel: " Provider"`}},
			message:      "spec.auth.methodLabel must be 1-32 characters without surrounding whitespace",
		},
		{
			name: "secret method configuration", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"goName: AnthropicWorkspaceID, type: string", "goName: AnthropicWorkspaceID, type: secretString"}},
			message:      "spec.auth method anthropic configuration fields require goName, supported type, and description",
		},
		{
			name: "method configuration repeats spec configuration", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"{name: anthropicWorkspaceId, goName: AnthropicWorkspaceID,", "{name: model, goName: AnthropicWorkspaceID,"}},
			message:      "configuration field model must be unique across spec.configuration and auth method configuration",
		},
		{
			name: "method configuration repeats another method", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"{name: anthropicWorkspaceId, goName: AnthropicWorkspaceID,", "{name: openaiProjectId, goName: AnthropicWorkspaceID,"}},
			message:      "configuration field openaiProjectId must be unique across spec.configuration and auth method configuration",
		},
		{
			name: "method configuration repeats a Go name", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"goName: AnthropicWorkspaceID,", "goName: Model,"}},
			message:      "configuration field goName Model must be unique across spec.configuration and auth method configuration",
		},
		{
			name: "method configuration repeats a credential field", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"{name: anthropicWorkspaceId,", "{name: gemini_api_key,"}},
			message:      "auth method anthropic configuration field gemini_api_key cannot repeat a credential field name",
		},
		{
			name: "reserved auth_methods credential name", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"{name: gemini_api_key,", "{name: auth_methods,"}},
			message:      "auth_method, auth_methods, AuthMethodID, and AuthMethodIDs are reserved for auth method selection",
		},
		{
			name: "reserved AuthMethodIDs credential Go name", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"goName: GeminiAPIKey,", "goName: AuthMethodIDs,"}},
			message:      "auth_method, auth_methods, AuthMethodID, and AuthMethodIDs are reserved for auth method selection",
		},
		{
			name: "reserved auth_method credential name", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"{name: gemini_api_key,", "{name: auth_method,"}},
			message:      "auth_method, auth_methods, AuthMethodID, and AuthMethodIDs are reserved for auth method selection",
		},
		{
			name: "Studio unit on a credential field", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"description: Secret Gemini API key., required: true}", "description: Secret Gemini API key., required: true, studioUnit: {unit: modelPicker, port: model}}"}},
			message:      "spec.auth method gemini field gemini_api_key: studioUnit is allowed only on spec.configuration fields",
		},
		{
			name: "Studio unit on a method configuration field", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"description: OpenAI project ID that owns usage., required: true}", "description: OpenAI project ID that owns usage., required: true, studioUnit: {unit: modelPicker, port: model}}"}},
			message:      "spec.auth method openai configuration field openaiProjectId: studioUnit is allowed only on spec.configuration fields",
		},
		{
			name: "Studio unit on a legacy credential field", fixture: "testdata/google-oauth.yaml",
			replacements: []manifestReplacement{{"        description: OAuth access token.\n", "        description: OAuth access token.\n        studioUnit: {unit: modelPicker, port: model}\n"}},
			message:      "spec.auth field access_token: studioUnit is allowed only on spec.configuration fields",
		},
		{
			name: "Studio unit without spec.studio", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{multipleAuthSelectionStudio, ""}},
			message:      "configuration field model studioUnit must name a declared spec.studio unit",
		},
		{
			name: "Studio unit is undeclared", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"studioUnit: {unit: modelPicker, port: model}", "studioUnit: {unit: modelChooser, port: model}"}},
			message:      "configuration field model studioUnit must name a declared spec.studio unit",
		},
		{
			name: "Studio unit port is an input", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{
				{"studioUnit: {unit: modelPicker, port: model}", "studioUnit: {unit: modelPicker, port: provider}"},
				{"        outputs:\n          - {name: model,", "        inputs:\n          - {name: provider, goName: Provider, type: string}\n        outputs:\n          - {name: model,"},
			},
			message: "configuration field model studioUnit port must name an output port of studio unit modelPicker",
		},
		{
			name: "Studio unit port type differs", fixture: "testdata/multiple-auth-selection.yaml",
			replacements: []manifestReplacement{{"- {name: model, goName: Model, type: string}", "- {name: model, goName: Model, type: stringList}"}},
			message:      "configuration field model studioUnit port model type stringList must equal field type string",
		},
		{
			name: "Studio unit declares an unknown key", fixture: "testdata/multiple-auth-selection.yaml",
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

const multiAuthGoogleOAuthMethod = `      - id: google-oauth
        displayName: Google OAuth
        description: Authorize an individual Google account.
        recommended: true
        type: oauth2
        connectionKind: example-google-oauth
        fields:
          - {name: oauth_client_id, goName: OAuthClientID, type: string, description: OAuth application client ID., required: true}
          - {name: oauth_client_secret, goName: OAuthClientSecret, type: secretString, description: OAuth application client secret., required: true}
          - {name: access_token, goName: AccessToken, type: secretString, description: Short-lived access token., required: true}
          - {name: refresh_token, goName: RefreshToken, type: secretString, description: Long-lived refresh token., required: true}
        guide:
          startURL: https://console.cloud.google.com/apis/credentials
          steps: [Create a Web application and copy its client credentials.]
        oauth2:
          authorizationEndpoint: https://accounts.google.com/o/oauth2/v2/auth
          tokenEndpoint: https://oauth2.googleapis.com/token
          authorizationParameters: {access_type: offline, prompt: consent}
          clientIDCredential: oauth_client_id
          clientSecretCredential: oauth_client_secret
          scopes: [openid]
          credentialMappings:
            - {credential: access_token, source: access_token}
            - {credential: refresh_token, source: refresh_token}
          pkce: true
`

const multipleAuthSelectionStudio = `  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.2.0 <0.3.0"
      backendCapabilities: [use.configuration.write, example.models-list]
      mockScenarios: [connected]
      icon: icon.svg
    units:
      - id: modelPicker
        goName: ModelPicker
        description: Choose one model from the providers this connection added.
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
        - {id: defect, goName: Defect, description: defect}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
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
        - {id: defect, goName: Defect, description: defect}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
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
        - {id: defect, goName: Defect, description: defect}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
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
        - {id: defect, goName: Defect, description: defect}
      progress: [video]
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
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
        - {id: defect, goName: Defect, description: defect}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
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
        - {id: defect, goName: Defect, description: defect}
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.ErrorContains(t, err, "inputType and outputType")
}
