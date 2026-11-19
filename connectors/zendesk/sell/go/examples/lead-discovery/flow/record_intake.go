// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leaddiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/zendesk/sell/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const recordIntakeStepType = "CreateZendeskSellRecord"

var createdRecordAttribute = dex.DefineAttribute[sell.SellRecord]("zendesk-sell-created-record")

// RecordIntakeFlow creates a new Zendesk Sell record and retains its public identity.
type RecordIntakeFlow struct {
	dex.FlowDefaults
	connection sell.Connection
}

// NewRecordIntakeFlow binds the connection used by the create Step.
func NewRecordIntakeFlow(connection sell.Connection) *RecordIntakeFlow {
	return &RecordIntakeFlow{connection: connection}
}

// GetFlowType preserves this example's durable identity.
func (*RecordIntakeFlow) GetFlowType() string { return "ZendeskSellRecordIntake" }

// GetPersistenceSchema registers the created record identity.
func (*RecordIntakeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdRecordAttribute}}
}

// GetSteps connects the start request, provider create, and completion.
func (flow *RecordIntakeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRecordIntake{}),
		dex.DefineStep(sell.NewCreateRecordStep(sell.CreateRecordStepConfig[sell.CreateRecordInput]{
			StepType: recordIntakeStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Zendesk Sell", Explanation: "Create a Sell contact, deal, task, or note."},
			Connection:          flow.connection,
			MapToOperationInput: mapRecordIntakeInput,
			Created:             sdkgo.GoTo(completeRecordIntake{}),
		})),
		dex.DefineStep(completeRecordIntake{}),
	}
}

// GetRPCs exposes the created record to Dex Web.
func (flow *RecordIntakeFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:zendesk-sell-created-record value-type:json editable:false description:"Created record"
// GetDexSummary returns the created record identity.
func (*RecordIntakeFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	record, err := readCreatedRecord(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zendesk-sell-created-record": record}}, nil
}

// dex:field attribute-key:zendesk-sell-created-record value-type:json editable:false description:"Created record"
// GetDexDisplay returns the created record identity.
func (*RecordIntakeFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	record, err := readCreatedRecord(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zendesk-sell-created-record": record}}, nil
}

func readCreatedRecord(ctx dex.Context) (sell.SellRecord, error) {
	record, err := createdRecordAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sell.SellRecord{}, nil
	}
	return record, err
}

func mapRecordIntakeInput(input sell.CreateRecordInput) sell.CreateRecordInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Send the typed record input to the provider create Step."
type startRecordIntake struct{ dex.StepDefaults }

func (startRecordIntake) GetStepType() string { return "StartZendeskSellRecordIntake" }
func (startRecordIntake) WaitFor(dex.Context, sell.CreateRecordInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRecordIntake) Execute(_ dex.Context, input sell.CreateRecordInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sell.CreateRecordInput](recordIntakeStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Keep the created record identity and complete the Flow."
type completeRecordIntake struct {
	dex.StepDefaultsNoWaitFor[sell.CreateRecordResult]
}

func (completeRecordIntake) GetStepType() string { return "CompleteZendeskSellRecordIntake" }
func (completeRecordIntake) Execute(ctx dex.Context, result sell.CreateRecordResult) (*dex.StepDecision, error) {
	if err := createdRecordAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*RecordIntakeFlow)(nil)
