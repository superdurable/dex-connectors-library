// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package foldercontinuation runs Dropbox continueFolder as one Dex Flow.
package foldercontinuation

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DropboxFolderContinuation"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "dropbox"
	operationStepType = "CallContinueFolder"
)

var folderPageAttribute = dex.DefineAttribute[dropbox.FolderPage]("dropbox-continued-entries")

// Flow runs ContinueFolder once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dropbox.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dropbox.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{folderPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dropbox.NewContinueFolderStep(dropbox.ContinueFolderStepConfig[dropbox.ContinueFolderInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox", Explanation: "Call the provider's continueFolder operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-continued-entries value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFolderPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-continued-entries": outcome}}, nil
}

// dex:field attribute-key:dropbox-continued-entries value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFolderPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-continued-entries": outcome}}, nil
}

func readFolderPage(ctx dex.Context) (dropbox.FolderPage, error) {
	outcome, err := folderPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropbox.FolderPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input dropbox.ContinueFolderInput) dropbox.ContinueFolderInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartContinueFolder" }
func (startRequest) WaitFor(dex.Context, dropbox.ContinueFolderInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dropbox.ContinueFolderInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropbox.ContinueFolderInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dropbox.ContinueFolderResult]
}

func (completeResult) GetStepType() string { return "CompleteContinueFolder" }
func (completeResult) Execute(ctx dex.Context, result dropbox.ContinueFolderResult) (*dex.StepDecision, error) {
	if err := folderPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
