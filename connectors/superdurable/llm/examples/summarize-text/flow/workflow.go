// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package summarizetext demonstrates the llm connector's generateText Query
// in a Flow started from Dex Web Start Flow, with the Step's model picked in
// Dex Web from the live model list of the connection's provider.
package summarizetext

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "LLMSummarizeText"
	// ConnectionName is the static connection, declared in dex-app.yaml, that holds the provider, its key, and the default model.
	ConnectionName = "llm"

	summarizeTextStepType = "SummarizeText"
	maxTextBytes          = 64 << 10
)

var (
	summaryRequestAttribute = dex.DefineAttribute[SummaryRequest]("llm-summary-request")
	summaryOutcomeAttribute = dex.DefineAttribute[SummaryOutcome]("llm-summary-outcome")
	summaryFailureAttribute = dex.DefineAttribute[SummaryFailure]("llm-summary-failure")
	summaryTextStream       = dex.DefineStream[string]("llm-summary-text", 1<<20)
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
	// Provider is the connection's provider that served the summary, such as anthropic.
	Provider string `json:"provider"`
	// Model is the provider's model ID that the request was sent for, such as claude-sonnet-5.
	Model string `json:"model"`
	// ServedModel is the model the provider reports it used.
	ServedModel string `json:"servedModel,omitempty"`
	// FinishReason is the provider-neutral reason generation stopped.
	FinishReason textgen.FinishReason `json:"finishReason,omitempty"`
	// Usage is the token usage the provider reported.
	Usage textgen.Usage `json:"usage"`
}

// SummaryFailure records a providerRejected, invalidResponse, or defect outcome before the Flow fails.
type SummaryFailure struct {
	// Branch is the generateText branch that the connector selected.
	Branch sdkgo.BranchID `json:"branch"`
	// Provider is the connection's provider, such as openai.
	Provider string `json:"provider"`
	// Kind is the safe failure category, such as AUTHENTICATION.
	Kind sdkgo.FailureKind `json:"kind,omitempty"`
	// Message is the safe failure message, which never holds keys or provider text.
	Message string `json:"message,omitempty"`
}

// SummaryModelConfiguration is the SummarizeText Step's model pick from Dex Web.
type SummaryModelConfiguration struct {
	// Model is a model ID of the connection's provider, such as gpt-6-luna. Empty uses the
	// connection default: the connection's model, or the provider's default model when it has none.
	Model string `json:"model"`
}

// SummaryModelConfigurationRef identifies the SummarizeText Step's use configuration.
func SummaryModelConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: llm.ConnectorID, ConnectionName: ConnectionName, OperationID: textgen.TextGenerationOperationID,
		FlowType: FlowType, StepType: summarizeTextStepType,
	}
}

// Flow summarizes one piece of text with the model its Step picks.
type Flow struct {
	dex.FlowDefaults
	connection   llm.Connection
	summaryModel SummaryModelConfiguration
}

// NewFlow binds the llm Connection and the SummarizeText Step's model pick at
// registration time. The Step calls the picked model, or the connection
// default when the pick is empty.
func NewFlow(connection llm.Connection, summaryModel SummaryModelConfiguration) *Flow {
	summaryModel.Model = strings.TrimSpace(summaryModel.Model)
	return &Flow{connection: connection, summaryModel: summaryModel}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the Flow's Steps; SummarizeText streams the summary to the llm-summary-text Stream.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSummaryRequest{}),
		dex.DefineStep(llm.NewGenerateTextStep(llm.GenerateTextStepConfig[SummaryRequest]{
			StepType: summarizeTextStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "summary", GroupLabel: "Summary",
				Explanation: "Ask the model that the Step picks, from the connection's provider, for a short summary of the submitted text.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "summaryModel", UnitID: llm.UIUnitModelPicker, Label: "Summary model",
				Description: "Choose the model that writes the summary from the live model list of the connection's provider, or type one of its model IDs. Keep the connection default to use the connection's model, or the provider's default model when the connection has none.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: llm.UIModelPickerPortModel, JSONPointer: "/model"}},
			}}},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToGenerateTextRequest,
			TextStream:          &summaryTextStream,
			Generated:           sdkgo.GoTo(recordSummaryOutcome{}),
			Truncated:           sdkgo.GoTo(recordSummaryOutcome{}),
			Blocked:             sdkgo.GoTo(recordSummaryOutcome{}),
			ProviderRejected:    sdkgo.GoTo(recordSummaryFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordSummaryFailure{}),
			Defect:              sdkgo.GoTo(recordSummaryFailure{}),
		})),
		dex.DefineStep(recordSummaryOutcome{}),
		dex.DefineStep(recordSummaryFailure{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, outcome, and failure Attributes and the summary text Stream.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{summaryRequestAttribute, summaryOutcomeAttribute, summaryFailureAttribute},
		Streams:    []dex.StreamDef{summaryTextStream},
	}
}

