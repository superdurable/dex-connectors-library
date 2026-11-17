// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "ashby", Name: "ashby"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-ashby-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	fenceContext := singledispatchtest.NewContext("ashby-conformance", "create-candidate")
	if len(scenario.PriorHeartbeat) != 0 {
		var prior struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		for attempt := int32(0); attempt < prior.Attempt; attempt++ {
			fenceContext.AdvanceAttempt()
		}
		require.NoError(t, fenceContext.SetPersistedCheckpoint(prior.Value))
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			fenceContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listApplicationFeedback": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListApplicationFeedback(), reference),
		"requestApplicationFeedback": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.RequestApplicationFeedback(), reference, scenario.CallID),
		"submitApplicationFeedback": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SubmitApplicationFeedback(), reference, scenario.CallID),
		"listInterviews": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListInterviews(), reference),
		"getInterview": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetInterview(), reference),
		"listInterviewSchedules": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListInterviewSchedules(), reference),
		"getInterviewSchedule": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetInterviewSchedule(), reference),
		"createInterviewSchedule": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateInterviewSchedule(), reference, scenario.CallID),
		"cancelInterviewSchedule": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelInterviewSchedule(), reference, scenario.CallID),
		"cancelInterviewEvent": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelInterviewEvent(), reference, scenario.CallID),
		"listApplications": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListApplications(), reference),
		"getApplication": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetApplication(), reference),
		"createApplication": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateApplication(), reference, scenario.CallID),
		"changeApplicationStage": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.ChangeApplicationStage(), reference, scenario.CallID),
		"transferApplication": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TransferApplication(), reference, scenario.CallID),
		"setApplicationSource": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetApplicationSource(), reference, scenario.CallID),
		"deleteApplication": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteApplication(), reference, scenario.CallID),
		"listJobs": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListJobs(), reference),
		"getJob": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetJob(), reference),
		"searchJobs": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchJobs(), reference),
		"createJob": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateJob(), reference, scenario.CallID),
		"updateJobTitle": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateJobTitle(), reference, scenario.CallID),
		"setJobStatus": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SetJobStatus(), reference, scenario.CallID),
		"listCandidateNotes": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCandidateNotes(), reference),
		"createCandidateNote": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateCandidateNote(), reference, scenario.CallID),
		"listCandidateProjects": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCandidateProjects(), reference),
		"addCandidateProject": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.AddCandidateProject(), reference, scenario.CallID),
		"removeCandidateProject": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveCandidateProject(), reference, scenario.CallID),
		"searchCandidates": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchCandidates(), reference),
		"createCandidate": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateCandidate(), reference, scenario.CallID),
		"addCandidateTag": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.AddCandidateTag(), reference, scenario.CallID),
		"removeCandidateTag": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveCandidateTag(), reference, scenario.CallID),
		"getCandidate": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCandidate(), reference),
		"listCandidates": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCandidates(), reference),
		"updateCandidate": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCandidate(), reference, scenario.CallID),
	}}
}
