// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package fileupload runs Dropbox uploadFile as one Dex Flow.
package fileupload

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DropboxFileUpload"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "dropbox"
	operationStepType = "CallUploadFile"
)

var uploadedFileAttribute = dex.DefineAttribute[dropbox.UploadedFile]("dropbox-uploaded-file")

// Flow runs UploadFile once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dropbox.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dropbox.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{uploadedFileAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dropbox.NewUploadFileStep(dropbox.UploadFileStepConfig[dropbox.UploadFileInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox", Explanation: "Call the provider's uploadFile operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Uploaded:            sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-uploaded-file value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUploadedFile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-uploaded-file": outcome}}, nil
}

// dex:field attribute-key:dropbox-uploaded-file value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUploadedFile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-uploaded-file": outcome}}, nil
}

func readUploadedFile(ctx dex.Context) (dropbox.UploadedFile, error) {
	outcome, err := uploadedFileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropbox.UploadedFile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input dropbox.UploadFileInput) dropbox.UploadFileInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartUploadFile" }
func (startRequest) WaitFor(dex.Context, dropbox.UploadFileInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dropbox.UploadFileInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropbox.UploadFileInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dropbox.UploadFileResult]
}

func (completeResult) GetStepType() string { return "CompleteUploadFile" }
func (completeResult) Execute(ctx dex.Context, result dropbox.UploadFileResult) (*dex.StepDecision, error) {
	if err := uploadedFileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
