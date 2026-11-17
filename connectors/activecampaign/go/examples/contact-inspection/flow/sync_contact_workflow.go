// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const syncContactStepType = "CallSyncContact"

var syncedContactAttribute = dex.DefineAttribute[activecampaign.ContactMetadata]("activecampaign-synced-contact")

// SyncContactFlow demonstrates a repeat-safe contact upsert through a Dex Step.
type SyncContactFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewSyncContactFlow binds the same static connection as the inspection Flow.
func NewSyncContactFlow(connection activecampaign.Connection) *SyncContactFlow {
	return &SyncContactFlow{connection: connection}
}

// GetFlowType is the stable upsert example identity.
func (*SyncContactFlow) GetFlowType() string { return "ActiveCampaignSyncContact" }

// GetPersistenceSchema declares that this example needs no durable fields.
func (*SyncContactFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{syncedContactAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *SyncContactFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSyncContact{}),
		dex.DefineStep(activecampaign.NewSyncContactStep(activecampaign.SyncContactStepConfig[activecampaign.SyncContactInput]{
			StepType: syncContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Upsert one contact by email."},
			Connection:  flow.connection, MapToOperationInput: MapToSyncContactInput,
			Synced: sdkgo.GoTo(completeSyncContact{}),
		})),
		dex.DefineStep(completeSyncContact{}),
	}
}

// GetRPCs exposes the stored contact in Dex Web.
func (flow *SyncContactFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-synced-contact value-type:json editable:false description:"Synced contact"
// GetDexSummary shows the synced contact in Dex Web.
func (*SyncContactFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readSyncedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-synced-contact": contact}}, nil
}

// dex:field attribute-key:activecampaign-synced-contact value-type:json editable:false description:"Synced contact"
// GetDexDisplay shows the synced contact in Dex Web.
func (*SyncContactFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readSyncedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-synced-contact": contact}}, nil
}

func readSyncedContact(ctx dex.Context) (activecampaign.ContactMetadata, error) {
	contact, err := syncedContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.ContactMetadata{}, nil
	}
	return contact, err
}

// MapToSyncContactInput preserves the typed start input for the provider call.
func MapToSyncContactInput(input activecampaign.SyncContactInput) activecampaign.SyncContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the contact fields to the upsert Step."
type startSyncContact struct{ dex.StepDefaults }

func (startSyncContact) GetStepType() string { return "StartSyncContact" }
func (startSyncContact) WaitFor(dex.Context, activecampaign.SyncContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSyncContact) Execute(_ dex.Context, input activecampaign.SyncContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.SyncContactInput](syncContactStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the contact ID and visible names."
type completeSyncContact struct {
	dex.StepDefaultsNoWaitFor[activecampaign.SyncContactResult]
}

func (completeSyncContact) GetStepType() string { return "CompleteSyncContact" }
func (completeSyncContact) Execute(ctx dex.Context, result activecampaign.SyncContactResult) (*dex.StepDecision, error) {
	if err := syncedContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SyncContactFlow)(nil)
