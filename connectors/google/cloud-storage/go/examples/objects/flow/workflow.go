// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package objects runs Google Cloud Storage listObjects as one Dex Flow.
package objects

import (
	"errors"

	cloudstorage "github.com/superdurable/dex-connectors-library/connectors/google/cloud-storage/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleCloudStorageList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-cloud-storage"
	operationStepType = "CallListObjects"
)

var objectsAttribute = dex.DefineAttribute[cloudstorage.ObjectsPage]("google-cloud-storage-page")

// Flow runs ListObjects once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection cloudstorage.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection cloudstorage.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{objectsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(cloudstorage.NewListObjectsStep(cloudstorage.ListObjectsStepConfig[cloudstorage.ListObjectsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Cloud Storage", Explanation: "List objects available to the connected Google user."},
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

// dex:field attribute-key:google-cloud-storage-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readObjects(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-cloud-storage-page": outcome}}, nil
}

// dex:field attribute-key:google-cloud-storage-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readObjects(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-cloud-storage-page": outcome}}, nil
}

func readObjects(ctx dex.Context) (cloudstorage.ObjectsPage, error) {
	outcome, err := objectsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return cloudstorage.ObjectsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input cloudstorage.ListObjectsInput) cloudstorage.ListObjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListObjects" }
func (startRequest) WaitFor(dex.Context, cloudstorage.ListObjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input cloudstorage.ListObjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[cloudstorage.ListObjectsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[cloudstorage.ListObjectsResult]
}

func (completeResult) GetStepType() string { return "CompleteListObjects" }
func (completeResult) Execute(ctx dex.Context, result cloudstorage.ListObjectsResult) (*dex.StepDecision, error) {
	if err := objectsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
