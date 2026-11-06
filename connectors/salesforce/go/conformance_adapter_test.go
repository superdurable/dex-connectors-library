// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := salesforce.New(salesforce.Config{}, sdkgo.StaticCredentialProvider[salesforce.Credentials]{
		salesforceConnection: {
			AuthMethodID: salesforce.ProductionOAuthAuthMethodID,
			AccessToken: sdkgo.NewSecretString(salesforceTestToken),
			InstanceURL: endpoint,
		},
	}, salesforce.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryRecords": newConformanceQueryInvocation(t, scenario.Input, context, client.QueryRecords(), salesforceConnection),
		"getRecord": newConformanceQueryInvocation(t, scenario.Input, context, client.GetRecord(), salesforceConnection),
		"upsertRecordByExternalId": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRecordByExternalID(), salesforceConnection, scenario.CallID),
		"updateRecord": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateRecord(), salesforceConnection, scenario.CallID),
	}}
}
