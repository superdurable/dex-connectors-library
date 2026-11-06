// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/graphql"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := linear.New(linear.Config{}, sdkgo.StaticCredentialProvider[linear.Credentials]{
		linearConnection: {AuthMethodID: linear.PersonalAPIKeyAuthMethodID, APIKey: sdkgo.NewSecretString("linear-conformance-key")},
	}, linear.WithAPIURL(endpoint), linear.WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchIssues":       newConformanceQueryInvocation(t, scenario.Input, newLinearDexContext("conformance-search"), client.SearchIssues(), linearConnection),
		"getIssue":           newConformanceQueryInvocation(t, scenario.Input, newLinearDexContext("conformance-get"), client.GetIssue(), linearConnection),
		"createIssue":        newConformanceMutationInvocation(t, scenario.Input, newLinearDexContext("conformance-create"), client.CreateIssue(), linearConnection, scenario.CallID),
		"updateIssue":        newConformanceMutationInvocation(t, scenario.Input, newLinearDexContext("conformance-update"), client.UpdateIssue(), linearConnection, scenario.CallID),
		"addComment":         newConformanceMutationInvocation(t, scenario.Input, newLinearDexContext("conformance-comment"), client.AddComment(), linearConnection, scenario.CallID),
		"findUserByEmail":    newConformanceQueryInvocation(t, scenario.Input, newLinearDexContext("conformance-user"), client.FindUserByEmail(), linearConnection),
		"listWorkflowStates": newConformanceQueryInvocation(t, scenario.Input, newLinearDexContext("conformance-states"), client.ListWorkflowStates(), linearConnection),
	}}
}
