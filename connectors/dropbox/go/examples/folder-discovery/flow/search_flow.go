// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package folderdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SearchFlowType is the stable file search example identity.
const SearchFlowType = "DropboxFileSearch"

var searchPageAttribute = dex.DefineAttribute[dropbox.SearchFilesPage]("dropbox-search-results")

// SearchFlow runs one bounded Dropbox file search.
type SearchFlow struct {
	dex.FlowDefaults
	connection dropbox.Connection
}

// NewSearchFlow binds the provider connection at registration time.
func NewSearchFlow(connection dropbox.Connection) *SearchFlow {
	return &SearchFlow{connection: connection}
}

// GetFlowType preserves the file search Flow identity.
func (*SearchFlow) GetFlowType() string { return SearchFlowType }

// GetPersistenceSchema registers the search result Attribute.
func (*SearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{searchPageAttribute}}
}

// GetSteps registers the operation-specific search Step.
func (flow *SearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSearch{}),
		dex.DefineStep(dropbox.NewSearchFilesStep(dropbox.SearchFilesStepConfig[dropbox.SearchFilesInput]{
			StepType: "CallSearchFiles", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox", Explanation: "Search one page of Dropbox files and folders."},
			Connection:          flow.connection,
			MapToOperationInput: mapSearchInput,
			Found:               sdkgo.GoTo(completeSearch{}),
		})),
		dex.DefineStep(completeSearch{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *SearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-search-results value-type:json editable:false description:"Search results"
// GetDexSummary shows the last search page in Dex Web.
func (*SearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSearchPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-search-results": page}}, nil
}

// dex:field attribute-key:dropbox-search-results value-type:json editable:false description:"Search results"
// GetDexDisplay shows the last search page in Dex Web.
func (*SearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSearchPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-search-results": page}}, nil
}

func readSearchPage(ctx dex.Context) (dropbox.SearchFilesPage, error) {
	page, err := searchPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropbox.SearchFilesPage{}, nil
	}
	return page, err
}

func mapSearchInput(input dropbox.SearchFilesInput) dropbox.SearchFilesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the file search request to Dropbox."
type startSearch struct{ dex.StepDefaults }

func (startSearch) GetStepType() string { return "StartFileSearch" }
func (startSearch) WaitFor(dex.Context, dropbox.SearchFilesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSearch) Execute(_ dex.Context, input dropbox.SearchFilesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropbox.SearchFilesInput]("CallSearchFiles"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with one page of Dropbox search results."
type completeSearch struct {
	dex.StepDefaultsNoWaitFor[dropbox.SearchFilesResult]
}

func (completeSearch) GetStepType() string { return "CompleteFileSearch" }
func (completeSearch) Execute(ctx dex.Context, result dropbox.SearchFilesResult) (*dex.StepDecision, error) {
	if err := searchPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SearchFlow)(nil)
