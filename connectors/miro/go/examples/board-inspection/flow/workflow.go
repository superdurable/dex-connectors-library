// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package boardinspection runs Miro getBoard as one Dex Flow.
package boardinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/miro/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MiroBoardInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "miro"
	operationStepType = "CallGetBoard"
)

var boardAttribute = dex.DefineAttribute[miro.BoardMetadata]("miro-board")

// Flow runs GetBoard once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection miro.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection miro.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{boardAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(miro.NewGetBoardStep(miro.GetBoardStepConfig[miro.GetBoardInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Miro", Explanation: "Read a board from Miro."},
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

// dex:field attribute-key:miro-board value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBoard(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"miro-board": outcome}}, nil
}

// dex:field attribute-key:miro-board value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBoard(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"miro-board": outcome}}, nil
}

func readBoard(ctx dex.Context) (miro.BoardMetadata, error) {
	outcome, err := boardAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return miro.BoardMetadata{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input miro.GetBoardInput) miro.GetBoardInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetBoard" }
func (startRequest) WaitFor(dex.Context, miro.GetBoardInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input miro.GetBoardInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[miro.GetBoardInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[miro.GetBoardResult]
}

func (completeResult) GetStepType() string { return "CompleteGetBoard" }
func (completeResult) Execute(ctx dex.Context, result miro.GetBoardResult) (*dex.StepDecision, error) {
	if err := boardAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
