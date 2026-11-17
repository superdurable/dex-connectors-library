//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai/go"
	"github.com/superdurable/dex-connectors-library/connectors/openai/go/openaimock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSummarizeTextExampleRunsOnOpenAIMockDefaultsWithRealDex(t *testing.T) {
	mock := openaimock.New(t, ConnectionName)
	flow, client := startSummaryWorkerWithConnection(t, mock.Connection(), SummaryModelConfiguration{})
	flowID := fmt.Sprintf("openai-summary-mock-%d", time.Now().UnixNano())
	outcome := runSummaryFlow(t, client, flow, flowID)

	require.Equal(t, SummaryOutcome{
		Branch: openai.CreateResponseBranchCompleted, Summary: "The connector shipped today.", ResponseID: "resp_mocksummary001",
		Model: "gpt-6-sol", Status: "completed", Usage: openai.Usage{InputTokens: 30, OutputTokens: 7, TotalTokens: 37},
	}, outcome, "the Flow maps the default answer unchanged")
	calls := mock.CreateResponse().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, "The OpenAI connector shipped today.", calls[0].Input.Input)
}

func TestSummarizeTextExampleDeletesStoredResponseWithRealDex(t *testing.T) {
	mock := openaimock.New(t, ConnectionName)
	flow, client := startSummaryWorkerWithConnection(t, mock.Connection(), SummaryModelConfiguration{})
	flowID := fmt.Sprintf("openai-summary-delete-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The OpenAI connector shipped today.", DeleteAfterSummary: true}, dex.StartFlowOptions{})
	require.NoError(t, err)
	outcome := waitForSummaryOutcome(t, ctx, client, flowID)
	require.True(t, outcome.Deleted)
	require.Equal(t, "resp_mocksummary001", outcome.ResponseID)
	deleteCalls := mock.DeleteResponse().Calls()
	require.Len(t, deleteCalls, 1)
	require.Equal(t, "resp_mocksummary001", deleteCalls[0].Input.ResponseID)
}

func TestSummarizeTextExampleCreatesConversationWithRealDex(t *testing.T) {
	mock := openaimock.New(t, ConnectionName)
	flow, client := startSummaryWorkerWithConnection(t, mock.Connection(), SummaryModelConfiguration{})
	flowID := fmt.Sprintf("openai-summary-conversation-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The OpenAI connector shipped today.", CreateConversation: true}, dex.StartFlowOptions{})
	require.NoError(t, err)
	outcome := waitForSummaryOutcome(t, ctx, client, flowID)
	require.Equal(t, "conv_mock001", outcome.ConversationID)
	calls := mock.CreateConversation().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "The connector shipped today.", calls[0].Input.Items[0].Content)
}

func TestSummarizeTextExampleRunsOnAScriptedOpenAIMockAnswerWithRealDex(t *testing.T) {
	mock := openaimock.New(t, ConnectionName)
	flow, client := startSummaryWorkerWithConnection(t, mock.Connection(), SummaryModelConfiguration{})
	flowID := fmt.Sprintf("openai-summary-scripted-%d", time.Now().UnixNano())
	mock.CreateResponse().ForFlow(flowID).Respond(openaimock.CreateResponseCompleted(openai.Response{
		ID: "resp_scripted", Model: "gpt-6-luna", Status: "completed", OutputText: "Shipped on time.",
	}, nil))
	outcome := runSummaryFlow(t, client, flow, flowID)

	require.Equal(t, SummaryOutcome{
		Branch: openai.CreateResponseBranchCompleted, Summary: "Shipped on time.", ResponseID: "resp_scripted",
		Model: "gpt-6-luna", Status: "completed",
	}, outcome, "the Flow maps the scripted answer")
}
