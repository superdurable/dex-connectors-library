// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package summarizetext demonstrates the OpenAI createResponse Mutation in a
// Flow started from Dex Web Start Flow: it stores one Response that
// summarizes the submitted text and streams the summary while it is written.
package summarizetext

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/openai/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "OpenAISummarizeText"
	// ConnectionName is the static connection, declared in dex-app.yaml, that holds the OpenAI API key and model.
	ConnectionName = "openai-api"

	summarizeTextStepType = "SummarizeText"
	maxTextBytes          = 64 << 10
)

var (
	summaryRequestAttribute = dex.DefineAttribute[SummaryRequest]("openai-summary-request")
	summaryOutcomeAttribute = dex.DefineAttribute[SummaryOutcome]("openai-summary-outcome")
	summaryTextStream       = dex.DefineStream[string]("openai-summary-text", 1<<20)
)

// SummaryRequest is the typed start input entered in Dex Web Start Flow.
type SummaryRequest struct {
	// Text is the text to summarize, at most 64 KiB.
	Text string `json:"text"`
	// DeleteAfterSummary removes the stored response after its summary is recorded.
	DeleteAfterSummary bool `json:"deleteAfterSummary,omitempty"`
	// CreateConversation stores the summary as the first message in a new conversation.
	CreateConversation bool `json:"createConversation,omitempty"`
	// ListFilesAfterSummary records the number of files on the first OpenAI file page.
	ListFilesAfterSummary bool `json:"listFilesAfterSummary,omitempty"`
}

// SummaryOutcome is the Flow completion output.
type SummaryOutcome struct {
	// Branch is the completed createResponse branch.
	Branch sdkgo.BranchID `json:"branch"`
	// Summary is the completed Response's output text.
	Summary string `json:"summary"`
	// ResponseID is the stored Response's ID, which retrieveResponse reads back.
	ResponseID string `json:"responseId"`
	// Model is the model OpenAI reports it used.
	Model string `json:"model,omitempty"`
	// Status is the completed Response status.
	Status string `json:"status"`
	// Usage is the token usage OpenAI reported.
	Usage openai.Usage `json:"usage"`
	// Deleted is true when the optional cleanup Step removed the stored response.
	Deleted bool `json:"deleted,omitempty"`
	// ConversationID is the conversation created to hold the summary, when requested.
	ConversationID string `json:"conversationId,omitempty"`
	// ListedFileCount is the number of files on the first listed page, when requested.
	ListedFileCount int `json:"listedFileCount,omitempty"`
}

// SummaryModelConfiguration is the SummarizeText Step's model pick from Dex Web.
type SummaryModelConfiguration struct {
	// Model is the picked model ID. Empty uses the connection's model.
	Model string `json:"model"`
}

// SummaryModelConfigurationRef identifies the SummarizeText Step's use configuration.
func SummaryModelConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: openai.ConnectorID, ConnectionName: ConnectionName, OperationID: openai.CreateResponseDefinition.Operation.OperationID,
		FlowType: FlowType, StepType: summarizeTextStepType,
	}
}

// Flow summarizes one piece of text in a stored OpenAI Response.
type Flow struct {
	dex.FlowDefaults
	connection   openai.Connection
	summaryModel SummaryModelConfiguration
}

// NewFlow binds the OpenAI Connection and the SummarizeText Step's model pick
// at registration time. The Step calls the picked model, or the connection's
// model when the pick is empty.
func NewFlow(connection openai.Connection, summaryModel SummaryModelConfiguration) *Flow {
	summaryModel.Model = strings.TrimSpace(summaryModel.Model)
	return &Flow{connection: connection, summaryModel: summaryModel}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the Flow's Steps; SummarizeText streams the summary to the openai-summary-text Stream.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSummaryRequest{}),
		dex.DefineStep(openai.NewCreateResponseStep(openai.CreateResponseStepConfig[SummaryRequest]{
			StepType: summarizeTextStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "summary", GroupLabel: "Summary",
				Explanation: "Store an OpenAI Response that summarizes the submitted text, streaming the summary while it is written.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "summaryModel", UnitID: openai.UIUnitModelPicker, Label: "Summary model",
				Description: "Choose the OpenAI model that writes the summary from OpenAI's live model list, or keep the connection's model, which an empty pick uses.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: openai.UIModelPickerPortModel, JSONPointer: "/model"}},
			}}},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToCreateRequest,
			TextStream:          &summaryTextStream,
			Completed:           sdkgo.GoTo(recordSummaryOutcome{}),
		})),
		dex.DefineStep(recordSummaryOutcome{}),
		dex.DefineStep(openai.NewListFilesStep(openai.ListFilesStepConfig[SummaryOutcome]{
			StepType: "ListStoredFiles", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "summary", GroupLabel: "Summary", Explanation: "List one page of OpenAI file metadata after the summary is recorded."},
			Connection:          flow.connection,
			MapToOperationInput: func(SummaryOutcome) openai.ListFilesRequest { return openai.ListFilesRequest{} },
			Found:               sdkgo.GoTo(recordFilePageOutcome{}),
		})),
		dex.DefineStep(recordFilePageOutcome{}),
		dex.DefineStep(openai.NewCreateConversationStep(openai.CreateConversationStepConfig[SummaryOutcome]{
			StepType: "CreateSummaryConversation", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "summary", GroupLabel: "Summary", Explanation: "Store the summary as the first message in an OpenAI conversation."},
			Connection:  flow.connection,
			MapToOperationInput: func(outcome SummaryOutcome) openai.CreateConversationRequest {
				return openai.CreateConversationRequest{Items: []openai.ConversationMessage{{Type: "message", Role: "assistant", Content: outcome.Summary}}}
			},
			Created: sdkgo.GoTo(recordConversationOutcome{}),
		})),
		dex.DefineStep(recordConversationOutcome{}),
		dex.DefineStep(openai.NewDeleteResponseStep(openai.DeleteResponseStepConfig[SummaryOutcome]{
			StepType: "DeleteStoredResponse", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "summary", GroupLabel: "Summary", Explanation: "Delete the stored OpenAI Response after its summary has been recorded."},
			Connection:  flow.connection,
			MapToOperationInput: func(outcome SummaryOutcome) openai.DeleteResponseRequest {
				return openai.DeleteResponseRequest{ResponseID: outcome.ResponseID}
			},
			Deleted: sdkgo.GoTo(recordDeletionOutcome{}),
		})),
		dex.DefineStep(recordDeletionOutcome{}),
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
// dex:field attribute-key:openai-summary-request value-type:json editable:false description:"Submitted summary request"
// dex:field attribute-key:openai-summary-outcome value-type:json editable:false description:"OpenAI summary outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"openai-summary-request": request, "openai-summary-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the submitted request and the outcome for the Dex Web run detail.
//
// dex:field attribute-key:openai-summary-request value-type:json editable:false description:"Submitted text"
// dex:field attribute-key:openai-summary-outcome value-type:json editable:false description:"Summary, Response ID, status, and token usage"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"openai-summary-request": request, "openai-summary-outcome": outcome,
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

