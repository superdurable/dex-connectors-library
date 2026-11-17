// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/rest"
const conformanceFixturePathPrefix = "/rest"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	_ = endpoint // The injected transport intercepts the fixed HTTPS subscription URL.
	reference := sdkgo.ConnectionRef{Provider: "adobe", Name: "adobe-marketo"}
	client, err := New(Config{
		RESTEndpoint:  "https://123-ABC-456.mktorest.com/rest",
		TokenEndpoint: "https://123-ABC-456.mktorest.com/identity/oauth/token",
	}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"),
			AccessToken: sdkgo.NewSecretString("sample-access-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getLead":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetLead(), reference),
		"findLeadsByEmail":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.FindLeadsByEmail(), reference),
		"updateLead":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateLead(), reference, scenario.CallID),
		"createLead":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateLead(), reference, scenario.CallID),
		"upsertLeadByEmail":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertLeadByEmail(), reference, scenario.CallID),
		"getActivityPagingToken":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetActivityPagingToken(), reference),
		"listActivityTypes":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListActivityTypes(), reference),
		"listActivities":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListActivities(), reference),
		"getCompanyByExternalID":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCompanyByExternalID(), reference),
		"createCompany":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateCompany(), reference, scenario.CallID),
		"upsertCompany":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertCompany(), reference, scenario.CallID),
		"deleteCompany":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCompany(), reference, scenario.CallID),
		"getOpportunityByExternalID": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOpportunityByExternalID(), reference),
		"createOpportunity":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateOpportunity(), reference, scenario.CallID),
		"upsertOpportunity":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertOpportunity(), reference, scenario.CallID),
		"deleteOpportunity":          newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteOpportunity(), reference, scenario.CallID),
	}}
}
