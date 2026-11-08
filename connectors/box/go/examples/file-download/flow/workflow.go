// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package filedownload runs Box downloadFile as one Dex Flow.
package filedownload

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/box/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BoxFileDownload"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "box"
	operationStepType = "CallDownloadFile"
)

var downloadedFileAttribute = dex.DefineAttribute[box.DownloadedFile]("box-downloaded-file")

// Flow runs DownloadFile once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection box.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection box.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{downloadedFileAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(box.NewDownloadFileStep(box.DownloadFileStepConfig[box.DownloadFileInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Box", Explanation: "Call the provider's downloadFile operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Downloaded:          sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:box-downloaded-file value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDownloadedFile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-downloaded-file": outcome}}, nil
}

// dex:field attribute-key:box-downloaded-file value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDownloadedFile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-downloaded-file": outcome}}, nil
}

func readDownloadedFile(ctx dex.Context) (box.DownloadedFile, error) {
	outcome, err := downloadedFileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return box.DownloadedFile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input box.DownloadFileInput) box.DownloadFileInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartDownloadFile" }
func (startRequest) WaitFor(dex.Context, box.DownloadFileInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input box.DownloadFileInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[box.DownloadFileInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[box.DownloadFileResult]
}

func (completeResult) GetStepType() string { return "CompleteDownloadFile" }
func (completeResult) Execute(ctx dex.Context, result box.DownloadFileResult) (*dex.StepDecision, error) {
	if err := downloadedFileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
