// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package blobdownload runs Azure Blob Storage getBlob as one Dex Flow.
package blobdownload

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-blob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MicrosoftAzureBlobDownload"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "microsoft-azure-blob"
	operationStepType = "CallGetBlob"
)

var blobDownloadAttribute = dex.DefineAttribute[azureblob.BlobDownload]("azureblob-blob-download")

// Flow runs GetBlob once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection azureblob.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection azureblob.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{blobDownloadAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(azureblob.NewGetBlobStep(azureblob.GetBlobStepConfig[azureblob.GetBlobInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Azure Blob Storage", Explanation: "Download one Azure Storage blob."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Downloaded:          sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:azureblob-blob-download value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBlobDownload(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azureblob-blob-download": outcome}}, nil
}

// dex:field attribute-key:azureblob-blob-download value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBlobDownload(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"azureblob-blob-download": outcome}}, nil
}

func readBlobDownload(ctx dex.Context) (azureblob.BlobDownload, error) {
	outcome, err := blobDownloadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return azureblob.BlobDownload{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input azureblob.GetBlobInput) azureblob.GetBlobInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetBlob" }
func (startRequest) WaitFor(dex.Context, azureblob.GetBlobInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input azureblob.GetBlobInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[azureblob.GetBlobInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[azureblob.GetBlobResult]
}

func (completeResult) GetStepType() string { return "CompleteGetBlob" }
func (completeResult) Execute(ctx dex.Context, result azureblob.GetBlobResult) (*dex.StepDecision, error) {
	if err := blobDownloadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
