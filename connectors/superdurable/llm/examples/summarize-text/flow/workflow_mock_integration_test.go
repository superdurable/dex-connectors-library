//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/llmmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestSummarizeTextExampleRunsOnTheLLMMockWithRealDex runs the example Flow on a real Worker with the
// generated llmmock connection instead of a provider, as an application test does.
func TestSummarizeTextExampleRunsOnTheLLMMockWithRealDex(t *testing.T) {
	mock := llmmock.New(t, ConnectionName)
	flow, client := startSummaryWorkerWithConnection(t, mock.Connection(), SummaryModelConfiguration{Model: "mock-model"})
	runID := time.Now().UnixNano()

	t.Run("the manifest default completes the summary", func(t *testing.T) {
		flowID := fmt.Sprintf("llm-summary-mock-default-%d", runID)

		outcome := runSummaryFlowToCompletion(t, client, flow, flowID)

		require.Equal(t, SummaryOutcome{
			Branch: llm.GenerateTextBranchGenerated, Summary: "The connector shipped on time.", Provider: llm.ConnectorID,
			Model: "mock-model", ServedModel: "mock-model-2026-01", FinishReason: textgen.FinishReasonStop,
			Usage: textgen.Usage{InputTokens: 30, OutputTokens: 7, TotalTokens: 37},
		}, outcome)
	})

	t.Run("a scripted quota failure records the failure and fails the Flow", func(t *testing.T) {
		flowID := fmt.Sprintf("llm-summary-mock-quota-%d", runID)
		mock.GenerateText().ForFlow(flowID).Respond(llmmock.GenerateTextQuotaExhausted())

		result := runSummaryFlow(t, client, flow, flowID)

		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, "summary generation selected providerRejected: The provider account has exhausted its quota.")
	})

	t.Run("a rate limit is retried and the next case answers", func(t *testing.T) {
		flowID := fmt.Sprintf("llm-summary-mock-retry-%d", runID)
		mock.GenerateText().ForFlow(flowID).Respond(llmmock.GenerateTextRateLimited(), llmmock.GenerateTextOutputTokenLimit())

		outcome := runSummaryFlowToCompletion(t, client, flow, flowID)

		require.Equal(t, llm.GenerateTextBranchTruncated, outcome.Branch)
		require.Equal(t, "The connector shipped on", outcome.Summary)
	})

	requests := make(map[string]llm.GenerateTextRequest)
	branches := make(map[string][]string)
	for _, call := range mock.GenerateText().Calls() {
		requests[call.FlowID] = call.Input
		branches[call.FlowID] = append(branches[call.FlowID], string(call.Branch))
	}
	require.Equal(t, "mock-model", requests[fmt.Sprintf("llm-summary-mock-default-%d", runID)].Model, "the Step pick reaches the request")
	require.Equal(t, []string{"", string(llm.GenerateTextBranchTruncated)}, branches[fmt.Sprintf("llm-summary-mock-retry-%d", runID)])
}

// TestStructuredOutputFlowRunsOnTheLLMMockWithRealDex scripts the JSON answer for a Flow whose request sets a
// structured output schema, which the plain text manifest default cannot answer.
func TestStructuredOutputFlowRunsOnTheLLMMockWithRealDex(t *testing.T) {
	mock := llmmock.New(t, ConnectionName)
	flow := &releaseStatusFlow{connection: mock.Connection()}
	client := startWorker(t, flow)
	flowID := fmt.Sprintf("llm-release-status-mock-%d", time.Now().UnixNano())
	mock.GenerateText().ForFlow(flowID).Respond(llmmock.GenerateTextGenerated(llm.GenerateTextResponse{Text: `{"status":"shipped"}`}, nil))

	result := runSummaryFlow(t, client, flow, flowID)

	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var releaseStatus string
	require.NoError(t, result.DecodeSingleOutput(&releaseStatus))
	require.Equal(t, `{"status":"shipped"}`, releaseStatus)
	calls := mock.GenerateText().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, "release_status", calls[0].Input.StructuredOutput.Name, "the Step's schema reaches the request")
}

// releaseStatusFlow asks the model for a JSON release status, as an application with structured output does.
type releaseStatusFlow struct {
	dex.FlowDefaults
	connection llm.Connection
}

func (*releaseStatusFlow) GetFlowType() string { return "LLMReleaseStatusMock" }

func (*releaseStatusFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

func (flow *releaseStatusFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(llm.NewGenerateTextStep(llm.GenerateTextStepConfig[SummaryRequest]{
			StepType: "GenerateReleaseStatus", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "status", GroupLabel: "Status", Explanation: "Ask the model for the release status as JSON.",
			},
			MapToOperationInput: releaseStatusRequest,
			Generated:           sdkgo.GoTo(completeReleaseStatus{}),
		})),
		dex.DefineStep(completeReleaseStatus{}),
	}
}

func releaseStatusRequest(request SummaryRequest) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{
		Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: request.Text}},
		StructuredOutput: &textgen.StructuredOutput{Name: "release_status", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}},
			"required": []any{"status"}, "additionalProperties": false,
		}},
	}
}

type completeReleaseStatus struct {
	dex.StepDefaultsNoWaitFor[llm.GenerateTextResult]
}

func (completeReleaseStatus) Execute(_ dex.Context, result llm.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value.Text), nil
}
