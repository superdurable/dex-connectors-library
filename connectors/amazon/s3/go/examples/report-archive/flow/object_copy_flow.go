// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ObjectCopyFlowType identifies the bounded copy and read-back example.
	ObjectCopyFlowType         = "AmazonS3ObjectCopy"
	objectCopyStartStepType    = "StartS3ObjectCopy"
	objectCopyStepType         = "CopyS3Object"
	objectCopyReadStepType     = "ReadCopiedS3Object"
	objectCopyCompleteStepType = "CompleteS3ObjectCopy"
)

var copiedObjectAttribute = dex.DefineAttribute[s3.ObjectBytes]("amazon-s3-copied-object")

// ObjectCopyInput names the source and destination for a bounded object copy.
type ObjectCopyInput struct {
	// SourceBucket is the source bucket; blank uses the connection default.
	SourceBucket string `json:"sourceBucket,omitempty"`
	// SourceKey is the source object key.
	SourceKey string `json:"sourceKey"`
	// DestinationBucket is the destination bucket; blank uses the connection default.
	DestinationBucket string `json:"destinationBucket,omitempty"`
	// DestinationKey is the destination key.
	DestinationKey string `json:"destinationKey"`
	// ContentType is the media type to store on the destination.
	ContentType string `json:"contentType"`
}

// ObjectCopyFlow copies an object and reads its bounded bytes back.
type ObjectCopyFlow struct {
	dex.FlowDefaults
	connection s3.Connection
}

// NewObjectCopyFlow binds the static S3 example connection.
func NewObjectCopyFlow(connection s3.Connection) *ObjectCopyFlow {
	return &ObjectCopyFlow{connection: connection}
}

// GetFlowType returns ObjectCopyFlowType.
func (*ObjectCopyFlow) GetFlowType() string { return ObjectCopyFlowType }

// GetSteps returns the start, copy, read-back, and completion Steps.
func (flow *ObjectCopyFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startObjectCopy{}),
		dex.DefineStep(s3.NewCopyObjectStep(s3.CopyObjectStepConfig[ObjectCopyInput]{
			StepType: objectCopyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "s3-copy", GroupLabel: "S3 copy", Explanation: "Copy the source to a destination with a bounded send count."},
			Connection:  flow.connection,
			MapToOperationInput: func(input ObjectCopyInput) s3.CopyObjectInput {
				return s3.CopyObjectInput{SourceBucket: input.SourceBucket, SourceKey: input.SourceKey, DestinationBucket: input.DestinationBucket, DestinationKey: input.DestinationKey, ContentType: input.ContentType}
			},
			Copied: sdkgo.GoTo(sdkgo.StepRef[s3.CopyObjectResult](objectCopyReadStepType)),
		})),
		dex.DefineStep(s3.NewGetObjectBytesStep(s3.GetObjectBytesStepConfig[s3.CopyObjectResult]{
			StepType: objectCopyReadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "s3-copy", GroupLabel: "S3 copy", Explanation: "Read the copied object's complete bytes within the connection limit."},
			Connection:  flow.connection,
			MapToOperationInput: func(result s3.CopyObjectResult) s3.GetObjectBytesInput {
				return s3.GetObjectBytesInput{Bucket: result.Value.DestinationBucket, Key: result.Value.DestinationKey}
			},
			Read: sdkgo.GoTo(completeObjectCopy{}),
		})),
		dex.DefineStep(completeObjectCopy{}),
	}
}

// GetPersistenceSchema registers the copied object Result for Dex Web.
func (*ObjectCopyFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{copiedObjectAttribute}}
}

// GetRPCs returns the Dex Web summary and display RPCs.
func (flow *ObjectCopyFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the copied object Result.
//
// dex:field attribute-key:amazon-s3-copied-object value-type:json editable:false description:"Copied object metadata and bytes"
func (*ObjectCopyFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := copiedObjectAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-s3-copied-object": value}}, nil
}

// GetDexDisplay returns the copied object Result.
//
// dex:field attribute-key:amazon-s3-copied-object value-type:json editable:false description:"Copied object metadata and bytes"
func (*ObjectCopyFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := copiedObjectAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-s3-copied-object": value}}, nil
}

// dex:group group-id:s3-copy-start group-label:"S3 copy start"
// dex:explanation text:"Start a bounded S3 object copy."
type startObjectCopy struct{ dex.StepDefaults }

func (startObjectCopy) GetStepType() string { return objectCopyStartStepType }
func (startObjectCopy) WaitFor(dex.Context, ObjectCopyInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startObjectCopy) Execute(_ dex.Context, input ObjectCopyInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ObjectCopyInput](objectCopyStepType), input), nil
}

// dex:group group-id:s3-copy-complete group-label:"S3 copy complete"
// dex:explanation text:"Store the copied bytes and complete the Flow."
type completeObjectCopy struct {
	dex.StepDefaultsNoWaitFor[s3.GetObjectBytesResult]
}

func (completeObjectCopy) GetStepType() string { return objectCopyCompleteStepType }
func (completeObjectCopy) Execute(ctx dex.Context, result s3.GetObjectBytesResult) (*dex.StepDecision, error) {
	if err := copiedObjectAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}
