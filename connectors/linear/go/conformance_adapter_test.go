// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/superdurable/dex-connectors-library/connectors/linear/go"
)

const conformanceEndpointPath = "/graphql"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    client, err := linear.New(linear.Config{}, apiKeyCredentials(),
        linear.WithAPIURL(endpoint), linear.WithHTTPClient(transport))
    require.NoError(t, err)
    context := newLinearDexContext("conformance")
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "searchIssues": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchIssues(), linearConnection),
        "getIssue": newConformanceQueryInvocation(t, scenario.Input, context, client.GetIssue(), linearConnection),
        "createIssue": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateIssue(), linearConnection, scenario.CallID),
        "updateIssue": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateIssue(), linearConnection, scenario.CallID),
        "addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), linearConnection, scenario.CallID),
        "findUserByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindUserByEmail(), linearConnection),
        "listWorkflowStates": newConformanceQueryInvocation(t, scenario.Input, context, client.ListWorkflowStates(), linearConnection),
    }}
}
