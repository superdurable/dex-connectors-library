// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package refunddecision

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/airtable/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	createLifecycleRecordStepType = "CreateAirtableLifecycleRecord"
	deleteLifecycleRecordStepType = "DeleteAirtableLifecycleRecord"
)

var lifecycleRecordAttribute = dex.DefineAttribute[airtable.DeletedRecord]("airtable-lifecycle-record")

// RecordLifecycleInput supplies fields for one temporary record in the picked policy table.
type RecordLifecycleInput struct {
	// Fields maps Airtable field names or IDs to typed cell values.
	Fields map[string]airtable.CellValue `json:"fields"`
	// Typecast permits Airtable to convert text values where possible.
	Typecast bool `json:"typecast,omitempty"`
}

// RecordLifecycleFlow appends then removes one temporary record in the picked policy table.
type RecordLifecycleFlow struct {
	dex.FlowDefaults
	connection airtable.Connection
	settings   Settings
}

// NewRecordLifecycleFlow binds the existing static connection and policy table selection.
func NewRecordLifecycleFlow(connection airtable.Connection, settings *Settings) (*RecordLifecycleFlow, error) {
	if settings == nil || !settings.PolicyTable.isPicked() {
		return nil, errors.New("Airtable record lifecycle needs a picked policy table")
	}
	return &RecordLifecycleFlow{connection: connection, settings: *settings}, nil
}

// GetFlowType identifies the record lifecycle example.
func (*RecordLifecycleFlow) GetFlowType() string { return "AirtableRecordLifecycle" }

// GetPersistenceSchema registers the deletion confirmation.
func (*RecordLifecycleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{lifecycleRecordAttribute}}
}

// GetSteps connects create and delete through separate durable boundaries.
func (flow *RecordLifecycleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRecordLifecycle{}),
		dex.DefineStep(airtable.NewCreateRecordStep(airtable.CreateRecordStepConfig[RecordLifecycleInput]{
			StepType: createLifecycleRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "records", GroupLabel: "Airtable", Explanation: "Append a temporary record."},
			Connection:  flow.connection, MapToOperationInput: func(input RecordLifecycleInput) airtable.CreateRecordInput {
				return airtable.CreateRecordInput{BaseID: flow.settings.PolicyTable.BaseID, TableIDOrName: flow.settings.PolicyTable.TableID, Fields: input.Fields, Typecast: input.Typecast}
			},
			Created: sdkgo.GoTo(mapCreatedRecordForDeletion{settings: flow.settings}),
		})),
		dex.DefineStep(mapCreatedRecordForDeletion{settings: flow.settings}),
		dex.DefineStep(airtable.NewDeleteRecordStep(airtable.DeleteRecordStepConfig[airtable.DeleteRecordInput]{
			StepType: deleteLifecycleRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "records", GroupLabel: "Airtable", Explanation: "Remove the temporary record by ID."},
			Connection:  flow.connection, MapToOperationInput: func(input airtable.DeleteRecordInput) airtable.DeleteRecordInput { return input },
			Deleted: sdkgo.GoTo(completeRecordLifecycle{}),
		})),
		dex.DefineStep(completeRecordLifecycle{}),
	}
}

// GetRPCs exposes the last deletion confirmation to Dex Web.
func (flow *RecordLifecycleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:airtable-lifecycle-record value-type:json editable:false description:"Deleted temporary record"
// GetDexSummary returns the last deletion confirmation.
func (*RecordLifecycleFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readLifecycleRecord(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"airtable-lifecycle-record": result}}, nil
}

// dex:field attribute-key:airtable-lifecycle-record value-type:json editable:false description:"Deleted temporary record"
// GetDexDisplay returns the last deletion confirmation.
func (*RecordLifecycleFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readLifecycleRecord(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"airtable-lifecycle-record": result}}, nil
}

func readLifecycleRecord(ctx dex.Context) (airtable.DeletedRecord, error) {
	result, err := lifecycleRecordAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return airtable.DeletedRecord{}, nil
	}
	return result, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start one temporary record lifecycle."
type startRecordLifecycle struct{ dex.StepDefaults }

func (startRecordLifecycle) GetStepType() string { return "StartAirtableRecordLifecycle" }
func (startRecordLifecycle) WaitFor(dex.Context, RecordLifecycleInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRecordLifecycle) Execute(_ dex.Context, input RecordLifecycleInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[RecordLifecycleInput](createLifecycleRecordStepType), input), nil
}

// dex:group group-id:records group-label:"Airtable"
// dex:explanation text:"Use the new ID to request deletion."
type mapCreatedRecordForDeletion struct {
	dex.StepDefaultsNoWaitFor[airtable.CreateRecordResult]
	settings Settings
}

func (mapCreatedRecordForDeletion) GetStepType() string { return "MapCreatedAirtableRecord" }
func (step mapCreatedRecordForDeletion) Execute(_ dex.Context, result airtable.CreateRecordResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[airtable.DeleteRecordInput](deleteLifecycleRecordStepType), airtable.DeleteRecordInput{
		BaseID: step.settings.PolicyTable.BaseID, TableIDOrName: step.settings.PolicyTable.TableID, RecordID: result.Value.ID,
	}), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Confirm that the temporary record was deleted."
type completeRecordLifecycle struct {
	dex.StepDefaultsNoWaitFor[airtable.DeleteRecordResult]
}

func (completeRecordLifecycle) GetStepType() string { return "CompleteAirtableRecordLifecycle" }
func (completeRecordLifecycle) Execute(ctx dex.Context, result airtable.DeleteRecordResult) (*dex.StepDecision, error) {
	if err := lifecycleRecordAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*RecordLifecycleFlow)(nil)
