// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package structuredoutput provides a small Flow for testing structured llm mock answers.
package structuredoutput

import (
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SummaryRequest is the text supplied to the structured output Flow.
type SummaryRequest struct {
	// Text is the text sent to the model.
	Text string `json:"text"`
}

// NewFlow binds an llm connection to the structured output Flow.
func NewFlow(connection llm.Connection) dex.Flow {
	return &releaseStatusFlow{connection: connection}
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
			StepType: "GenerateReleaseStatus", ConnectionName: "llm", Connection: flow.connection,
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

// dex:group group-id:status group-label:"Status"
// dex:explanation text:"Complete the Flow with the JSON release status the model generated."
type completeReleaseStatus struct {
	dex.StepDefaultsNoWaitFor[llm.GenerateTextResult]
}

func (completeReleaseStatus) Execute(_ dex.Context, result llm.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value.Text), nil
}
