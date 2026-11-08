// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package agreementinspection runs Adobe Acrobat Sign getAgreement as one Dex Flow.
package agreementinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/acrobat-sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AdobeAcrobatSignAgreementInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "adobe-acrobat-sign"
	operationStepType = "CallGetAgreement"
)

var agreementAttribute = dex.DefineAttribute[acrobatsign.AgreementStatus]("adobe-acrobat-sign-agreement")

// Flow runs GetAgreement once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection acrobatsign.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection acrobatsign.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{agreementAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(acrobatsign.NewGetAgreementStep(acrobatsign.GetAgreementStepConfig[acrobatsign.GetAgreementInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adobe Acrobat Sign", Explanation: "Read an agreement from Adobe Acrobat Sign."},
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

// dex:field attribute-key:adobe-acrobat-sign-agreement value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAgreement(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-agreement": outcome}}, nil
}

// dex:field attribute-key:adobe-acrobat-sign-agreement value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAgreement(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-agreement": outcome}}, nil
}

func readAgreement(ctx dex.Context) (acrobatsign.AgreementStatus, error) {
	outcome, err := agreementAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return acrobatsign.AgreementStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input acrobatsign.GetAgreementInput) acrobatsign.GetAgreementInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAgreement" }
func (startRequest) WaitFor(dex.Context, acrobatsign.GetAgreementInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input acrobatsign.GetAgreementInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[acrobatsign.GetAgreementInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[acrobatsign.GetAgreementResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAgreement" }
func (completeResult) Execute(ctx dex.Context, result acrobatsign.GetAgreementResult) (*dex.StepDecision, error) {
	if err := agreementAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
