// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FolderLifecycleFlowType identifies the folder marker example.
	FolderLifecycleFlowType         = "AmazonS3FolderLifecycle"
	folderLifecycleStartStepType    = "StartS3FolderLifecycle"
	folderLifecycleCreateStepType   = "CreateS3Folder"
	folderLifecycleDeleteStepType   = "DeleteS3Folder"
	folderLifecycleCompleteStepType = "CompleteS3FolderLifecycle"
)

var deletedFolderAttribute = dex.DefineAttribute[s3.DeletedFolder]("amazon-s3-deleted-folder")

// FolderLifecycleInput names a temporary folder prefix to create and then delete.
type FolderLifecycleInput struct {
	// Bucket is the bucket; blank uses the connection default.
	Bucket string `json:"bucket,omitempty"`
	// Prefix is a temporary folder path; every current object beneath it is deleted.
	Prefix string `json:"prefix"`
}

// FolderLifecycleFlow creates a folder marker and then removes its prefix.
type FolderLifecycleFlow struct {
	dex.FlowDefaults
	connection s3.Connection
}

// NewFolderLifecycleFlow binds the static S3 example connection.
func NewFolderLifecycleFlow(connection s3.Connection) *FolderLifecycleFlow {
	return &FolderLifecycleFlow{connection: connection}
}

// GetFlowType returns FolderLifecycleFlowType.
func (*FolderLifecycleFlow) GetFlowType() string { return FolderLifecycleFlowType }

// GetSteps returns start, create, delete, and completion Steps.
func (flow *FolderLifecycleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startFolderLifecycle{}),
		dex.DefineStep(s3.NewCreateFolderStep(s3.CreateFolderStepConfig[FolderLifecycleInput]{
			StepType: folderLifecycleCreateStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "s3-folder", GroupLabel: "S3 folder", Explanation: "Create a zero-byte marker for a temporary folder prefix."},
			Connection:  flow.connection,
			MapToOperationInput: func(input FolderLifecycleInput) s3.CreateFolderInput {
				return s3.CreateFolderInput{Bucket: input.Bucket, Prefix: input.Prefix}
			},
			Created: sdkgo.GoTo(sdkgo.StepRef[s3.CreateFolderResult](folderLifecycleDeleteStepType)),
		})),
		dex.DefineStep(s3.NewDeleteFolderStep(s3.DeleteFolderStepConfig[s3.CreateFolderResult]{
			StepType: folderLifecycleDeleteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "s3-folder", GroupLabel: "S3 folder", Explanation: "Delete the temporary marker and any current object beneath its prefix."},
			Connection:  flow.connection,
			MapToOperationInput: func(result s3.CreateFolderResult) s3.DeleteFolderInput {
				return s3.DeleteFolderInput{Bucket: result.Value.Bucket, Prefix: result.Value.Prefix}
			},
			Deleted: sdkgo.GoTo(completeFolderLifecycle{}),
		})),
		dex.DefineStep(completeFolderLifecycle{}),
	}
}

// GetPersistenceSchema registers the deletion result for Dex Web.
func (*FolderLifecycleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deletedFolderAttribute}}
}

// GetRPCs returns the Dex Web summary and display RPCs.
func (flow *FolderLifecycleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the deleted prefix.
//
// dex:field attribute-key:amazon-s3-deleted-folder value-type:json editable:false description:"Deleted temporary folder prefix"
func (*FolderLifecycleFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := deletedFolderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-s3-deleted-folder": value}}, nil
}

// GetDexDisplay returns the deleted prefix.
//
// dex:field attribute-key:amazon-s3-deleted-folder value-type:json editable:false description:"Deleted temporary folder prefix"
func (*FolderLifecycleFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := deletedFolderAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-s3-deleted-folder": value}}, nil
}

// dex:group group-id:s3-folder-start group-label:"S3 folder start"
// dex:explanation text:"Start a temporary folder marker lifecycle."
type startFolderLifecycle struct{ dex.StepDefaults }

func (startFolderLifecycle) GetStepType() string { return folderLifecycleStartStepType }
func (startFolderLifecycle) WaitFor(dex.Context, FolderLifecycleInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startFolderLifecycle) Execute(_ dex.Context, input FolderLifecycleInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[FolderLifecycleInput](folderLifecycleCreateStepType), input), nil
}

// dex:group group-id:s3-folder-complete group-label:"S3 folder complete"
// dex:explanation text:"Store the deletion result and complete the Flow."
type completeFolderLifecycle struct {
	dex.StepDefaultsNoWaitFor[s3.DeleteFolderResult]
}

func (completeFolderLifecycle) GetStepType() string { return folderLifecycleCompleteStepType }
func (completeFolderLifecycle) Execute(ctx dex.Context, result s3.DeleteFolderResult) (*dex.StepDecision, error) {
	if err := deletedFolderAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}
