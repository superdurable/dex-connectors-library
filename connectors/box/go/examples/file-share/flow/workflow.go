// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sharefile demonstrates Box file sharing.
package sharefile

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/box/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "BoxShareFile"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "box"
)

// Request selects the file and shared link access.
type Request box.ShareBoxFileInput

var outcomeAttribute = dex.DefineAttribute[box.UpdatedBoxItem]("box-shared-file")

// Flow shares one Box file and completes with its shared link metadata.
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

// GetPersistenceSchema registers the shared file result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the shared file metadata.
// dex:field attribute-key:box-shared-file value-type:json editable:false description:"Shared file"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readFileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-shared-file": value}}, nil
}

// GetDexDisplay returns the shared file metadata.
// dex:field attribute-key:box-shared-file value-type:json editable:false description:"Shared file"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readFileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-shared-file": value}}, nil
}

func readFileOutcome(ctx dex.Context) (box.UpdatedBoxItem, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return box.UpdatedBoxItem{}, nil
	}
	return value, err
}

// GetSteps registers the file sharing path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareFile{}),
		dex.DefineStep(box.NewShareFileStep(box.ShareFileStepConfig[Request]{
			StepType: "ShareFile", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "box-file", GroupLabel: "Box file", Explanation: "Create a shared link for the selected Box file."},
			MapToOperationInput: func(request Request) box.ShareBoxFileInput { return box.ShareBoxFileInput(request) },
			Shared:             sdkgo.GoTo(completeFile{}),
		})),
		dex.DefineStep(completeFile{}),
	}
}

// dex:group group-id:box-file-prepare group-label:"Prepare file"
// dex:explanation text:"Pass the typed file request into the provider sharing operation."
type prepareFile struct{ dex.StepDefaults }

func (prepareFile) GetStepType() string { return "PrepareFile" }
func (prepareFile) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareFile) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("ShareFile"), request), nil
}

// dex:group group-id:box-file-complete group-label:"Complete file sharing"
// dex:explanation text:"Complete with Box's shared file metadata."
type completeFile struct {
	dex.StepDefaultsNoWaitFor[box.ShareFileResult]
}

func (completeFile) GetStepType() string { return "CompleteFile" }
func (completeFile) Execute(ctx dex.Context, result box.ShareFileResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
