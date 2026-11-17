// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createfolder demonstrates an absolute Box folder creation.
package createfolder

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/box/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "BoxCreateFolder"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "box"
)

// Request supplies the destination parent and folder name.
type Request box.CreateBoxFolderInput

var outcomeAttribute = dex.DefineAttribute[box.CreatedBoxItem]("box-folder-outcome")

// Flow creates one Box folder and completes with its ID.
type Flow struct {
	dex.FlowDefaults
	connection box.Connection
}

// NewFlow binds a Box connection.
func NewFlow(connection box.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the created folder result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created folder ID.
// dex:field attribute-key:box-folder-outcome value-type:json editable:false description:"Created folder ID"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readFolderOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-folder-outcome": value}}, nil
}

// GetDexDisplay returns the created folder ID.
// dex:field attribute-key:box-folder-outcome value-type:json editable:false description:"Created folder ID"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readFolderOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-folder-outcome": value}}, nil
}

func readFolderOutcome(ctx dex.Context) (box.CreatedBoxItem, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return box.CreatedBoxItem{}, nil
	}
	return value, err
}

// GetSteps registers the folder creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareFolder{}),
		dex.DefineStep(box.NewCreateFolderStep(box.CreateFolderStepConfig[Request]{
			StepType: "CreateFolder", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "box-folder", GroupLabel: "Box folder", Explanation: "Create a named folder under the selected Box parent."},
			MapToOperationInput: func(request Request) box.CreateBoxFolderInput { return box.CreateBoxFolderInput(request) },
			Created:             sdkgo.GoTo(completeFolder{}),
		})),
		dex.DefineStep(completeFolder{}),
	}
}

// dex:group group-id:box-folder-prepare group-label:"Prepare folder"
// dex:explanation text:"Pass the typed folder request into the provider create."
type prepareFolder struct{ dex.StepDefaults }

func (prepareFolder) GetStepType() string { return "PrepareFolder" }
func (prepareFolder) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareFolder) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateFolder"), request), nil
}

// dex:group group-id:box-folder-complete group-label:"Complete folder creation"
// dex:explanation text:"Complete with Box's created folder identifier."
type completeFolder struct {
	dex.StepDefaultsNoWaitFor[box.CreateFolderResult]
}

func (completeFolder) GetStepType() string { return "CompleteFolder" }
func (completeFolder) Execute(ctx dex.Context, result box.CreateFolderResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
