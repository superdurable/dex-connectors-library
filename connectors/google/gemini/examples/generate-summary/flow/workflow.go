// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package generatesummary demonstrates the Gemini generateContent Query with
// JSON Schema structured output in a Flow started from Dex Web.
package generatesummary

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "GeminiGenerateSummary"
	// ConnectionName is the static Dex Web connection that holds the Gemini API key and model.
	ConnectionName = "gemini-api"

	generateSummaryStepType = "GenerateSummary"
	maxTitleCharacters      = 200
	maxTextBytes            = 64 << 10
	// maxSummaryOutputTokens includes thought tokens, so it leaves room for a model whose default
	// thinking level is above minimal.
	maxSummaryOutputTokens = 4096
)

var (
	summaryRequestAttribute = dex.DefineAttribute[SummaryRequest]("gemini-summary-request")
	summaryOutcomeAttribute = dex.DefineAttribute[SummaryOutcome]("gemini-summary-outcome")
)

// SummaryRequest is the typed start input entered in Dex Web Start Flow.
type SummaryRequest struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Summary is the structured output that Gemini must return.
type Summary struct {
	Headline  string   `json:"headline"`
	Summary   string   `json:"summary"`
	KeyPoints []string `json:"keyPoints"`
}

// Status is the SummaryOutcome route that completed the Flow.
type Status string

const (
	// StatusGenerated means Gemini returned a summary that matches the schema.
	StatusGenerated Status = "generated"
	// StatusTruncated means Gemini stopped at the output token limit.
	StatusTruncated Status = "truncated"
	// StatusBlocked means Gemini blocked the prompt or the candidate.
	StatusBlocked Status = "blocked"
)

// SummaryOutcome is the Flow completion output.
type SummaryOutcome struct {
	Status       Status       `json:"status"`
	Title        string       `json:"title"`
	Summary      *Summary     `json:"summary,omitempty"`
	FinishReason string       `json:"finishReason,omitempty"`
	BlockReason  string       `json:"blockReason,omitempty"`
	ModelVersion string       `json:"modelVersion,omitempty"`
	Usage        gemini.Usage `json:"usage"`
}

// Flow summarizes one piece of text with Gemini.
type Flow struct {
	dex.FlowDefaults
	connection gemini.Connection
}

// NewFlow binds the trusted Gemini Connection at registration time. The
// GenerateSummary Step calls the model chosen for that connection in Dex Web.
func NewFlow(connection gemini.Connection) *Flow {
	return &Flow{connection: connection}
}

func (*Flow) GetFlowType() string { return FlowType }

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSummaryRequest{}),
		dex.DefineStep(gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[SummaryRequest]{
			StepType: generateSummaryStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gemini", GroupLabel: "Gemini",
				Explanation: "Ask Gemini for a structured JSON summary of the submitted text.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.MapToGenerateContentRequest,
			Generated:           sdkgo.GoTo(summaryGenerated{}),
			Truncated:           sdkgo.GoTo(summaryNotGenerated{}),
			Blocked:             sdkgo.GoTo(summaryNotGenerated{}),
		})),
		dex.DefineStep(summaryGenerated{}),
		dex.DefineStep(summaryNotGenerated{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{summaryRequestAttribute, summaryOutcomeAttribute}}
}

// dex:field attribute-key:gemini-summary-request value-type:json editable:false description:"Submitted summary request"
// dex:field attribute-key:gemini-summary-outcome value-type:json editable:false description:"Gemini summary outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gemini-summary-request": request,
		"gemini-summary-outcome": outcome,
	}}, nil
}

// dex:field attribute-key:gemini-summary-request value-type:json editable:false description:"Submitted title and text"
// dex:field attribute-key:gemini-summary-outcome value-type:json editable:false description:"Structured summary, finish reason, and token usage"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := summaryInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gemini-summary-request": request,
		"gemini-summary-outcome": outcome,
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

