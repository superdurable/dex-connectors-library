// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"
	"fmt"

	"github.com/superdurable/dex-connectors-library/connectors/paypal/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// PayoutBatchFlowType is the stable identity of the payout batch read example.
const PayoutBatchFlowType = "PayPalPayoutBatchRead"

const payoutBatchStepType = "ReadPayPalPayoutBatch"

// PayoutBatchRequest selects one payout batch.
type PayoutBatchRequest struct {
	// BatchID is the PayPal payout batch ID.
	BatchID string `json:"batchID"`
}

// PayoutBatchOutcome is the completed payout read.
type PayoutBatchOutcome struct {
	// Batch is the provider's payout batch summary.
	Batch paypal.PayoutBatch `json:"batch"`
}

var payoutBatchOutcomeAttribute = dex.DefineAttribute[PayoutBatchOutcome]("paypal-payout-batch-outcome")

// PayoutBatchFlow reads a batch through the declared PayPal connection.
type PayoutBatchFlow struct {
	dex.FlowDefaults
	connection paypal.Connection
}

// NewPayoutBatchFlow binds the trusted connection at registration.
func NewPayoutBatchFlow(connection paypal.Connection) *PayoutBatchFlow {
	return &PayoutBatchFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*PayoutBatchFlow) GetFlowType() string { return PayoutBatchFlowType }

// GetPersistenceSchema registers the outcome attribute.
func (*PayoutBatchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{payoutBatchOutcomeAttribute}}
}

// GetSteps returns the start, provider read, and completion steps.
func (flow *PayoutBatchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(preparePayoutBatchRequest{}),
		dex.DefineStep(paypal.NewGetPayoutBatchStep(paypal.GetPayoutBatchStepConfig[PayoutBatchRequest]{
			StepType: payoutBatchStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "paypal-payout", GroupLabel: "PayPal payout", Explanation: "Read the payout batch details."},
			MapToOperationInput: func(request PayoutBatchRequest) paypal.GetPayoutBatchInput {
				return paypal.GetPayoutBatchInput{BatchID: request.BatchID, Page: 1}
			},
			Found: sdkgo.GoTo(completePayoutBatchRead{}),
		})),
		dex.DefineStep(completePayoutBatchRead{}),
	}
}

// GetRPCs exposes the payout result in Dex Web.
func (flow *PayoutBatchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:paypal-payout-batch-outcome value-type:json editable:false description:"Payout batch outcome"
// GetDexSummary reads the persisted result for Dex Web.
func (*PayoutBatchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := payoutBatchOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paypal-payout-batch-outcome": outcome}}, nil
}

// dex:field attribute-key:paypal-payout-batch-outcome value-type:json editable:false description:"Payout batch outcome"
// GetDexDisplay reads the persisted result for Dex Web.
func (*PayoutBatchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := payoutBatchOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paypal-payout-batch-outcome": outcome}}, nil
}

// dex:group group-id:payout-input group-label:"Payout input"
// dex:explanation text:"Check the payout batch ID before reading PayPal."
type preparePayoutBatchRequest struct{ dex.StepDefaults }

func (preparePayoutBatchRequest) GetStepType() string { return "PreparePayoutBatchRead" }
func (preparePayoutBatchRequest) WaitFor(dex.Context, PayoutBatchRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (preparePayoutBatchRequest) Execute(_ dex.Context, request PayoutBatchRequest) (*dex.StepDecision, error) {
	if request.BatchID == "" {
		return dex.ForceFail(fmt.Errorf("batchID is required").Error()), nil
	}
	return dex.GoTo(sdkgo.StepRef[PayoutBatchRequest](payoutBatchStepType), request), nil
}

// dex:group group-id:payout-outcome group-label:"Payout outcome"
// dex:explanation text:"Persist the PayPal payout batch result."
type completePayoutBatchRead struct {
	dex.StepDefaultsNoWaitFor[paypal.GetPayoutBatchResult]
}

func (completePayoutBatchRead) GetStepType() string { return "CompletePayoutBatchRead" }
func (completePayoutBatchRead) Execute(ctx dex.Context, result paypal.GetPayoutBatchResult) (*dex.StepDecision, error) {
	outcome := PayoutBatchOutcome{Batch: result.Value}
	if err := payoutBatchOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*PayoutBatchFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*PayoutBatchFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*PayoutBatchFlow)(nil).GetDexDisplay
