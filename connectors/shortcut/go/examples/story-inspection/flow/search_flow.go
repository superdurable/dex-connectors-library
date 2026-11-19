// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package storyinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/shortcut/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// SearchFlowType identifies the story search example Flow.
	SearchFlowType = "ShortcutStorySearch"
	searchStepType = "CallSearchStories"
)

var searchAttribute = dex.DefineAttribute[shortcut.SearchStoriesOutput]("shortcut-story-search")

// SearchFlow runs one story search and retains its bounded result page.
type SearchFlow struct {
	dex.FlowDefaults
	connection shortcut.Connection
}

// NewSearchFlow binds the static Shortcut connection for the search example.
func NewSearchFlow(connection shortcut.Connection) *SearchFlow {
	return &SearchFlow{connection: connection}
}

func (*SearchFlow) GetFlowType() string { return SearchFlowType }

func (*SearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{searchAttribute}}
}

func (flow *SearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSearch{}),
		dex.DefineStep(shortcut.NewSearchStoriesStep(shortcut.SearchStoriesStepConfig[shortcut.SearchStoriesInput]{
			StepType: searchStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Shortcut", Explanation: "Search Shortcut stories."},
			Connection:          flow.connection,
			MapToOperationInput: mapSearchInput,
			Found:               sdkgo.GoTo(completeSearch{}),
		})),
		dex.DefineStep(completeSearch{}),
	}
}

// GetRPCs exposes the search page in Dex Web.
func (flow *SearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:shortcut-story-search value-type:json editable:false description:"Search result"
// GetDexSummary returns the latest retained search page.
func (*SearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	output, err := readSearch(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shortcut-story-search": output}}, nil
}

// dex:field attribute-key:shortcut-story-search value-type:json editable:false description:"Search result"
// GetDexDisplay returns the latest retained search page.
func (*SearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	output, err := readSearch(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shortcut-story-search": output}}, nil
}

func readSearch(ctx dex.Context) (shortcut.SearchStoriesOutput, error) {
	output, err := searchAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return shortcut.SearchStoriesOutput{}, nil
	}
	return output, err
}

func mapSearchInput(input shortcut.SearchStoriesInput) shortcut.SearchStoriesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the story search input to Shortcut."
type startSearch struct{ dex.StepDefaults }

func (startSearch) GetStepType() string { return "StartSearchStories" }
func (startSearch) WaitFor(dex.Context, shortcut.SearchStoriesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSearch) Execute(_ dex.Context, input shortcut.SearchStoriesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[shortcut.SearchStoriesInput](searchStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store and complete with the Shortcut story search page."
type completeSearch struct {
	dex.StepDefaultsNoWaitFor[shortcut.SearchStoriesResult]
}

func (completeSearch) GetStepType() string { return "CompleteSearchStories" }
func (completeSearch) Execute(ctx dex.Context, result shortcut.SearchStoriesResult) (*dex.StepDecision, error) {
	if err := searchAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SearchFlow)(nil)