// MapToGenerateContentRequest maps the start input to the Gemini request. Model stays empty so the
// connection's model applies, and Temperature and ThinkingBudget stay nil for Gemini 3.
func (flow *Flow) MapToGenerateContentRequest(request SummaryRequest) gemini.GenerateContentRequest {
	return gemini.GenerateContentRequest{
		SystemInstruction: "You write concise newsletter summaries. Use only facts stated in the provided text. " +
			"Return a headline, a two-sentence summary, and up to five key points.",
		Contents: []gemini.Content{{Role: "user", Parts: []gemini.Part{{
			Text: "Title: " + request.Title + "\n\n" + request.Text,
		}}}},
		ResponseJSONSchema: SummarySchema(),
		MaxOutputTokens:    maxSummaryOutputTokens,
	}
}

// SummarySchema is the JSON Schema sent as generationConfig.responseJsonSchema.
func SummarySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"headline":  map[string]any{"type": "string", "description": "A headline of at most twelve words."},
			"summary":   map[string]any{"type": "string", "description": "A two-sentence summary."},
			"keyPoints": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 5},
		},
		"required":             []any{"headline", "summary", "keyPoints"},
		"additionalProperties": false,
	}
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Validate the submitted text and persist the request before calling Gemini."
type recordSummaryRequest struct {
	dex.StepDefaults
}

func (recordSummaryRequest) GetStepType() string { return "RecordSummaryRequest" }

// WaitFor skips immediately; Dex Web Start Flow invokes the start Step's WaitFor, so StepDefaultsNoWaitFor cannot be used.
func (recordSummaryRequest) WaitFor(dex.Context, SummaryRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordSummaryRequest) Execute(ctx dex.Context, request SummaryRequest) (*dex.StepDecision, error) {
	request.Title = strings.TrimSpace(request.Title)
	if strings.TrimSpace(request.Text) == "" || len(request.Text) > maxTextBytes || utf8.RuneCountInString(request.Title) > maxTitleCharacters {
		return dex.ForceFail("summary request requires text of at most 64 KiB and a title of at most 200 characters"), nil
	}
	if err := summaryRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SummaryRequest](generateSummaryStepType), request), nil
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Decode the structured Gemini summary and complete the Flow."
type summaryGenerated struct {
	dex.StepDefaultsNoWaitFor[gemini.GenerateContentResult]
}

func (summaryGenerated) GetStepType() string { return "SummaryGenerated" }

func (summaryGenerated) Execute(ctx dex.Context, result gemini.GenerateContentResult) (*dex.StepDecision, error) {
	request, err := summaryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	var summary Summary
	if err := json.Unmarshal([]byte(result.Value.Text), &summary); err != nil ||
		strings.TrimSpace(summary.Headline) == "" || strings.TrimSpace(summary.Summary) == "" {
		return dex.ForceFail("Gemini returned text that does not match the summary schema"), nil
	}
	outcome := outcomeFor(StatusGenerated, request, result)
	outcome.Summary = &summary
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:summary group-label:"Summary"
// dex:explanation text:"Complete without a summary when Gemini truncated or blocked the candidate."
type summaryNotGenerated struct {
	dex.StepDefaultsNoWaitFor[gemini.GenerateContentResult]
}

func (summaryNotGenerated) GetStepType() string { return "SummaryNotGenerated" }

func (summaryNotGenerated) Execute(ctx dex.Context, result gemini.GenerateContentResult) (*dex.StepDecision, error) {
	request, err := summaryRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	status := StatusBlocked
	if result.Branch == gemini.GenerateContentBranchTruncated {
		status = StatusTruncated
	}
	outcome := outcomeFor(status, request, result)
	if err := summaryOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func outcomeFor(status Status, request SummaryRequest, result gemini.GenerateContentResult) SummaryOutcome {
	return SummaryOutcome{
		Status: status, Title: request.Title, FinishReason: result.Value.FinishReason,
		BlockReason: result.Value.BlockReason, ModelVersion: result.Value.ModelVersion, Usage: result.Value.Usage,
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
