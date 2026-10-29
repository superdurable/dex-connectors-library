// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package photosearch defines a runnable Unsplash photo search Flow.
package photosearch

import (
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/unsplash"
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

func (*Flow) GetFlowType() string                         { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

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
func (searched) Execute(_ dex.Context, result unsplash.SearchPhotosResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
