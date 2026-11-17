// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package agreementinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/acrobat-sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const cancelAgreementStepType = "CallCancelAgreement"

var cancelledAgreementAttribute = dex.DefineAttribute[acrobatsign.CancelledAgreement]("adobe-acrobat-sign-cancelled-agreement")

// CancelAgreementFlow cancels one agreement and stores its final status.
type CancelAgreementFlow struct {
	dex.FlowDefaults
	connection acrobatsign.Connection
}

// NewCancelAgreementFlow binds the same static Acrobat Sign connection.
func NewCancelAgreementFlow(connection acrobatsign.Connection) *CancelAgreementFlow {
	return &CancelAgreementFlow{connection: connection}
}

// GetFlowType is the stable cancellation example identity.
func (*CancelAgreementFlow) GetFlowType() string { return "AdobeAcrobatSignAgreementCancel" }

// GetPersistenceSchema registers the cancellation result shown by Dex Web.
func (*CancelAgreementFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{cancelledAgreementAttribute}}
}

// GetSteps registers one read boundary and a typed completion.
func (flow *CancelAgreementFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAgreementCancel{}),
		dex.DefineStep(acrobatsign.NewCancelAgreementStep(acrobatsign.CancelAgreementStepConfig[acrobatsign.CancelAgreementInput]{
			StepType: cancelAgreementStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adobe Acrobat Sign", Explanation: "Cancel one agreement after checking its current status."},
			Connection:  flow.connection, MapToOperationInput: MapToCancelAgreementInput,
			Cancelled: sdkgo.GoTo(completeAgreementCancel{}),
		})),
		dex.DefineStep(completeAgreementCancel{}),
	}
}

// GetRPCs exposes the stored status in Dex Web.
func (flow *CancelAgreementFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adobe-acrobat-sign-cancelled-agreement value-type:json editable:false description:"Cancelled agreement status"
// GetDexSummary shows the cancelled agreement in Dex Web.
func (*CancelAgreementFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCancelledAgreement(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-cancelled-agreement": page}}, nil
}

// dex:field attribute-key:adobe-acrobat-sign-cancelled-agreement value-type:json editable:false description:"Cancelled agreement status"
// GetDexDisplay shows the cancelled agreement in Dex Web.
func (*CancelAgreementFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCancelledAgreement(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adobe-acrobat-sign-cancelled-agreement": page}}, nil
}

func readCancelledAgreement(ctx dex.Context) (acrobatsign.CancelledAgreement, error) {
	page, err := cancelledAgreementAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return acrobatsign.CancelledAgreement{}, nil
	}
	return page, err
}

// MapToCancelAgreementInput preserves the typed agreement ID.
func MapToCancelAgreementInput(input acrobatsign.CancelAgreementInput) acrobatsign.CancelAgreementInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the agreement ID to the provider Step."
type startAgreementCancel struct{ dex.StepDefaults }

func (startAgreementCancel) GetStepType() string { return "StartCancelAgreement" }
func (startAgreementCancel) WaitFor(dex.Context, acrobatsign.CancelAgreementInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAgreementCancel) Execute(_ dex.Context, input acrobatsign.CancelAgreementInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[acrobatsign.CancelAgreementInput](cancelAgreementStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the cancelled agreement status."
type completeAgreementCancel struct {
	dex.StepDefaultsNoWaitFor[acrobatsign.CancelAgreementResult]
}

func (completeAgreementCancel) GetStepType() string { return "CompleteCancelAgreement" }
func (completeAgreementCancel) Execute(ctx dex.Context, result acrobatsign.CancelAgreementResult) (*dex.StepDecision, error) {
	if err := cancelledAgreementAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CancelAgreementFlow)(nil)
