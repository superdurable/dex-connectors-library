// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package rowchanged demonstrates the Notion databaseRowChanged Trigger: every row created or edited in
// the picked data source starts one Flow, which reads the row back and records its current properties.
package rowchanged

import (
	"errors"
	"strings"

	notion "github.com/superdurable/dex-connectors-library/connectors/notion/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each row change starts.
	FlowType = "NotionDatabaseRowChange"
	// ConnectionName is the static Dex Web connection for Notion.
	ConnectionName = "notion-row-watcher"
	// DatabaseRowChangedTriggerBinding is the stable databaseRowChanged binding that starts this Flow.
	DatabaseRowChangedTriggerBinding = "database-row-changes"

	recordRowChangeStepType   = "RecordRowChange"
	readChangedRowStepType    = "ReadChangedRow"
	completeRowChangeStepType = "CompleteRowChange"
)

var (
	rowChangeAttribute        = dex.DefineAttribute[Input]("notion-row-change")
	rowChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("notion-row-change-outcome")
)

// Input is one databaseRowChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change notion.DatabaseRowChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// DataSourceID is the watched data source.
	DataSourceID string `json:"dataSourceId"`
	// RowID is the changed row's page ID.
	RowID string `json:"rowId"`
	// ChangeKind is the delivered change kind.
	ChangeKind notion.DatabaseRowChangeKind `json:"changeKind"`
	// Current is the row read back from Notion.
	Current notion.Page `json:"current"`
}

// Flow records one Notion database row change.
type Flow struct {
	dex.FlowDefaults
	connection notion.Connection
}

// NewFlow binds the Notion Connection at registration time.
func NewFlow(connection notion.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Notion connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRowChange{}),
		dex.DefineStep(notion.NewGetPageStep(notion.GetPageStepConfig[Input]{
			StepType: readChangedRowStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed row back, so the record holds its current properties rather than the polled snapshot.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetPageInput,
			Found: sdkgo.GoTo(completeRowChange{}),
		})),
		dex.DefineStep(completeRowChange{}),
	}
}

// GetConnectorTriggerBindings declares the databaseRowChanged binding and its data source picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		notion.DefineDatabaseRowChangedTriggerBinding(notion.DatabaseRowChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: DatabaseRowChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedDataSource", UnitID: notion.UIUnitDataSourcePicker, Label: "Watched database",
				Description: "Choose the Notion data source whose created and edited rows start this Flow: paste the database link from its ••• menu > Copy link at https://app.notion.com and pick a data source, or paste a data source ID from the database's settings > Manage data sources > Copy data source ID. Share the database with this connection first. The picker saves the data source ID and name; the binding needs one, so the Worker does not start until it is saved. The first start records existing rows as a baseline and starts no Flow for them. Restart the Worker after saving; a changed data source records a new baseline.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: notion.UIDataSourcePickerPortDataSourceID, JSONPointer: "/dataSourceId"},
					{Port: notion.UIDataSourcePickerPortDataSourceName, JSONPointer: "/dataSourceName"},
				},
			}}},
		}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the change and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{rowChangeAttribute, rowChangeOutcomeAttribute}}
}

// GetDexSummary returns the change kind and outcome.
//
// dex:field attribute-key:notion-row-change value-type:json editable:false description:"Delivered row change"
// dex:field attribute-key:notion-row-change-outcome value-type:json editable:false description:"Recorded row"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := rowChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"notion-row-change":         change,
		"notion-row-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the row read back from Notion.
//
// dex:field attribute-key:notion-row-change value-type:json editable:false description:"Data source, change kind, and row from the poll"
// dex:field attribute-key:notion-row-change-outcome value-type:json editable:false description:"Current row read back from Notion"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := rowChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"notion-row-change":         change,
		"notion-row-change-outcome": outcome,
	}}, nil
}

func rowChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, rowChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, rowChangeOutcomeAttribute)
	return change, outcome, err
}

// optionalAttribute returns the zero value while a running Flow has not set the Attribute yet.
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// ResolveFlowID names one Flow per row version; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent]) string {
	return "notion-row-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryRowChange admits every change; an application narrows it, for example to created rows.
func AcceptEveryRowChange(sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent]) bool { return true }

// MapToGetPageInput reads the changed row's properties without its body blocks.
func MapToGetPageInput(input Input) notion.GetPageInput {
	return notion.GetPageInput{PageID: input.Change.Row.ID, ShouldSkipContent: true}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered row change, then read the row back from Notion."
type recordRowChange struct {
	dex.StepDefaults
}

func (recordRowChange) GetStepType() string { return recordRowChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordRowChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordRowChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := rowChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedRowStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the row read back from Notion as the outcome and complete the Flow."
type completeRowChange struct {
	dex.StepDefaultsNoWaitFor[notion.GetPageResult]
}

func (completeRowChange) GetStepType() string { return completeRowChangeStepType }

func (completeRowChange) Execute(ctx dex.Context, result notion.GetPageResult) (*dex.StepDecision, error) {
	change, err := rowChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{
		DataSourceID: change.Change.DataSourceID, RowID: change.Change.Row.ID, ChangeKind: change.Change.ChangeKind, Current: result.Value.Page,
	}
	if err := rowChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.DataSourceID) == "" || strings.TrimSpace(input.Change.Row.ID) == "":
		return errors.New("change.dataSourceId and change.row.id are required")
	}
	switch input.Change.ChangeKind {
	case notion.DatabaseRowCreated, notion.DatabaseRowUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