// GetDexSummary returns the submitted request and the outcome or failure for the Dex Web run list.
//
// dex:field attribute-key:llm-summary-request value-type:json editable:false description:"Submitted summary request"
// dex:field attribute-key:llm-summary-outcome value-type:json editable:false description:"Summary outcome, provider, and model"
// dex:field attribute-key:llm-summary-failure value-type:json editable:false description:"Summary failure"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, failure, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"llm-summary-request": request, "llm-summary-outcome": outcome, "llm-summary-failure": failure,
	}}, nil
}

// GetDexDisplay returns the submitted request and the outcome or failure for the Dex Web run detail.
//
// dex:field attribute-key:llm-summary-request value-type:json editable:false description:"Submitted text"
// dex:field attribute-key:llm-summary-outcome value-type:json editable:false description:"Summary, provider, model, finish reason, and token usage"
// dex:field attribute-key:llm-summary-failure value-type:json editable:false description:"Failure branch, provider, and safe message"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, failure, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"llm-summary-request": request, "llm-summary-outcome": outcome, "llm-summary-failure": failure,
	}}, nil
}

func summaryInspection(ctx dex.Context) (SummaryRequest, SummaryOutcome, SummaryFailure, error) {
	request, err := optionalAttribute(ctx, summaryRequestAttribute)
	if err != nil {
		return SummaryRequest{}, SummaryOutcome{}, SummaryFailure{}, err
	}
	outcome, err := optionalAttribute(ctx, summaryOutcomeAttribute)
	if err != nil {
		return SummaryRequest{}, SummaryOutcome{}, SummaryFailure{}, err
	}
	failure, err := optionalAttribute(ctx, summaryFailureAttribute)
	if err != nil {
		return SummaryRequest{}, SummaryOutcome{}, SummaryFailure{}, err
	}
	return request, outcome, failure, nil
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
// Model is the Step's pick, empty when the connection default applies. The
// request sets no temperature, effort, or output limit, so every model the
// picker lists accepts it.
func (flow *Flow) MapToGenerateTextRequest(request SummaryRequest) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{
		Model:        flow.summaryModel.Model,
		Instructions: "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Messages:     []textgen.Message{{Role: textgen.MessageRoleUser, Text: request.Text}},
	}
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Validate the submitted text and persist the request before calling the model."
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
// dex:explanation text:"Persist the generated, truncated, or blocked summary with its provider and model and complete the Flow."
type recordSummaryOutcome struct {
	dex.StepDefaultsNoWaitFor[llm.GenerateTextResult]
}

func (recordSummaryOutcome) GetStepType() string { return "RecordSummaryOutcome" }

func (recordSummaryOutcome) Execute(ctx dex.Context, result llm.GenerateTextResult) (*dex.StepDecision, error) {
	outcome := SummaryOutcome{
		Branch: result.Branch, Summary: result.Value.Text, Provider: result.Receipt.Provider, Model: result.Value.RequestedModel,
		ServedModel: result.Value.ServedModel, FinishReason: result.Value.FinishReason, Usage: result.Value.Usage,
	}
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Persist the rejected, invalid, or defect outcome with its safe message and fail the Flow."
type recordSummaryFailure struct {
	dex.StepDefaultsNoWaitFor[llm.GenerateTextResult]
}

func (recordSummaryFailure) GetStepType() string { return "RecordSummaryFailure" }

func (recordSummaryFailure) Execute(ctx dex.Context, result llm.GenerateTextResult) (*dex.StepDecision, error) {
	failure := SummaryFailure{Branch: result.Branch, Provider: result.Receipt.Provider}
	message := "summary generation selected " + string(result.Branch)
	if result.Failure != nil {
		failure.Provider, failure.Kind, failure.Message = result.Failure.Provider, result.Failure.Kind, result.Failure.Message
		message += ": " + result.Failure.Message
	}
	if err := summaryFailureAttribute.Set(ctx, failure); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
