// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package packageinspection runs OneSpanSign getPackage as one Dex Flow.
package packageinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/onespan/sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OneSpanSignPackageInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "onespan-sign"
	operationStepType = "CallGetPackage"
)

var packageAttribute = dex.DefineAttribute[onespansign.PackageStatus]("onespan-sign-package")

// Flow runs GetPackage once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection onespansign.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection onespansign.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{packageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(onespansign.NewGetPackageStep(onespansign.GetPackageStepConfig[onespansign.GetPackageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OneSpan Sign", Explanation: "Read a package from OneSpan Sign."},
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

// dex:field attribute-key:onespan-sign-package value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPackage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"onespan-sign-package": outcome}}, nil
}

// dex:field attribute-key:onespan-sign-package value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPackage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"onespan-sign-package": outcome}}, nil
}

func readPackage(ctx dex.Context) (onespansign.PackageStatus, error) {
	outcome, err := packageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return onespansign.PackageStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input onespansign.GetPackageInput) onespansign.GetPackageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPackage" }
func (startRequest) WaitFor(dex.Context, onespansign.GetPackageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input onespansign.GetPackageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[onespansign.GetPackageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[onespansign.GetPackageResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPackage" }
func (completeResult) Execute(ctx dex.Context, result onespansign.GetPackageResult) (*dex.StepDecision, error) {
	if err := packageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
