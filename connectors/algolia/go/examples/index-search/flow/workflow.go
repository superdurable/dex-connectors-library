// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package indexsearch defines a runnable Algolia index search Flow.
package indexsearch

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/algolia"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "AlgoliaIndexSearch"
	// ConnectionName is the static Dex Web connection used by this Flow.
	ConnectionName = "algolia"
	searchStepType = "SearchAlgoliaIndex"
)

var outcomeAttribute = dex.DefineAttribute[algolia.SearchPage]("algolia-index-search-outcome")

// Request is the Dex Web Start Flow input.
type Request struct {
	// IndexName names an existing Algolia index in the connected application.
	IndexName string `json:"indexName"`
	// Query is the search phrase; blank matches all records.
	Query string `json:"query"`
}

// Flow searches one Algolia index.
type Flow struct {
	dex.FlowDefaults
	connection algolia.Connection
}

// NewFlow binds the configured Algolia connection.
func NewFlow(connection algolia.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(algolia.NewSearchStep(algolia.SearchStepConfig[Request]{
			StepType: searchStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "index-search", GroupLabel: "Index search", Explanation: "Search the connected Algolia index for matching records."},
			MapToOperationInput: MapToSearchInput,
			Searched:            sdkgo.GoTo(searched{}),
		})),
		dex.DefineStep(searched{}),
	}
}

// MapToSearchInput maps the validated request to one bounded Algolia search.
func MapToSearchInput(request Request) algolia.SearchInput {
	return algolia.SearchInput{IndexName: request.IndexName, Query: request.Query, HitsPerPage: 10}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetDexSummary returns the completed provider outcome.
// dex:field attribute-key:algolia-index-search-outcome value-type:json editable:false description:"Index search results"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"algolia-index-search-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:algolia-index-search-outcome value-type:json editable:false description:"Index search results"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"algolia-index-search-outcome": outcome}}, nil
}

func optionalOutcome(ctx dex.Context) (algolia.SearchPage, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return algolia.SearchPage{}, nil
	}
	return outcome, err
}

// dex:group group-id:index-search group-label:"Index search"
// dex:explanation text:"Validate the index name before searching Algolia."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string                             { return "StartAlgoliaIndexSearch" }
func (start) WaitFor(dex.Context, Request) (*dex.Wait, error) { return dex.SkipWaitImmediately(), nil }
func (start) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	request.IndexName = strings.TrimSpace(request.IndexName)
	if request.IndexName == "" {
		return dex.ForceFail("indexName is required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[Request](searchStepType), request), nil
}

// dex:group group-id:index-search group-label:"Index search"
// dex:explanation text:"Complete with the matching record identities."
type searched struct {
	dex.StepDefaultsNoWaitFor[algolia.SearchResult]
}

func (searched) GetStepType() string { return "AlgoliaHitsFound" }
func (searched) Execute(ctx dex.Context, result algolia.SearchResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
