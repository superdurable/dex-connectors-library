// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := airtable.New(airtable.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[airtable.Credentials]{
		airtableConnection: {PersonalAccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, airtable.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newStepDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listRecords":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListRecords(), airtableConnection),
		"getRecord":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetRecord(), airtableConnection),
		"upsertRecords": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRecords(), airtableConnection, scenario.CallID),
		"updateRecords": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateRecords(), airtableConnection, scenario.CallID),
	}}
}
