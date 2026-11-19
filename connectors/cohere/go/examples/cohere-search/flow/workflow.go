// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package coheresearch embeds, reranks, and summarizes documents in a Dex Flow.
package coheresearch

import (
	"github.com/superdurable/dex-connectors-library/connectors/cohere/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"strings"
)

// FlowType is the stable Dex Flow identity.
const FlowType = "CohereSearch"

// ConnectionName is the static Dex Web connection name.
const ConnectionName = "cohere"
const embedStepType = "EmbedSearchQuery"
const rerankStepType = "RerankSearchDocuments"
const chatStepType = "SummarizeSearchDocuments"

var requestAttribute = dex.DefineAttribute[Request]("cohere-search-request")
var rankingAttribute = dex.DefineAttribute[cohere.RerankOutput]("cohere-search-ranking")

// Request is the typed Start Flow input.
type Request struct {
	// Query is the search question to embed and rank against.
	Query string `json:"query"`
	// Documents are the text candidates to rank.
	Documents []string `json:"documents"`
}

// Outcome is the Flow completion output.
type Outcome struct {
	// Results are indices of input documents in relevance order.
	Results []cohere.RankedDocument `json:"results"`
	// Summary is the generated explanation of the leading result.
	Summary string `json:"summary"`
}

// Flow owns query embedding and document reranking.
type Flow struct {
	dex.FlowDefaults
	connection cohere.Connection
}

// NewFlow binds the named Cohere connection at registration.
func NewFlow(connection cohere.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the request Attribute used across the two calls.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, rankingAttribute}}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the example's read-only Dex Web summary.
func (*Flow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns the example's read-only Dex Web display.
func (*Flow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(cohere.NewEmbedStep(cohere.EmbedStepConfig[Request]{StepType: embedStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "cohere-search", GroupLabel: "Cohere search", Explanation: "Embed the search query."}, MapToOperationInput: MapToEmbedInput, Embedded: sdkgo.GoTo(afterEmbedding{})})),
		dex.DefineStep(afterEmbedding{}),
		dex.DefineStep(cohere.NewRerankStep(cohere.RerankStepConfig[Request]{StepType: rerankStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "cohere-search", GroupLabel: "Cohere search", Explanation: "Rank the candidate documents."}, MapToOperationInput: MapToRerankInput, Ranked: sdkgo.GoTo(afterRerank{})})),
		dex.DefineStep(afterRerank{}),
		dex.DefineStep(cohere.NewChatStep(cohere.ChatStepConfig[cohere.ChatInput]{StepType: chatStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "cohere-search", GroupLabel: "Cohere search", Explanation: "Summarize the leading search result."}, MapToOperationInput: MapToChatInput, Answered: sdkgo.GoTo(completeSearch{})})),
		dex.DefineStep(completeSearch{}),
	}
}

// MapToEmbedInput embeds the query for search use.
func MapToEmbedInput(request Request) cohere.EmbedInput {
	return cohere.EmbedInput{Model: "embed-v4.0", Texts: []string{request.Query}, InputType: "search_query"}
}

// MapToRerankInput ranks the original documents for the query.
func MapToRerankInput(request Request) cohere.RerankInput {
	return cohere.RerankInput{Model: "rerank-v5.0-pro", Query: request.Query, Documents: request.Documents}
}

// MapToChatInput passes a validated chat request to the connector.
func MapToChatInput(request cohere.ChatInput) cohere.ChatInput { return request }

// dex:group group-id:cohere-search group-label:"Cohere search"
// dex:explanation text:"Validate and persist the query and documents."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCohereSearch" }
func (startRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(ctx dex.Context, request Request) (*dex.StepDecision, error) {
	if strings.TrimSpace(request.Query) == "" || len(request.Documents) < 2 {
		return dex.ForceFail("query and at least two documents are required"), nil
	}
	if err := requestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Request](embedStepType), request), nil
}

// dex:group group-id:cohere-search group-label:"Cohere search"
// dex:explanation text:"Load the original documents after embedding."
type afterEmbedding struct {
	dex.StepDefaultsNoWaitFor[cohere.EmbedResult]
}

func (afterEmbedding) GetStepType() string { return "AfterCohereEmbedding" }
func (afterEmbedding) Execute(ctx dex.Context, _ cohere.EmbedResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Request](rerankStepType), request), nil
}

// dex:group group-id:cohere-search group-label:"Cohere search"
// dex:explanation text:"Persist ranking before requesting a summary."
type afterRerank struct {
	dex.StepDefaultsNoWaitFor[cohere.RerankResult]
}

func (afterRerank) GetStepType() string { return "AfterCohereRerank" }
func (afterRerank) Execute(ctx dex.Context, result cohere.RerankResult) (*dex.StepDecision, error) {
	if err := rankingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Results) == 0 {
		return dex.ForceFail("Cohere returned no ranked document"), nil
	}
	leadingIndex := result.Value.Results[0].Index
	chatInput := cohere.ChatInput{Model: "command-a-03-2025", Messages: []cohere.ChatMessage{{Role: "user", Content: "Summarize the best match for " + request.Query + ": " + request.Documents[leadingIndex]}}, MaxTokens: 128}
	return dex.GoTo(sdkgo.StepRef[cohere.ChatInput](chatStepType), chatInput), nil
}

// dex:group group-id:cohere-search group-label:"Cohere search"
// dex:explanation text:"Complete with ranking and generated summary."
type completeSearch struct {
	dex.StepDefaultsNoWaitFor[cohere.ChatResult]
}

func (completeSearch) GetStepType() string { return "CompleteCohereSearch" }
func (completeSearch) Execute(ctx dex.Context, result cohere.ChatResult) (*dex.StepDecision, error) {
	ranking, err := rankingAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Outcome{Results: ranking.Results, Summary: result.Value.Text}), nil
}
