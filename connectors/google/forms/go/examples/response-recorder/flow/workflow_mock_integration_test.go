//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package responserecorder

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms/formsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestResponseRecorderUsesMockPagesAndLabelsAnswersWithRealDex(t *testing.T) {
	mock := formsmock.New(t, ConnectionName)
	flow, harness := newFormsIntegrationHarnessWithConnection(t, mock.Connection(), FormConfiguration{FormID: "1FAIpQLintake"})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	listed := runRecorder(t, ctx, harness.client, flow, "forms-mock-list-"+runID, Input{})
	require.Equal(t, dex.FlowCompleted, listed.status)
	require.Equal(t, StatusRecorded, listed.outcome.Status)
	require.Equal(t, 3, listed.outcome.PagesRead)
	require.ElementsMatch(t, []string{"r_acme", "r_early", "r_shared_a", "r_shared_b"}, recordedIDs(listed.outcome))
	require.NotNil(t, listed.outcome.NextCursor)
	require.Equal(t, sharedInstant, listed.outcome.NextCursor.LastSubmittedAt)
	require.ElementsMatch(t, []string{"r_shared_a", "r_shared_b"}, listed.outcome.NextCursor.ResponseIDs)
	acme := recordedResponse(t, listed.outcome, "r_acme")
	require.Equal(t, []RecordedAnswer{
		{QuestionID: "q_name", Question: "Company name", Values: []string{"Acme GmbH"}},
		{QuestionID: "q_regions", Question: "Regions", Values: []string{"EU", "US"}},
	}, acme.Answers)
	listCalls := mock.ListResponses().Calls()
	require.Len(t, listCalls, 3)
	for index, token := range []string{"", "responses-page-2", "responses-page-3"} {
		require.Equal(t, forms.ListResponsesInput{FormID: "1FAIpQLintake", PageSize: 25, PageToken: token}, listCalls[index].Input)
	}
	require.Empty(t, mock.GetResponse().Calls(), "list mode must not fetch individual responses")

	named := runRecorder(t, ctx, harness.client, flow, "forms-mock-named-"+runID, Input{ResponseID: "r_acme"})
	require.Equal(t, dex.FlowCompleted, named.status)
	require.Equal(t, StatusRecorded, named.outcome.Status)
	require.Equal(t, []string{"r_acme"}, recordedIDs(named.outcome))
	require.Len(t, mock.GetForm().Calls(), 2)
	for _, call := range mock.GetForm().Calls() {
		require.Equal(t, forms.GetFormInput{FormID: "1FAIpQLintake"}, call.Input)
	}
	getCalls := mock.GetResponse().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, forms.GetResponseInput{FormID: "1FAIpQLintake", ResponseID: "r_acme"}, getCalls[0].Input)
}
