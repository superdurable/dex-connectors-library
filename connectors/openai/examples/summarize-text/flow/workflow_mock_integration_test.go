//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/openaimock"
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
