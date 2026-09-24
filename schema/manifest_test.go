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
      defectBranch: defect
      resultAttribute: optional
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
        - {id: uncertain, goName: Uncertain, description: outcome uncertain}
        - {id: defect, goName: Defect, description: local defect}
      defectBranch: defect
      uncertainBranch: uncertain
      resultAttribute: required
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
metadata: {name: slack, displayName: Slack, description: Slack connector}
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
    oauth2:
      authorizationEndpoint: https://slack.com/oauth/v2/authorize
      tokenEndpoint: https://slack.com/api/oauth.v2.access
      scopes: [chat:write]
      userScopes: [channels:history]
      credentialMappings:
        - {credential: bot_token, source: access_token}
        - {credential: user_token, source: authed_user.access_token}
      pkce: true
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
      defectBranch: defect
      resultAttribute: none
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.NoError(t, err)
	require.Equal(t, []string{"channels:history"}, manifest.Spec.Auth.OAuth2.UserScopes)
	require.Equal(t, "authed_user.access_token", manifest.Spec.Auth.OAuth2.CredentialMappings[1].Source)
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
metadata: {name: public-provider, displayName: Public, description: public}
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
      defectBranch: defect
      resultAttribute: none
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.ErrorContains(t, err, "required authorization needs connector auth")
}

func TestDecodeStudioSetupMetadata(t *testing.T) {
	manifest, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: studio-fixture, displayName: Studio Fixture, description: setup UI fixture}
spec:
  provider: fixture
  codegen: {go: {package: studiofixture}}
  configuration: {fields: []}
  auth: {type: none, connectionKind: none, fields: []}
  studio:
    setup:
      entrypoint: index.html
      hostApiRange: ">=0.1.0 <0.2.0"
      backendCapabilities: [configuration.write]
      mockScenarios: [not-configured, ready]
      icon: icon.svg
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
      defectBranch: defect
      resultAttribute: none
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.NoError(t, err)
	require.NotNil(t, manifest.Spec.Studio)
	require.Equal(t, "index.html", manifest.Spec.Studio.Setup.Entrypoint)
	require.Contains(t, manifest.Spec.Studio.Setup.BackendCapabilities, "configuration.write")
}

func TestRejectProviderIdempotencyAndInvalidProgress(t *testing.T) {
	_, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad}
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
      defectBranch: defect
      uncertainBranch: uncertain
      resultAttribute: required
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
metadata: {name: bad-provider, displayName: Bad, description: bad}
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
      defectBranch: defect
      uncertainBranch: uncertain
      resultAttribute: required
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.ErrorContains(t, err, "mutations must declare")
}

func TestRejectMissingOperationGoTypesAndSourceVersion(t *testing.T) {
	_, err := schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad, version: v0.1.0}
spec:
  provider: bad
  codegen: {go: {package: badprovider}}
  configuration: {fields: []}
  auth: {type: none, fields: []}
  operations: []
`))
	require.ErrorContains(t, err, "field version not found")

	_, err = schema.Decode(strings.NewReader(`
apiVersion: connectors.dex.dev/v1alpha1
kind: Connector
metadata: {name: bad-provider, displayName: Bad, description: bad}
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
      defectBranch: defect
      resultAttribute: none
      execution: {executeMethodTimeout: 30s, durability: sync, retry: {initialInterval: 1s, backoffCoefficient: 2, maximumInterval: 30s, maximumAttempts: 5, totalDuration: 2m}}
`))
	require.ErrorContains(t, err, "inputType and outputType")
}
