// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package storyinspection runs Shortcut getStory as one Dex Flow.
package storyinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/shortcut/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ShortcutStoryInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "shortcut"
	operationStepType = "CallGetStory"
)

var storyAttribute = dex.DefineAttribute[shortcut.StoryMetadata]("shortcut-story")

// Flow runs GetStory once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection shortcut.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection shortcut.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{storyAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(shortcut.NewGetStoryStep(shortcut.GetStoryStepConfig[shortcut.GetStoryInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Shortcut", Explanation: "Read a story from Shortcut."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:shortcut-story value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readStory(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shortcut-story": outcome}}, nil
}

// dex:field attribute-key:shortcut-story value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readStory(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shortcut-story": outcome}}, nil
}

func readStory(ctx dex.Context) (shortcut.StoryMetadata, error) {
	outcome, err := storyAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return shortcut.StoryMetadata{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input shortcut.GetStoryInput) shortcut.GetStoryInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetStory" }
func (startRequest) WaitFor(dex.Context, shortcut.GetStoryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input shortcut.GetStoryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[shortcut.GetStoryInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[shortcut.GetStoryResult]
}

func (completeResult) GetStepType() string { return "CompleteGetStory" }
func (completeResult) Execute(ctx dex.Context, result shortcut.GetStoryResult) (*dex.StepDecision, error) {
	if err := storyAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
