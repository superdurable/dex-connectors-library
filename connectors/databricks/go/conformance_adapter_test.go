// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "databricks", Name: "databricks"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-databricks-api-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getJobRun": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetJobRun(), reference),
		"listJobs": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListJobs(), reference),
		"getJob": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetJob(), reference),
		"listJobRuns": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListJobRuns(), reference),
		"runJob": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RunJob(), reference, scenario.CallID),
		"cancelJobRun": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelJobRun(), reference, scenario.CallID),
	}}
}
