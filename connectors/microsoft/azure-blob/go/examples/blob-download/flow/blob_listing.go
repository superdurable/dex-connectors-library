// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package blobdownload

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-blob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listBlobStepType = "CallListBlobs"

var blobListingAttribute = dex.DefineAttribute[azureblob.BlobsPage]("azureblob-blobs-page")

// BlobListingFlow lists one page of blob names in a container.
type BlobListingFlow struct {
	dex.FlowDefaults
	connection azureblob.Connection
}

// NewBlobListingFlow binds the existing named Azure connection.
func NewBlobListingFlow(connection azureblob.Connection) *BlobListingFlow {
	return &BlobListingFlow{connection: connection}
}

// GetPersistenceSchema registers the page stored by this Flow.
func (*BlobListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{blobListingAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *BlobListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startBlobListing{}),
		dex.DefineStep(azureblob.NewListBlobsStep(azureblob.ListBlobsStepConfig[azureblob.ListBlobsInput]{
			StepType: listBlobStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Azure Blob Storage", Explanation: "List one page of blob names."},
			Connection:          flow.connection,
			MapToOperationInput: func(input azureblob.ListBlobsInput) azureblob.ListBlobsInput { return input },
			Listed:              sdkgo.GoTo(completeBlobListing{}),
		})),
		dex.DefineStep(completeBlobListing{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *BlobListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:azureblob-blobs-page value-type:json editable:false description:"Blob listing"
// GetDexSummary shows the listed names in Dex Web.
func (*BlobListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readBlobListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azureblob-blobs-page": page}}, nil
}

// dex:field attribute-key:azureblob-blobs-page value-type:json editable:false description:"Blob listing"
// GetDexDisplay shows the listed names in Dex Web.
func (*BlobListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readBlobListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azureblob-blobs-page": page}}, nil
}

func readBlobListing(ctx dex.Context) (azureblob.BlobsPage, error) {
	page, err := blobListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return azureblob.BlobsPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the container and cursor to the provider Step."
type startBlobListing struct{ dex.StepDefaults }

func (startBlobListing) WaitFor(dex.Context, azureblob.ListBlobsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startBlobListing) Execute(_ dex.Context, input azureblob.ListBlobsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[azureblob.ListBlobsInput](listBlobStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the blob listing and complete the Flow."
type completeBlobListing struct {
	dex.StepDefaultsNoWaitFor[azureblob.ListBlobsResult]
}

func (completeBlobListing) Execute(ctx dex.Context, result azureblob.ListBlobsResult) (*dex.StepDecision, error) {
	if err := blobListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*BlobListingFlow)(nil)
