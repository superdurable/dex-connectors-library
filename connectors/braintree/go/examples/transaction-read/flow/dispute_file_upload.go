// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DisputeFileUploadFlowType identifies the file evidence example.
const DisputeFileUploadFlowType = "BraintreeDisputeFileUpload"

const disputeFileUploadOperationStepType = "CallCreateDisputeFileEvidence"

var uploadedEvidenceAttribute = dex.DefineAttribute[braintree.DisputeEvidenceProfile]("braintree-dispute-evidence-uploaded")

// DisputeFileUploadFlow uploads one Braintree dispute file through GraphQL.
type DisputeFileUploadFlow struct {
	dex.FlowDefaults
	connection braintree.Connection
}

// NewDisputeFileUploadFlow binds the provider connection at registration time.
func NewDisputeFileUploadFlow(connection braintree.Connection) *DisputeFileUploadFlow {
	return &DisputeFileUploadFlow{connection: connection}
}

func (*DisputeFileUploadFlow) GetFlowType() string { return DisputeFileUploadFlowType }

func (*DisputeFileUploadFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{uploadedEvidenceAttribute}}
}

func (flow *DisputeFileUploadFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDisputeFileUploadRequest{}),
		dex.DefineStep(braintree.NewCreateDisputeFileEvidenceStep(braintree.CreateDisputeFileEvidenceStepConfig[braintree.CreateDisputeFileEvidenceInput]{
			StepType: disputeFileUploadOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braintree", Explanation: "Upload one immutable worker-mounted document as dispute evidence."},
			Connection:  flow.connection,
			MapToOperationInput: func(input braintree.CreateDisputeFileEvidenceInput) braintree.CreateDisputeFileEvidenceInput {
				return input
			},
			Created: sdkgo.GoTo(completeDisputeFileUploadResult{}),
		})),
		dex.DefineStep(completeDisputeFileUploadResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *DisputeFileUploadFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braintree-dispute-evidence-uploaded value-type:json editable:false description:"Uploaded evidence ID and category"
// GetDexSummary shows the uploaded evidence.
func (*DisputeFileUploadFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	evidence, err := readUploadedEvidence(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-dispute-evidence-uploaded": evidence}}, nil
}

// dex:field attribute-key:braintree-dispute-evidence-uploaded value-type:json editable:false description:"Uploaded evidence ID and category"
// GetDexDisplay shows the uploaded evidence.
func (*DisputeFileUploadFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	evidence, err := readUploadedEvidence(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-dispute-evidence-uploaded": evidence}}, nil
}

func readUploadedEvidence(ctx dex.Context) (braintree.DisputeEvidenceProfile, error) {
	evidence, err := uploadedEvidenceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braintree.DisputeEvidenceProfile{}, nil
	}
	return evidence, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the dispute ID and mounted file digest to the provider Step."
type startDisputeFileUploadRequest struct{ dex.StepDefaults }

func (startDisputeFileUploadRequest) GetStepType() string { return "StartCreateDisputeFileEvidence" }
func (startDisputeFileUploadRequest) WaitFor(dex.Context, braintree.CreateDisputeFileEvidenceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDisputeFileUploadRequest) Execute(_ dex.Context, input braintree.CreateDisputeFileEvidenceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braintree.CreateDisputeFileEvidenceInput](disputeFileUploadOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the uploaded evidence result."
type completeDisputeFileUploadResult struct {
	dex.StepDefaultsNoWaitFor[braintree.CreateDisputeFileEvidenceResult]
}

func (completeDisputeFileUploadResult) GetStepType() string {
	return "CompleteCreateDisputeFileEvidence"
}
func (completeDisputeFileUploadResult) Execute(ctx dex.Context, result braintree.CreateDisputeFileEvidenceResult) (*dex.StepDecision, error) {
	if err := uploadedEvidenceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DisputeFileUploadFlow)(nil)
