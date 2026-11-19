// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/x/twitter/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// SearchFlowType is the stable recent search Flow identity.
	SearchFlowType = "XRecentPostSearch"
	searchStepType = "CallSearchRecentPosts"
)

var postPageAttribute = dex.DefineAttribute[x.PostPage]("x-post-page")

// SearchFlow reads a page of recent public Posts.
type SearchFlow struct {
	dex.FlowDefaults
	connection x.Connection
}

// NewSearchFlow binds the provider connection at registration time.
func NewSearchFlow(connection x.Connection) *SearchFlow { return &SearchFlow{connection: connection} }

func (*SearchFlow) GetFlowType() string { return SearchFlowType }
func (*SearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{postPageAttribute}}
}
func (flow *SearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSearch{}),
		dex.DefineStep(x.NewSearchRecentPostsStep(x.SearchRecentPostsStepConfig[x.SearchRecentPostsInput]{
			StepType: searchStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "X", Explanation: "Search public Posts from the last seven days."},
			Connection:  flow.connection, MapToOperationInput: mapSearchInput,
			Found: sdkgo.GoTo(completeSearch{}),
		})),
		dex.DefineStep(completeSearch{}),
	}
}
func (flow *SearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:x-post-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last search page in Dex Web.
func (*SearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPostPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"x-post-page": page}}, nil
}

// dex:field attribute-key:x-post-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last search page in Dex Web.
func (*SearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPostPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"x-post-page": page}}, nil
}

func readPostPage(ctx dex.Context) (x.PostPage, error) {
	page, err := postPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return x.PostPage{}, nil
	}
	return page, err
}

func mapSearchInput(input x.SearchRecentPostsInput) x.SearchRecentPostsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the search query to the provider Step."
type startSearch struct{ dex.StepDefaults }

func (startSearch) GetStepType() string { return "StartSearchRecentPosts" }
func (startSearch) WaitFor(dex.Context, x.SearchRecentPostsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSearch) Execute(_ dex.Context, input x.SearchRecentPostsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[x.SearchRecentPostsInput](searchStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the typed search page."
type completeSearch struct {
	dex.StepDefaultsNoWaitFor[x.SearchRecentPostsResult]
}

func (completeSearch) GetStepType() string { return "CompleteSearchRecentPosts" }
func (completeSearch) Execute(ctx dex.Context, result x.SearchRecentPostsResult) (*dex.StepDecision, error) {
	if err := postPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SearchFlow)(nil)
