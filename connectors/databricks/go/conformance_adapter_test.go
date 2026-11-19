// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
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
	createContext := singledispatchtest.NewContext("databricks-conformance", "create-job")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if scenario.Operation == "createJob" && attempt > 1 {
			createContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createJob":     newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateJob(), reference, scenario.CallID),
		"updateJobName": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateJobName(), reference, scenario.CallID),
		"deleteJob":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteJob(), reference, scenario.CallID),
		"getJobRun":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetJobRun(), reference),
		"listJobs":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListJobs(), reference),
		"getJob":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetJob(), reference),
		"listJobRuns":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListJobRuns(), reference),
		"runJob":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RunJob(), reference, scenario.CallID),
		"cancelJobRun":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelJobRun(), reference, scenario.CallID),
	}}
}
