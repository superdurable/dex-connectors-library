// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package documentsearch runs an OpenSearch document query as a Dex Flow.
package documentsearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/opensearch/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OpenSearchDocumentSearch"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "opensearch"
	operationStepType = "CallSearchDocuments"
)

var searchPageAttribute = dex.DefineAttribute[opensearch.SearchPage]("opensearch-search-page")

// Flow searches one index and completes with a typed result page.
type Flow struct {
	dex.FlowDefaults
	connection opensearch.Connection
}

// NewFlow binds the OpenSearch connection at registration time.
func NewFlow(connection opensearch.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{searchPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(opensearch.NewSearchDocumentsStep(opensearch.SearchDocumentsStepConfig[opensearch.SearchDocumentsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OpenSearch", Explanation: "Search one index with a Lucene query."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:opensearch-search-page value-type:json editable:false description:"Search results"
// GetDexSummary shows the search page in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSearchPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opensearch-search-page": page}}, nil
}

// dex:field attribute-key:opensearch-search-page value-type:json editable:false description:"Search results"
// GetDexDisplay shows the search page in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSearchPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"opensearch-search-page": page}}, nil
}

func readSearchPage(ctx dex.Context) (opensearch.SearchPage, error) {
	page, err := searchPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return opensearch.SearchPage{}, nil
	}
	return page, err
}

func mapToOperationInput(input opensearch.SearchDocumentsInput) opensearch.SearchDocumentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the search input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSearchDocuments" }
func (startRequest) WaitFor(dex.Context, opensearch.SearchDocumentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input opensearch.SearchDocumentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[opensearch.SearchDocumentsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the search results."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[opensearch.SearchDocumentsResult]
}

func (completeResult) GetStepType() string { return "CompleteSearchDocuments" }
func (completeResult) Execute(ctx dex.Context, result opensearch.SearchDocumentsResult) (*dex.StepDecision, error) {
	if err := searchPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
