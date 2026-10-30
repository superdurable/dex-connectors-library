// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package photosearch defines a runnable Unsplash photo search Flow.
package photosearch

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/unsplash/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "UnsplashPhotoSearch"
	// ConnectionName is the static Dex Web connection used by this Flow.
	ConnectionName = "unsplash"
	searchStepType = "SearchUnsplashPhotos"
)

var outcomeAttribute = dex.DefineAttribute[unsplash.PhotoPage]("unsplash-photo-search-outcome")

// Request is the Dex Web Start Flow input.
type Request struct {
	// Query is the photo search phrase.
	Query string `json:"query"`
	// PerPage is the page size; zero selects ten.
	PerPage int `json:"perPage,omitempty"`
}

// Flow searches a page of public Unsplash photos.
type Flow struct {
	dex.FlowDefaults
	connection unsplash.Connection
}

// NewFlow binds a configured Unsplash connection.
func NewFlow(connection unsplash.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(unsplash.NewSearchPhotosStep(unsplash.SearchPhotosStepConfig[Request]{
			StepType: searchStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "photo-search", GroupLabel: "Photo search", Explanation: "Search Unsplash for public photos matching the phrase."},
			MapToOperationInput: MapToSearchPhotosInput,
			Searched:            sdkgo.GoTo(searched{}),
		})),
		dex.DefineStep(searched{}),
	}
}

// MapToSearchPhotosInput maps a validated request to the provider search.
func MapToSearchPhotosInput(request Request) unsplash.SearchPhotosInput {
	return unsplash.SearchPhotosInput{Query: request.Query, PerPage: request.PerPage}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetDexSummary returns the completed provider outcome.
// dex:field attribute-key:unsplash-photo-search-outcome value-type:json editable:false description:"Photo search results and attribution"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unsplash-photo-search-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:unsplash-photo-search-outcome value-type:json editable:false description:"Photo search results and attribution"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unsplash-photo-search-outcome": outcome}}, nil
}

func optionalOutcome(ctx dex.Context) (unsplash.PhotoPage, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return unsplash.PhotoPage{}, nil
	}
	return outcome, err
}

// dex:group group-id:photo-search group-label:"Photo search"
// dex:explanation text:"Validate the phrase and page size before searching Unsplash."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string                             { return "StartUnsplashPhotoSearch" }
func (start) WaitFor(dex.Context, Request) (*dex.Wait, error) { return dex.SkipWaitImmediately(), nil }
func (start) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	request.Query = strings.TrimSpace(request.Query)
	if request.Query == "" || request.PerPage < 0 || request.PerPage > 30 {
		return dex.ForceFail("query is required and perPage must be 1 through 30"), nil
	}
	if request.PerPage == 0 {
		request.PerPage = 10
	}
	return dex.GoTo(sdkgo.StepRef[Request](searchStepType), request), nil
}

// dex:group group-id:photo-search group-label:"Photo search"
// dex:explanation text:"Complete with the matching photo page and attribution metadata."
type searched struct {
	dex.StepDefaultsNoWaitFor[unsplash.SearchPhotosResult]
}

func (searched) GetStepType() string { return "UnsplashPhotosFound" }
func (searched) Execute(ctx dex.Context, result unsplash.SearchPhotosResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
