// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	crm "github.com/superdurable/dex-connectors-library/connectors/zoho/crm/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "/crm/v8", "/crm/v8"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := crm.New(crm.Config{}, sdkgo.StaticCredentialProvider[crm.Credentials]{
		crmConnection: testCredentials(crm.USDataCenterAuthMethodID),
	}, crm.WithAPIBaseURL(endpoint), crm.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newCRMDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"findRecords":         newConformanceQueryInvocation(t, scenario.Input, context, client.FindRecords(), crmConnection),
		"getRecord":           newConformanceQueryInvocation(t, scenario.Input, context, client.GetRecord(), crmConnection),
		"listModifiedRecords": newConformanceQueryInvocation(t, scenario.Input, context, client.ListModifiedRecords(), crmConnection),
		"upsertRecord":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertRecord(), crmConnection, scenario.CallID),
		"updateRecord":        newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateRecord(), crmConnection, scenario.CallID),
		"listModuleFields":    newConformanceQueryInvocation(t, scenario.Input, context, client.ListModuleFields(), crmConnection),
	}}
}
