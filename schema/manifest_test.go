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
	require.Equal(t, []string{"structured", "text"}, manifest.Spec.Operations[1].Progress)
}

func TestDecodeOAuthManifestFixture(t *testing.T) {
	file, err := os.Open("testdata/google-oauth.yaml")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	manifest, err := schema.Decode(file)
	require.NoError(t, err)
	require.Equal(t, "google-sheets-oauth", manifest.Spec.Auth.ConnectionKind)
	require.True(t, manifest.Spec.Auth.OAuth2.PKCE)
	require.Equal(t, []string{"https://www.googleapis.com/auth/spreadsheets"}, manifest.Spec.Auth.OAuth2.Scopes)
	require.Equal(t, "secretString", manifest.Spec.Auth.Fields[0].Type)
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
