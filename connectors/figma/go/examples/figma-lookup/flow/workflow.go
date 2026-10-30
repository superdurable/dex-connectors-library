// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package figmalookup demonstrates two Figma reads in a Dex Flow.
package figmalookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/figma/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex Flow identity.
const FlowType = "FigmaLookup"

// ConnectionName is the static Dex Web connection name.
const ConnectionName = "figma"
const firstStepType = "ReadFigmaFirst"
const secondStepType = "ReadFigmaSecond"

var fileKeyAttribute = dex.DefineAttribute[string]("figma-file-key")

// Request is the typed Start Flow input.
type Request struct {
	// FileKey is the key from a Figma file URL.
	FileKey string `json:"fileKey"`
}

// Outcome is the Flow completion output.
type Outcome struct {
	// Comments contains the provider data returned by the Flow.
	Comments []figma.FileComment `json:"comments"`
}

// Flow reads the first and second provider resources.
type Flow struct {
	dex.FlowDefaults
	connection figma.Connection
}

// NewFlow binds the static connection at registration.
func NewFlow(connection figma.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema declares the example's durable state.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{fileKeyAttribute}}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the example's read-only Dex Web summary.
func (*Flow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns the example's read-only Dex Web display.
func (*Flow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(figma.NewGetFileStep(figma.GetFileStepConfig[Request]{StepType: firstStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "figma-lookup", GroupLabel: "Figma lookup", Explanation: "Read Figma records."}, MapToOperationInput: MapToGetFileInput, Found: sdkgo.GoTo(afterFirst{})})),
		dex.DefineStep(afterFirst{}),
		dex.DefineStep(figma.NewGetFileCommentsStep(figma.GetFileCommentsStepConfig[figma.GetFileCommentsInput]{StepType: secondStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "figma-lookup", GroupLabel: "Figma lookup", Explanation: "Read the related Figma resource."}, MapToOperationInput: MapToGetFileCommentsInput, Listed: sdkgo.GoTo(completeLookup{})})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToGetFileInput maps the start input to the first read.
func MapToGetFileInput(request Request) figma.GetFileInput {
	return figma.GetFileInput{FileKey: request.FileKey}
}

// MapToGetFileCommentsInput maps the transition value to the second read.
func MapToGetFileCommentsInput(input figma.GetFileCommentsInput) figma.GetFileCommentsInput {
	return input
}

// dex:group group-id:figma-lookup group-label:"Figma lookup"
// dex:explanation text:"Start the Figma read with the typed request."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartFigmaLookup" }
func (startRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(ctx dex.Context, request Request) (*dex.StepDecision, error) {
	if request.FileKey == "" {
		return dex.ForceFail("fileKey is required"), nil
	}
	if err := fileKeyAttribute.Set(ctx, request.FileKey); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Request](firstStepType), request), nil
}

// dex:group group-id:figma-lookup group-label:"Figma lookup"
// dex:explanation text:"Choose the related Figma read."
type afterFirst struct {
	dex.StepDefaultsNoWaitFor[figma.GetFileResult]
}

func (afterFirst) GetStepType() string { return "AfterFigmaFirst" }
func (afterFirst) Execute(ctx dex.Context, result figma.GetFileResult) (*dex.StepDecision, error) {
	fileKey, err := fileKeyAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[figma.GetFileCommentsInput](secondStepType), figma.GetFileCommentsInput{FileKey: fileKey}), nil
}

// dex:group group-id:figma-lookup group-label:"Figma lookup"
// dex:explanation text:"Complete the Flow with Figma data."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[figma.GetFileCommentsResult]
}

func (completeLookup) GetStepType() string { return "CompleteFigmaLookup" }
func (completeLookup) Execute(_ dex.Context, result figma.GetFileCommentsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(Outcome{Comments: result.Value.Comments}), nil
}
