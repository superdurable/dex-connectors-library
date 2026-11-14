// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package folderfiles runs KingsoftOffice listFolderFiles as one Dex Flow.
package folderfiles

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/kingsoftoffice/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WPSListFolderFiles"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "kingsoftoffice"
	operationStepType = "CallListFolderFiles"
)

var folderFilesOutcomeAttribute = dex.DefineAttribute[kingsoftoffice.ListFolderFilesOutput]("kingsoftoffice-folder-files-outcome")

// Flow runs ListFolderFiles once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection kingsoftoffice.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection kingsoftoffice.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{folderFilesOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(kingsoftoffice.NewListFolderFilesStep(kingsoftoffice.ListFolderFilesStepConfig[kingsoftoffice.ListFolderFilesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "KingsoftOffice", Explanation: "List one page of file metadata in the requested WPS drive folder."},
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

// dex:field attribute-key:kingsoftoffice-folder-files-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFolderFilesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kingsoftoffice-folder-files-outcome": outcome}}, nil
}

// dex:field attribute-key:kingsoftoffice-folder-files-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFolderFilesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kingsoftoffice-folder-files-outcome": outcome}}, nil
}

func readFolderFilesOutcome(ctx dex.Context) (kingsoftoffice.ListFolderFilesOutput, error) {
	outcome, err := folderFilesOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kingsoftoffice.ListFolderFilesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input kingsoftoffice.ListFolderFilesInput) kingsoftoffice.ListFolderFilesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListFolderFiles" }
func (startRequest) WaitFor(dex.Context, kingsoftoffice.ListFolderFilesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input kingsoftoffice.ListFolderFilesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kingsoftoffice.ListFolderFilesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[kingsoftoffice.ListFolderFilesResult]
}

func (completeResult) GetStepType() string { return "CompleteListFolderFiles" }
func (completeResult) Execute(ctx dex.Context, result kingsoftoffice.ListFolderFilesResult) (*dex.StepDecision, error) {
	if err := folderFilesOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
