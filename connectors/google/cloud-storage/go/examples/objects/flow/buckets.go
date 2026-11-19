// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package objects

import (
	"errors"

	cloudstorage "github.com/superdurable/dex-connectors-library/connectors/google/cloud-storage/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// BucketFlowType is the stable bucket listing example identity.
	BucketFlowType     = "GoogleCloudStorageBuckets"
	bucketListStepType = "CallListBuckets"
)

var bucketsAttribute = dex.DefineAttribute[cloudstorage.BucketsPage]("google-cloud-storage-buckets")

// BucketFlow lists project buckets through the static Cloud Storage connection.
type BucketFlow struct {
	dex.FlowDefaults
	connection cloudstorage.Connection
}

// NewBucketFlow binds the provider connection before registration.
func NewBucketFlow(connection cloudstorage.Connection) *BucketFlow {
	return &BucketFlow{connection: connection}
}

func (*BucketFlow) GetFlowType() string { return BucketFlowType }

func (*BucketFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{bucketsAttribute}}
}

func (flow *BucketFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startBucketList{}),
		dex.DefineStep(cloudstorage.NewListBucketsStep(cloudstorage.ListBucketsStepConfig[cloudstorage.ListBucketsInput]{
			StepType: bucketListStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Cloud Storage", Explanation: "List buckets in the selected Google Cloud project."},
			Connection:          flow.connection,
			MapToOperationInput: mapBucketListInput,
			Listed:              sdkgo.GoTo(completeBucketList{}),
		})),
		dex.DefineStep(completeBucketList{}),
	}
}

// GetRPCs exposes read-only bucket summary and display views.
func (flow *BucketFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-cloud-storage-buckets value-type:json editable:false description:"Bucket page"
// GetDexSummary shows the bucket page in Dex Web.
func (*BucketFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readBuckets(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-cloud-storage-buckets": page}}, nil
}

// dex:field attribute-key:google-cloud-storage-buckets value-type:json editable:false description:"Bucket page"
// GetDexDisplay shows the bucket page in Dex Web.
func (*BucketFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readBuckets(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-cloud-storage-buckets": page}}, nil
}

func readBuckets(ctx dex.Context) (cloudstorage.BucketsPage, error) {
	page, err := bucketsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return cloudstorage.BucketsPage{}, nil
	}
	return page, err
}

func mapBucketListInput(input cloudstorage.ListBucketsInput) cloudstorage.ListBucketsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the project and page selection to Cloud Storage."
type startBucketList struct{ dex.StepDefaults }

func (startBucketList) GetStepType() string { return "StartListBuckets" }
func (startBucketList) WaitFor(dex.Context, cloudstorage.ListBucketsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startBucketList) Execute(_ dex.Context, input cloudstorage.ListBucketsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[cloudstorage.ListBucketsInput](bucketListStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store and return the bucket page."
type completeBucketList struct {
	dex.StepDefaultsNoWaitFor[cloudstorage.ListBucketsResult]
}

func (completeBucketList) GetStepType() string { return "CompleteListBuckets" }
func (completeBucketList) Execute(ctx dex.Context, result cloudstorage.ListBucketsResult) (*dex.StepDecision, error) {
	if err := bucketsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*BucketFlow)(nil)
