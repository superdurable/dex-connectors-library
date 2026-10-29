// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package moviesearch demonstrates one TMDB operation in a Dex Flow.
package moviesearch

import (
	"github.com/superdurable/dex-connectors-library/connectors/tmdb"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "TMDBMovieSearch"
	// ConnectionName is the static Dex Web connection for this example.
	ConnectionName    = "tmdb"
	operationStepType = "SearchMovies"
)

// Request is the typed start input for this example.
type Request tmdb.SearchMoviesInput

// Flow runs one TMDB request and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection tmdb.Connection
}

// NewFlow binds a configured TMDB connection.
func NewFlow(connection tmdb.Connection) *Flow { return &Flow{connection: connection} }
func (*Flow) GetFlowType() string              { return FlowType }
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareRequest{}),
		dex.DefineStep(tmdb.NewSearchMoviesStep(tmdb.SearchMoviesStepConfig[Request]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "tmdb", GroupLabel: "TMDB", Explanation: "Call the TMDB SearchMovies endpoint."},
			MapToOperationInput: func(request Request) tmdb.SearchMoviesInput { return tmdb.SearchMoviesInput(request) },
			Searched:            sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// dex:group group-id:tmdb-example group-label:"TMDB example"
// dex:explanation text:"Persist the typed start input before the provider operation."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareRequest" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](operationStepType), request), nil
}

// dex:group group-id:tmdb-example group-label:"TMDB example"
// dex:explanation text:"Complete the Flow with the provider result."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[tmdb.SearchMoviesResult]
}

func (completeRequest) GetStepType() string { return "CompleteRequest" }
func (completeRequest) Execute(_ dex.Context, result tmdb.SearchMoviesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
