// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package objectmetadata runs FXiaoke listObjectMetadata as one Dex Flow.
package objectmetadata

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/fxiaoke/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FXiaokeObjectMetadata"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "fxiaoke"
	operationStepType = "CallListObjectMetadata"
)

var objectMetadataOutcomeAttribute = dex.DefineAttribute[fxiaoke.ListObjectMetadataOutput]("fxiaoke-list-object-metadata-outcome")

// Flow runs ListObjectMetadata once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection fxiaoke.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection fxiaoke.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{objectMetadataOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(fxiaoke.NewListObjectMetadataStep(fxiaoke.ListObjectMetadataStepConfig[fxiaoke.ListObjectMetadataInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "FXiaoke", Explanation: "Call the provider's listObjectMetadata operation."},
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

// dex:field attribute-key:fxiaoke-list-object-metadata-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListObjectMetadataOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fxiaoke-list-object-metadata-outcome": outcome}}, nil
}

// dex:field attribute-key:fxiaoke-list-object-metadata-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListObjectMetadataOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fxiaoke-list-object-metadata-outcome": outcome}}, nil
}

func readListObjectMetadataOutcome(ctx dex.Context) (fxiaoke.ListObjectMetadataOutput, error) {
	outcome, err := objectMetadataOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return fxiaoke.ListObjectMetadataOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input fxiaoke.ListObjectMetadataInput) fxiaoke.ListObjectMetadataInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListObjectMetadata" }
func (startRequest) WaitFor(dex.Context, fxiaoke.ListObjectMetadataInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input fxiaoke.ListObjectMetadataInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[fxiaoke.ListObjectMetadataInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[fxiaoke.ListObjectMetadataResult]
}

func (completeResult) GetStepType() string { return "CompleteListObjectMetadata" }
func (completeResult) Execute(ctx dex.Context, result fxiaoke.ListObjectMetadataResult) (*dex.StepDecision, error) {
	if err := objectMetadataOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
