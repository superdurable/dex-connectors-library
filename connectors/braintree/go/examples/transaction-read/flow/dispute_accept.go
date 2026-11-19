// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DisputeAcceptFlowType identifies the dispute acceptance example.
const DisputeAcceptFlowType = "BraintreeDisputeAccept"

const disputeAcceptOperationStepType = "CallAcceptDispute"

var acceptedDisputeAttribute = dex.DefineAttribute[braintree.DisputeProfile]("braintree-dispute-accepted")

// DisputeAcceptFlow accepts one Braintree dispute through GraphQL.
type DisputeAcceptFlow struct {
	dex.FlowDefaults
	connection braintree.Connection
}

// NewDisputeAcceptFlow binds the provider connection at registration time.
func NewDisputeAcceptFlow(connection braintree.Connection) *DisputeAcceptFlow {
	return &DisputeAcceptFlow{connection: connection}
}

func (*DisputeAcceptFlow) GetFlowType() string { return DisputeAcceptFlowType }

func (*DisputeAcceptFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{acceptedDisputeAttribute}}
}

func (flow *DisputeAcceptFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDisputeAcceptRequest{}),
		dex.DefineStep(braintree.NewAcceptDisputeStep(braintree.AcceptDisputeStepConfig[braintree.GetDisputeInput]{
			StepType: disputeAcceptOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braintree", Explanation: "Accept one open dispute after checking its current status."},
			Connection:          flow.connection,
			MapToOperationInput: func(input braintree.GetDisputeInput) braintree.GetDisputeInput { return input },
			Accepted:            sdkgo.GoTo(completeDisputeAcceptResult{}),
		})),
		dex.DefineStep(completeDisputeAcceptResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *DisputeAcceptFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braintree-dispute-accepted value-type:json editable:false description:"Provider result"
// GetDexSummary shows the accepted dispute.
func (*DisputeAcceptFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	dispute, err := readAcceptedDispute(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-dispute-accepted": dispute}}, nil
}

// dex:field attribute-key:braintree-dispute-accepted value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the accepted dispute.
func (*DisputeAcceptFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	dispute, err := readAcceptedDispute(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-dispute-accepted": dispute}}, nil
}

func readAcceptedDispute(ctx dex.Context) (braintree.DisputeProfile, error) {
	dispute, err := acceptedDisputeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braintree.DisputeProfile{}, nil
	}
	return dispute, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected dispute ID to the provider Step."
type startDisputeAcceptRequest struct{ dex.StepDefaults }

func (startDisputeAcceptRequest) GetStepType() string { return "StartAcceptDispute" }
func (startDisputeAcceptRequest) WaitFor(dex.Context, braintree.GetDisputeInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDisputeAcceptRequest) Execute(_ dex.Context, input braintree.GetDisputeInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braintree.GetDisputeInput](disputeAcceptOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the dispute result."
type completeDisputeAcceptResult struct {
	dex.StepDefaultsNoWaitFor[braintree.AcceptDisputeResult]
}

func (completeDisputeAcceptResult) GetStepType() string { return "CompleteAcceptDispute" }
func (completeDisputeAcceptResult) Execute(ctx dex.Context, result braintree.AcceptDisputeResult) (*dex.StepDecision, error) {
	if err := acceptedDisputeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DisputeAcceptFlow)(nil)
