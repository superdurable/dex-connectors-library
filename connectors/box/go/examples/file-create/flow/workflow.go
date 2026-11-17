// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createfile demonstrates bounded Box file upload.
package createfile

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/box/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "BoxCreateFile"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "box"
)

// Request supplies the destination, file name, and Base64 content.
type Request box.CreateBoxFileInput

var outcomeAttribute = dex.DefineAttribute[box.CreatedBoxFile]("box-file-outcome")

// Flow creates one Box file and completes with its ID.
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

// GetPersistenceSchema registers the created file result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created file ID.
// dex:field attribute-key:box-file-outcome value-type:json editable:false description:"Created file ID"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readFileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-file-outcome": value}}, nil
}

// GetDexDisplay returns the created file ID.
// dex:field attribute-key:box-file-outcome value-type:json editable:false description:"Created file ID"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readFileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"box-file-outcome": value}}, nil
}

func readFileOutcome(ctx dex.Context) (box.CreatedBoxFile, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return box.CreatedBoxFile{}, nil
	}
	return value, err
}

// GetSteps registers the file creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareFile{}),
		dex.DefineStep(box.NewCreateFileStep(box.CreateFileStepConfig[Request]{
			StepType: "CreateFile", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "box-file", GroupLabel: "Box file", Explanation: "Upload a bounded named file under the selected Box parent."},
			MapToOperationInput: func(request Request) box.CreateBoxFileInput { return box.CreateBoxFileInput(request) },
			Created:             sdkgo.GoTo(completeFile{}),
		})),
		dex.DefineStep(completeFile{}),
	}
}

// dex:group group-id:box-file-prepare group-label:"Prepare file"
// dex:explanation text:"Pass the typed file request into the provider create."
type prepareFile struct{ dex.StepDefaults }

func (prepareFile) GetStepType() string { return "PrepareFile" }
func (prepareFile) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareFile) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateFile"), request), nil
}

// dex:group group-id:box-file-complete group-label:"Complete file creation"
// dex:explanation text:"Complete with Box's created file identifier."
type completeFile struct {
	dex.StepDefaultsNoWaitFor[box.CreateFileResult]
}

func (completeFile) GetStepType() string { return "CompleteFile" }
func (completeFile) Execute(ctx dex.Context, result box.CreateFileResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