// MapToCreateRequest maps the start input to the createResponse request.
// Model is the Step's pick, empty when the connection's model applies.
func (flow *Flow) MapToCreateRequest(request SummaryRequest) openai.CreateRequest {
	return openai.CreateRequest{
		Model:        flow.summaryModel.Model,
		Instructions: "Summarize the user's text in at most three sentences. Use only facts stated in the text.",
		Input:        request.Text,
	}
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Validate the submitted text and persist the request before calling OpenAI."
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
// dex:explanation text:"Persist the completed Response with its ID and usage and complete the Flow."
type recordSummaryOutcome struct {
	dex.StepDefaultsNoWaitFor[openai.CreateResponseResult]
}

func (recordSummaryOutcome) GetStepType() string { return "RecordSummaryOutcome" }

func (recordSummaryOutcome) Execute(ctx dex.Context, result openai.CreateResponseResult) (*dex.StepDecision, error) {
	outcome := SummaryOutcome{
		Branch: result.Branch, Summary: result.Value.OutputText, ResponseID: result.Value.ID,
		Model: result.Value.Model, Status: result.Value.Status, Usage: result.Value.Usage,
	}
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	request, err := summaryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if request.ListFilesAfterSummary {
		return dex.GoTo(sdkgo.StepRef[SummaryOutcome]("ListStoredFiles"), outcome), nil
	}
	if request.DeleteAfterSummary {
		if request.CreateConversation {
			return dex.GoTo(sdkgo.StepRef[SummaryOutcome]("CreateSummaryConversation"), outcome), nil
		}
		return dex.GoTo(sdkgo.StepRef[SummaryOutcome]("DeleteStoredResponse"), outcome), nil
	}
	if request.CreateConversation {
		return dex.GoTo(sdkgo.StepRef[SummaryOutcome]("CreateSummaryConversation"), outcome), nil
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Record the number of stored files found on the first OpenAI page."
type recordFilePageOutcome struct {
	dex.StepDefaultsNoWaitFor[openai.ListFilesResult]
}

func (recordFilePageOutcome) GetStepType() string { return "RecordFilePageOutcome" }

func (recordFilePageOutcome) Execute(ctx dex.Context, result openai.ListFilesResult) (*dex.StepDecision, error) {
	outcome, err := summaryOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.ListedFileCount = len(result.Value.Files)
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	request, err := summaryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if request.CreateConversation {
		return dex.GoTo(sdkgo.StepRef[SummaryOutcome]("CreateSummaryConversation"), outcome), nil
	}
	if request.DeleteAfterSummary {
		return dex.GoTo(sdkgo.StepRef[SummaryOutcome]("DeleteStoredResponse"), outcome), nil
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Record the conversation ID that holds the completed summary."
type recordConversationOutcome struct {
	dex.StepDefaultsNoWaitFor[openai.CreateConversationResult]
}

func (recordConversationOutcome) GetStepType() string { return "RecordConversationOutcome" }

func (recordConversationOutcome) Execute(ctx dex.Context, result openai.CreateConversationResult) (*dex.StepDecision, error) {
	outcome, err := summaryOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.ConversationID = result.Value.ID
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	request, err := summaryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if request.DeleteAfterSummary {
		return dex.GoTo(sdkgo.StepRef[SummaryOutcome]("DeleteStoredResponse"), outcome), nil
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Record that OpenAI deleted the stored Response after the summary was saved."
type recordDeletionOutcome struct {
	dex.StepDefaultsNoWaitFor[openai.DeleteResponseResult]
}

func (recordDeletionOutcome) GetStepType() string { return "RecordDeletionOutcome" }

func (recordDeletionOutcome) Execute(ctx dex.Context, result openai.DeleteResponseResult) (*dex.StepDecision, error) {
	outcome, err := summaryOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Deleted = result.Value.Deleted
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
