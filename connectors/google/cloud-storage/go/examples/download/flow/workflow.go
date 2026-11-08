// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package download runs Google Cloud Storage downloadObject as one Dex Flow.
package download

import (
	"errors"

	cloudstorage "github.com/superdurable/dex-connectors-library/connectors/google/cloud-storage/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleCloudStorageDownload"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-cloud-storage"
	operationStepType = "CallDownloadObject"
)

var downloadAttribute = dex.DefineAttribute[cloudstorage.DownloadedObject]("google-cloud-storage-download")

// Flow runs DownloadObject once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection cloudstorage.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection cloudstorage.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{downloadAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(cloudstorage.NewDownloadObjectStep(cloudstorage.DownloadObjectStepConfig[cloudstorage.DownloadObjectInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Cloud Storage", Explanation: "Download the selected object from Google Cloud Storage."},
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

// dex:field attribute-key:google-cloud-storage-download value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDownloadedObject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-cloud-storage-download": outcome}}, nil
}

// dex:field attribute-key:google-cloud-storage-download value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDownloadedObject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-cloud-storage-download": outcome}}, nil
}

func readDownloadedObject(ctx dex.Context) (cloudstorage.DownloadedObject, error) {
	outcome, err := downloadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return cloudstorage.DownloadedObject{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input cloudstorage.DownloadObjectInput) cloudstorage.DownloadObjectInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartDownloadObject" }
func (startRequest) WaitFor(dex.Context, cloudstorage.DownloadObjectInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input cloudstorage.DownloadObjectInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[cloudstorage.DownloadObjectInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[cloudstorage.DownloadObjectResult]
}

func (completeResult) GetStepType() string { return "CompleteDownloadObject" }
func (completeResult) Execute(ctx dex.Context, result cloudstorage.DownloadObjectResult) (*dex.StepDecision, error) {
	if err := downloadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
