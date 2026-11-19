// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// RecordLifecycleFlowType is the Flow that creates then archives one CRM record.
	RecordLifecycleFlowType        = "HubSpotRecordLifecycle"
	startRecordLifecycleStepType   = "StartHubSpotRecordLifecycle"
	createLifecycleRecordStepType  = "CreateHubSpotLifecycleRecord"
	recordCreatedLifecycleStepType = "RecordCreatedHubSpotLifecycleRecord"
	deleteLifecycleRecordStepType  = "DeleteHubSpotLifecycleRecord"
	finishRecordLifecycleStepType  = "FinishHubSpotRecordLifecycle"
)

var recordLifecycleTitleAttribute = dex.DefineAttribute[string]("title")

// RecordLifecycleFlow demonstrates create and archive with the same connection.
type RecordLifecycleFlow struct {
	dex.FlowDefaults
	connection hubspot.Connection
}

// NewRecordLifecycleFlow binds the connection for the create and archive Steps.
func NewRecordLifecycleFlow(connection hubspot.Connection) *RecordLifecycleFlow {
	return &RecordLifecycleFlow{connection: connection}
}

// GetFlowType returns the stable lifecycle Flow identity.
func (*RecordLifecycleFlow) GetFlowType() string { return RecordLifecycleFlowType }

// GetSteps returns the lifecycle Start, connector, and completion Steps.
func (flow *RecordLifecycleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRecordLifecycle{}),
		dex.DefineStep(hubspot.NewCreateObjectStep(hubspot.CreateObjectStepConfig[hubspot.CreateObjectInput]{
			StepType: createLifecycleRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "hubspot", GroupLabel: "HubSpot", Explanation: "Create the requested CRM record."},
			Connection:  flow.connection, MapToOperationInput: func(input hubspot.CreateObjectInput) hubspot.CreateObjectInput { return input },
			Created: sdkgo.GoTo(recordCreatedLifecycle{}),
		})),
		dex.DefineStep(recordCreatedLifecycle{}),
		dex.DefineStep(hubspot.NewDeleteObjectStep(hubspot.DeleteObjectStepConfig[hubspot.DeleteObjectInput]{
			StepType: deleteLifecycleRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "hubspot", GroupLabel: "HubSpot", Explanation: "Archive the newly created CRM record."},
			Connection:  flow.connection, MapToOperationInput: func(input hubspot.DeleteObjectInput) hubspot.DeleteObjectInput { return input },
			Deleted: sdkgo.GoTo(finishRecordLifecycle{}),
		})),
		dex.DefineStep(finishRecordLifecycle{}),
	}
}

// GetRPCs returns the summary and display RPCs for Dex Web.
func (flow *RecordLifecycleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema has no durable Attributes or Streams.
func (*RecordLifecycleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{recordLifecycleTitleAttribute}}
}

// GetDexSummary describes a record lifecycle run.
// dex:field attribute-key:title value-type:string editable:false description:"HubSpot CRM record lifecycle"
func (*RecordLifecycleFlow) GetDexSummary(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"title": "HubSpot CRM record lifecycle"}}, nil
}

// GetDexDisplay describes the Flow in Dex Web.
// dex:field attribute-key:title value-type:string editable:false description:"Create and archive a HubSpot CRM record"
func (*RecordLifecycleFlow) GetDexDisplay(dex.Context, dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"title": "Create and archive a HubSpot CRM record"}}, nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Validate the requested record type and start creation."
type startRecordLifecycle struct{ dex.StepDefaults }

func (startRecordLifecycle) GetStepType() string { return startRecordLifecycleStepType }
func (startRecordLifecycle) WaitFor(dex.Context, hubspot.CreateObjectInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRecordLifecycle) Execute(ctx dex.Context, input hubspot.CreateObjectInput) (*dex.StepDecision, error) {
	if err := recordLifecycleTitleAttribute.Set(ctx, "HubSpot CRM record lifecycle"); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[hubspot.CreateObjectInput](createLifecycleRecordStepType), input), nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Pass the created record ID to the archive Step."
type recordCreatedLifecycle struct {
	dex.StepDefaultsNoWaitFor[hubspot.CreateObjectResult]
}

func (recordCreatedLifecycle) GetStepType() string { return recordCreatedLifecycleStepType }
func (recordCreatedLifecycle) Execute(_ dex.Context, result hubspot.CreateObjectResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[hubspot.DeleteObjectInput](deleteLifecycleRecordStepType), hubspot.DeleteObjectInput{ObjectType: result.Value.ObjectType, ObjectID: result.Value.ID}), nil
}

// dex:group group-id:hubspot group-label:"HubSpot"
// dex:explanation text:"Complete after HubSpot confirms the record is archived."
type finishRecordLifecycle struct {
	dex.StepDefaultsNoWaitFor[hubspot.DeleteObjectResult]
}

func (finishRecordLifecycle) GetStepType() string { return finishRecordLifecycleStepType }
func (finishRecordLifecycle) Execute(_ dex.Context, result hubspot.DeleteObjectResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*RecordLifecycleFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*RecordLifecycleFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*RecordLifecycleFlow)(nil).GetDexDisplay
