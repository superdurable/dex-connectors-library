// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package upload creates one Cloud Storage object as a Dex Flow.
package upload

import (
	"errors"

	cloudstorage "github.com/superdurable/dex-connectors-library/connectors/google/cloud-storage/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleCloudStorageUpload"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-cloud-storage"
	operationStepType = "CallUploadObject"
)

var uploadedObjectAttribute = dex.DefineAttribute[cloudstorage.Object]("google-uploaded-object")

// Flow creates one object and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection cloudstorage.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection cloudstorage.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{uploadedObjectAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(cloudstorage.NewUploadObjectStep(cloudstorage.UploadObjectStepConfig[cloudstorage.UploadObjectInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Cloud Storage", Explanation: "Upload an object to the selected Google Cloud Storage bucket."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Uploaded:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-uploaded-object value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUploadedObject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-uploaded-object": outcome}}, nil
}

// dex:field attribute-key:google-uploaded-object value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUploadedObject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-uploaded-object": outcome}}, nil
}

func readUploadedObject(ctx dex.Context) (cloudstorage.Object, error) {
	outcome, err := uploadedObjectAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return cloudstorage.Object{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input cloudstorage.UploadObjectInput) cloudstorage.UploadObjectInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartUploadObject" }
func (startRequest) WaitFor(dex.Context, cloudstorage.UploadObjectInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input cloudstorage.UploadObjectInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[cloudstorage.UploadObjectInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[cloudstorage.UploadObjectResult]
}

func (completeResult) GetStepType() string { return "CompleteUploadObject" }
func (completeResult) Execute(ctx dex.Context, result cloudstorage.UploadObjectResult) (*dex.StepDecision, error) {
	if err := uploadedObjectAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
