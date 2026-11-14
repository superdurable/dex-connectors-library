// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package bucketlist runs Qiniu listBuckets as one Dex Flow.
package bucketlist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/qiniu/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "QiniuBucketList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "qiniu"
	operationStepType = "CallListBuckets"
)

var bucketListOutcomeAttribute = dex.DefineAttribute[qiniu.ListBucketsOutput]("qiniu-bucket-list-outcome")

// Flow runs ListBuckets once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection qiniu.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection qiniu.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{bucketListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(qiniu.NewListBucketsStep(qiniu.ListBucketsStepConfig[qiniu.ListBucketsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Qiniu", Explanation: "Call the provider's listBuckets operation."},
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

// dex:field attribute-key:qiniu-bucket-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBucketListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qiniu-bucket-list-outcome": outcome}}, nil
}

// dex:field attribute-key:qiniu-bucket-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBucketListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"qiniu-bucket-list-outcome": outcome}}, nil
}

func readBucketListOutcome(ctx dex.Context) (qiniu.ListBucketsOutput, error) {
	outcome, err := bucketListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return qiniu.ListBucketsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input qiniu.ListBucketsInput) qiniu.ListBucketsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListBuckets" }
func (startRequest) WaitFor(dex.Context, qiniu.ListBucketsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input qiniu.ListBucketsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[qiniu.ListBucketsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[qiniu.ListBucketsResult]
}

func (completeResult) GetStepType() string { return "CompleteListBuckets" }
func (completeResult) Execute(ctx dex.Context, result qiniu.ListBucketsResult) (*dex.StepDecision, error) {
	if err := bucketListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
