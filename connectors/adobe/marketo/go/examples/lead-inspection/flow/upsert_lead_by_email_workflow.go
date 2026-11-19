// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// UpsertLeadByEmailFlowType is the stable lead upsert Flow identity.
const UpsertLeadByEmailFlowType = "MarketoUpsertLeadByEmail"
const upsertLeadByEmailStepType = "CallMarketoUpsertLeadByEmail"

var syncedLeadAttribute = dex.DefineAttribute[marketo.SyncedLead]("marketo-synced-lead")

// UpsertLeadByEmailFlow creates or updates one lead by email and stores its returned ID.
type UpsertLeadByEmailFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewUpsertLeadByEmailFlow binds the static Marketo connection.
func NewUpsertLeadByEmailFlow(connection marketo.Connection) *UpsertLeadByEmailFlow {
	return &UpsertLeadByEmailFlow{connection: connection}
}

// GetFlowType identifies the upsert example.
func (*UpsertLeadByEmailFlow) GetFlowType() string { return UpsertLeadByEmailFlowType }

// GetPersistenceSchema registers the synced lead result.
func (*UpsertLeadByEmailFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{syncedLeadAttribute}}
}

// GetSteps registers the upsert and completion boundaries.
func (flow *UpsertLeadByEmailFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpsertLeadByEmail{}),
		dex.DefineStep(marketo.NewUpsertLeadByEmailStep(marketo.UpsertLeadByEmailStepConfig[marketo.UpsertLeadByEmailInput]{
			StepType: upsertLeadByEmailStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Create or update a lead by email."},
			Connection:  flow.connection, MapToOperationInput: mapUpsertLeadByEmailInput,
			Synced: sdkgo.GoTo(completeUpsertLeadByEmail{}),
		})),
		dex.DefineStep(completeUpsertLeadByEmail{}),
	}
}

// GetRPCs exposes the result to Dex Web.
func (flow *UpsertLeadByEmailFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-synced-lead value-type:json editable:false description:"Synced Marketo lead"
// GetDexSummary returns the synced lead ID.
func (*UpsertLeadByEmailFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readSyncedLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-synced-lead": result}}, nil
}

// dex:field attribute-key:marketo-synced-lead value-type:json editable:false description:"Synced Marketo lead"
// GetDexDisplay returns the synced lead ID.
func (*UpsertLeadByEmailFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readSyncedLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-synced-lead": result}}, nil
}

func readSyncedLead(ctx dex.Context) (marketo.SyncedLead, error) {
	result, err := syncedLeadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.SyncedLead{}, nil
	}
	return result, err
}

func mapUpsertLeadByEmailInput(input marketo.UpsertLeadByEmailInput) marketo.UpsertLeadByEmailInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the email and selected names to Marketo."
type startUpsertLeadByEmail struct{ dex.StepDefaults }

func (startUpsertLeadByEmail) GetStepType() string { return "StartMarketoUpsertLeadByEmail" }
func (startUpsertLeadByEmail) WaitFor(dex.Context, marketo.UpsertLeadByEmailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpsertLeadByEmail) Execute(_ dex.Context, input marketo.UpsertLeadByEmailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.UpsertLeadByEmailInput](upsertLeadByEmailStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the synced lead ID and complete the Flow."
type completeUpsertLeadByEmail struct {
	dex.StepDefaultsNoWaitFor[marketo.UpsertLeadByEmailResult]
}

func (completeUpsertLeadByEmail) GetStepType() string { return "CompleteMarketoUpsertLeadByEmail" }
func (completeUpsertLeadByEmail) Execute(ctx dex.Context, result marketo.UpsertLeadByEmailResult) (*dex.StepDecision, error) {
	if err := syncedLeadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpsertLeadByEmailFlow)(nil)
