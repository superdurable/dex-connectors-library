// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package summarizetext demonstrates the Meta generateText Query in a Flow
// started from Dex Web Start Flow.
package summarizetext

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/meta"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "MetaSummarizeText"
	// ConnectionName is the static Dex Web connection that holds the Meta API key and model.
	ConnectionName = "meta-api"

	summarizeTextStepType = "SummarizeText"
	maxTextBytes          = 64 << 10
	// maxSummaryOutputTokens includes reasoning tokens, which count toward Meta's output limit.
	maxSummaryOutputTokens = 4096
)

var (
	summaryRequestAttribute = dex.DefineAttribute[SummaryRequest]("meta-summary-request")
	summaryOutcomeAttribute = dex.DefineAttribute[SummaryOutcome]("meta-summary-outcome")
	summaryTextStream       = dex.DefineStream[string]("meta-summary-text", 1<<20)
)

// SummaryRequest is the typed start input entered in Dex Web Start Flow.
type SummaryRequest struct {
	// Text is the text to summarize, at most 64 KiB.
	Text string `json:"text"`
}

// SummaryOutcome is the Flow completion output.
type SummaryOutcome struct {
	// Branch is the generateText branch that completed the Flow: generated, truncated, or blocked.
	Branch sdkgo.BranchID `json:"branch"`
	// Summary is the model's text; it is partial when truncated and empty when blocked.
	Summary string `json:"summary"`
	// ServedModel is the model Meta reports it used.
	ServedModel string `json:"servedModel,omitempty"`
	// FinishReason is the provider-neutral reason generation stopped.
	FinishReason llm.FinishReason `json:"finishReason,omitempty"`
	// Usage is the token usage Meta reported.
	Usage llm.Usage `json:"usage"`
}

// SummaryModelConfiguration is the SummarizeText Step's model pick from Dex Web.
type SummaryModelConfiguration struct {
	// Model is the picked model ID. Empty uses the connection's model.
	Model string `json:"model"`
}

// SummaryModelConfigurationRef identifies the SummarizeText Step's use configuration.
func SummaryModelConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: meta.ConnectorID, ConnectionName: ConnectionName, OperationID: llm.TextGenerationOperationID,
		FlowType: FlowType, StepType: summarizeTextStepType,
	}
}

// Flow summarizes one piece of text with a Muse Spark model.
type Flow struct {
	dex.FlowDefaults
	connection   meta.Connection
	summaryModel SummaryModelConfiguration
}

// NewFlow binds the Meta Connection and the SummarizeText Step's model pick at
// registration time. The Step calls the picked model, or the connection's
// model when the pick is empty.
func NewFlow(connection meta.Connection, summaryModel SummaryModelConfiguration) *Flow {
	summaryModel.Model = strings.TrimSpace(summaryModel.Model)
	return &Flow{connection: connection, summaryModel: summaryModel}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the Flow's Steps; SummarizeText streams the summary to the meta-summary-text Stream.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSummaryRequest{}),
		dex.DefineStep(meta.NewGenerateTextStep(meta.GenerateTextStepConfig[SummaryRequest]{
			StepType: summarizeTextStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "summary", GroupLabel: "Summary",
				Explanation: "Ask a Muse Spark model for a short summary of the submitted text.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "summaryModel", UnitID: meta.UIUnitModelPicker, Label: "Summary model",
				Description: "Choose the Meta model that writes the summary, or keep the connection's model.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: meta.UIModelPickerPortModel, JSONPointer: "/model"}},
			}}},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToGenerateTextRequest,
			TextStream:          &summaryTextStream,
			Generated:           sdkgo.GoTo(recordSummaryOutcome{}),
			Truncated:           sdkgo.GoTo(recordSummaryOutcome{}),
			Blocked:             sdkgo.GoTo(recordSummaryOutcome{}),
		})),
		dex.DefineStep(recordSummaryOutcome{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes and the summary text Stream.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{summaryRequestAttribute, summaryOutcomeAttribute},
		Streams:    []dex.StreamDef{summaryTextStream},
	}
}

// GetDexSummary returns the submitted request and the outcome for the Dex Web run list.
//
// dex:field attribute-key:meta-summary-request value-type:json editable:false description:"Submitted summary request"
// dex:field attribute-key:meta-summary-outcome value-type:json editable:false description:"Meta summary outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"meta-summary-request": request, "meta-summary-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the submitted request and the outcome for the Dex Web run detail.
//
// dex:field attribute-key:meta-summary-request value-type:json editable:false description:"Submitted text"
// dex:field attribute-key:meta-summary-outcome value-type:json editable:false description:"Summary, finish reason, and token usage"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"meta-summary-request": request, "meta-summary-outcome": outcome,
	}}, nil
}

func summaryInspection(ctx dex.Context) (SummaryRequest, SummaryOutcome, error) {
	request, err := optionalAttribute(ctx, summaryRequestAttribute)
	if err != nil {
		return SummaryRequest{}, SummaryOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, summaryOutcomeAttribute)
	if err != nil {
		return SummaryRequest{}, SummaryOutcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// MapToGenerateTextRequest maps the start input to the generateText request.
// Model is the Step's pick, empty when the connection's model applies.
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) meta.GenerateTextRequest {
	return meta.GenerateTextRequest{
		Model:           flow.summaryModel.Model,
		Instructions:    "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: request.Text}},
		MaxOutputTokens: maxSummaryOutputTokens,
		ReasoningEffort: llm.ReasoningEffortLow,
	}
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Validate the submitted text and persist the request before calling Meta."
type recordSummaryRequest struct {
	dex.StepDefaults
}

func (recordSummaryRequest) GetStepType() string { return "RecordSummaryRequest" }

// WaitFor skips immediately; Dex Web Start Flow invokes the start Step's WaitFor, so StepDefaultsNoWaitFor cannot be used.
func (recordSummaryRequest) WaitFor(dex.Context, SummaryRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordSummaryRequest) Execute(ctx dex.Context, request SummaryRequest) (*dex.StepDecision, error) {
	if strings.TrimSpace(request.Text) == "" || len(request.Text) > maxTextBytes {
		return dex.ForceFail("summary request requires non-empty text of at most 64 KiB"), nil
	}
	if err := summaryRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SummaryRequest](summarizeTextStepType), request), nil
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Persist the generated, truncated, or blocked summary outcome and complete the Flow."
type recordSummaryOutcome struct {
	dex.StepDefaultsNoWaitFor[meta.GenerateTextResult]
}

func (recordSummaryOutcome) GetStepType() string { return "RecordSummaryOutcome" }

func (recordSummaryOutcome) Execute(ctx dex.Context, result meta.GenerateTextResult) (*dex.StepDecision, error) {
	outcome := SummaryOutcome{
		Branch: result.Branch, Summary: result.Value.Text, ServedModel: result.Value.ServedModel,
		FinishReason: result.Value.FinishReason, Usage: result.Value.Usage,
	}
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
