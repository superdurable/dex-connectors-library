// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// BucketInventoryFlowType is the stable Dex Web identity of the bucket listing example.
	BucketInventoryFlowType         = "AmazonS3BucketInventory"
	bucketInventoryStartStepType    = "StartS3BucketInventory"
	bucketInventoryListStepType     = "ListS3Buckets"
	bucketInventoryCompleteStepType = "CompleteS3BucketInventory"
)

var bucketInventoryAttribute = dex.DefineAttribute[s3.ListBucketsOutput]("amazon-s3-bucket-inventory")

// BucketInventoryInput requests one page of account-owned buckets.
type BucketInventoryInput struct {
	// MaxBuckets is the page size from 1 to 10000; zero uses 100.
	MaxBuckets int `json:"maxBuckets,omitempty"`
}

// BucketInventoryFlow lists account-owned buckets with the existing S3 connection.
type BucketInventoryFlow struct {
	dex.FlowDefaults
	connection s3.Connection
}

// NewBucketInventoryFlow binds the static example connection to the listing Flow.
func NewBucketInventoryFlow(connection s3.Connection) *BucketInventoryFlow {
	return &BucketInventoryFlow{connection: connection}
}

// GetFlowType returns BucketInventoryFlowType.
func (*BucketInventoryFlow) GetFlowType() string { return BucketInventoryFlowType }

// GetSteps returns the start, S3 list, and completion Steps.
func (flow *BucketInventoryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startBucketInventory{}),
		dex.DefineStep(s3.NewListBucketsStep(s3.ListBucketsStepConfig[BucketInventoryInput]{
			StepType: bucketInventoryListStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "amazon-s3", GroupLabel: "Amazon S3", Explanation: "List one page of buckets owned by the signing account."},
			Connection:  flow.connection,
			MapToOperationInput: func(input BucketInventoryInput) s3.ListBucketsInput {
				return s3.ListBucketsInput{MaxBuckets: input.MaxBuckets}
			},
			Listed: sdkgo.GoTo(completeBucketInventory{}),
		})),
		dex.DefineStep(completeBucketInventory{}),
	}
}

// GetPersistenceSchema registers the inventory displayed by Dex Web.
func (*BucketInventoryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{bucketInventoryAttribute}}
}

// GetRPCs returns the summary and display RPCs.
func (flow *BucketInventoryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the recorded bucket page.
//
// dex:field attribute-key:amazon-s3-bucket-inventory value-type:json editable:false description:"The page of S3 buckets"
func (*BucketInventoryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := bucketInventoryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-s3-bucket-inventory": page}}, nil
}

// GetDexDisplay returns the recorded bucket page.
//
// dex:field attribute-key:amazon-s3-bucket-inventory value-type:json editable:false description:"The page of S3 buckets"
func (*BucketInventoryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := bucketInventoryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-s3-bucket-inventory": page}}, nil
}

// dex:group group-id:bucket-inventory-start group-label:"Bucket inventory start"
// dex:explanation text:"Start a bounded S3 bucket listing."
type startBucketInventory struct{ dex.StepDefaults }

func (startBucketInventory) GetStepType() string { return bucketInventoryStartStepType }
func (startBucketInventory) WaitFor(dex.Context, BucketInventoryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startBucketInventory) Execute(_ dex.Context, input BucketInventoryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[BucketInventoryInput](bucketInventoryListStepType), input), nil
}

// dex:group group-id:bucket-inventory-complete group-label:"Bucket inventory complete"
// dex:explanation text:"Store the listed bucket page and complete the Flow."
type completeBucketInventory struct {
	dex.StepDefaultsNoWaitFor[s3.ListBucketsResult]
}

func (completeBucketInventory) GetStepType() string { return bucketInventoryCompleteStepType }
func (completeBucketInventory) Execute(ctx dex.Context, result s3.ListBucketsResult) (*dex.StepDecision, error) {
	if err := bucketInventoryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}
