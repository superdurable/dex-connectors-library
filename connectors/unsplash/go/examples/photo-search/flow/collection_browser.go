// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package photosearch

import (
	"github.com/superdurable/dex-connectors-library/connectors/unsplash/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CollectionBrowserFlowType identifies a public collection browsing Flow.
const CollectionBrowserFlowType = "UnsplashCollectionBrowser"

var collectionPageAttribute = dex.DefineAttribute[unsplash.CollectionList]("unsplash-collections")

// CollectionBrowserInput selects a public collection page.
type CollectionBrowserInput struct {
	// Page is the 1-based page; zero selects the first.
	Page int `json:"page,omitempty"`
	// PerPage is the page size; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// CollectionBrowserFlow browses public Unsplash collections.
type CollectionBrowserFlow struct {
	dex.FlowDefaults
	connection unsplash.Connection
}

// NewCollectionBrowserFlow binds the public Unsplash connection.
func NewCollectionBrowserFlow(connection unsplash.Connection) *CollectionBrowserFlow {
	return &CollectionBrowserFlow{connection: connection}
}

// GetFlowType returns CollectionBrowserFlowType.
func (*CollectionBrowserFlow) GetFlowType() string { return CollectionBrowserFlowType }

// GetPersistenceSchema registers the collection page Attribute.
func (*CollectionBrowserFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{collectionPageAttribute}}
}

// GetSteps declares the start, collection read, and completion Steps.
func (flow *CollectionBrowserFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCollectionBrowse{}),
		dex.DefineStep(unsplash.NewListCollectionsStep(unsplash.ListCollectionsStepConfig[CollectionBrowserInput]{
			StepType: "ListCollections", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "collections", GroupLabel: "Collections", Explanation: "Read a page of public Unsplash collections."},
			MapToOperationInput: func(input CollectionBrowserInput) unsplash.ListCollectionsInput {
				return unsplash.ListCollectionsInput{Page: input.Page, PerPage: input.PerPage}
			},
			Listed: sdkgo.GoTo(completeCollectionBrowse{}),
		})),
		dex.DefineStep(completeCollectionBrowse{}),
	}
}

// GetRPCs exposes collection metadata in Dex Web.
func (flow *CollectionBrowserFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the public collections in the run list.
// dex:field attribute-key:unsplash-collections value-type:json editable:false description:"Public Unsplash collections"
func (*CollectionBrowserFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := collectionPageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unsplash-collections": value}}, nil
}

// GetDexDisplay returns the public collections in run detail.
// dex:field attribute-key:unsplash-collections value-type:json editable:false description:"Public Unsplash collections and attribution links"
func (*CollectionBrowserFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := collectionPageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unsplash-collections": value}}, nil
}

// dex:group group-id:collections group-label:"Collections"
// dex:explanation text:"Validate the requested page before listing public Unsplash collections."
type startCollectionBrowse struct {
	dex.StepDefaultsNoWaitFor[CollectionBrowserInput]
}

func (startCollectionBrowse) GetStepType() string { return "StartCollectionBrowse" }
func (startCollectionBrowse) Execute(_ dex.Context, input CollectionBrowserInput) (*dex.StepDecision, error) {
	if input.Page < 0 || input.PerPage < 0 || input.PerPage > 30 {
		return dex.ForceFail("page and perPage must be valid"), nil
	}
	return dex.GoTo(sdkgo.StepRef[CollectionBrowserInput]("ListCollections"), input), nil
}

// dex:group group-id:collections group-label:"Collections"
// dex:explanation text:"Persist public collection metadata and complete the browsing Flow."
type completeCollectionBrowse struct {
	dex.StepDefaultsNoWaitFor[unsplash.ListCollectionsResult]
}

func (completeCollectionBrowse) GetStepType() string { return "CompleteCollectionBrowse" }
func (completeCollectionBrowse) Execute(ctx dex.Context, result unsplash.ListCollectionsResult) (*dex.StepDecision, error) {
	if err := collectionPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CollectionBrowserFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*CollectionBrowserFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*CollectionBrowserFlow)(nil).GetDexDisplay
