// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package moviesearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tmdb/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const tvFlowType = "TMDBTVSearch"
const tvOperationStepType = "SearchTV"

var tvOutcomeAttribute = dex.DefineAttribute[tmdb.TVPage]("tmdb-tv-search-outcome")

// TVRequest starts a TV title search.
type TVRequest tmdb.SearchTVInput

// TVFlow searches the public TV catalog.
type TVFlow struct {
	dex.FlowDefaults
	connection tmdb.Connection
}

// NewTVFlow binds the static TMDB connection.
func NewTVFlow(connection tmdb.Connection) *TVFlow { return &TVFlow{connection: connection} }

// GetFlowType returns the stable TV example identity.
func (*TVFlow) GetFlowType() string { return tvFlowType }

// GetSteps registers the TV search and completion Steps.
func (flow *TVFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareTVRequest{}),
		dex.DefineStep(tmdb.NewSearchTVStep(tmdb.SearchTVStepConfig[TVRequest]{
			StepType: tvOperationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "tmdb-tv", GroupLabel: "TMDB TV", Explanation: "Search TMDB for TV series."},
			MapToOperationInput: func(request TVRequest) tmdb.SearchTVInput { return tmdb.SearchTVInput(request) },
			Searched:            sdkgo.GoTo(completeTVRequest{}),
		})),
		dex.DefineStep(completeTVRequest{}),
	}
}

// GetRPCs registers the summary and display views.
func (flow *TVFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the TV search result.
func (*TVFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tvOutcomeAttribute}}
}

// GetDexSummary returns the saved TV search page.
// dex:field attribute-key:tmdb-tv-search-outcome value-type:json editable:false description:"TV search page"
func (*TVFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTVOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tmdb-tv-search-outcome": outcome}}, nil
}

// GetDexDisplay returns the saved TV search page.
// dex:field attribute-key:tmdb-tv-search-outcome value-type:json editable:false description:"TV search page"
func (*TVFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTVOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tmdb-tv-search-outcome": outcome}}, nil
}

func readTVOutcome(ctx dex.Context) (tmdb.TVPage, error) {
	outcome, err := tvOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tmdb.TVPage{}, nil
	}
	return outcome, err
}

// dex:group group-id:tmdb-tv-prepare group-label:"Prepare TV request"
// dex:explanation text:"Pass the typed TV search input to the connector Step."
type prepareTVRequest struct{ dex.StepDefaults }

func (prepareTVRequest) GetStepType() string { return "PrepareTVRequest" }
func (prepareTVRequest) WaitFor(dex.Context, TVRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareTVRequest) Execute(_ dex.Context, request TVRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[TVRequest](tvOperationStepType), request), nil
}

// dex:group group-id:tmdb-tv-complete group-label:"Complete TV request"
// dex:explanation text:"Persist and return the TV search page."
type completeTVRequest struct {
	dex.StepDefaultsNoWaitFor[tmdb.SearchTVResult]
}

func (completeTVRequest) GetStepType() string { return "CompleteTVRequest" }
func (completeTVRequest) Execute(ctx dex.Context, result tmdb.SearchTVResult) (*dex.StepDecision, error) {
	if err := tvOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TVFlow)(nil)
