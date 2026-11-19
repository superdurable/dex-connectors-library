// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// UpsertOpportunityFlowType is the stable opportunity upsert Flow identity.
const UpsertOpportunityFlowType = "MarketoUpsertOpportunity"
const upsertOpportunityStepType = "CallMarketoUpsertOpportunity"

var opportunityWriteAttribute = dex.DefineAttribute[marketo.OpportunityWriteResult]("marketo-opportunity-write")

// UpsertOpportunityFlow creates or updates one opportunity by external ID and stores its GUID.
type UpsertOpportunityFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewUpsertOpportunityFlow binds the static Marketo connection.
func NewUpsertOpportunityFlow(connection marketo.Connection) *UpsertOpportunityFlow {
	return &UpsertOpportunityFlow{connection: connection}
}

// GetFlowType identifies the upsert example.
func (*UpsertOpportunityFlow) GetFlowType() string { return UpsertOpportunityFlowType }

// GetPersistenceSchema registers the synced opportunity result.
func (*UpsertOpportunityFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{opportunityWriteAttribute}}
}

// GetSteps registers the upsert and completion boundaries.
func (flow *UpsertOpportunityFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpsertOpportunity{}),
		dex.DefineStep(marketo.NewUpsertOpportunityStep(marketo.UpsertOpportunityStepConfig[marketo.UpsertOpportunityInput]{
			StepType: upsertOpportunityStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Create or update an opportunity by external ID."},
			Connection:  flow.connection, MapToOperationInput: mapUpsertOpportunityInput,
			Synced: sdkgo.GoTo(completeUpsertOpportunity{}),
		})),
		dex.DefineStep(completeUpsertOpportunity{}),
	}
}

// GetRPCs exposes the result to Dex Web.
func (flow *UpsertOpportunityFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-opportunity-write value-type:json editable:false description:"Synced Marketo opportunity"
// GetDexSummary returns the synced opportunity ID.
func (*UpsertOpportunityFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readOpportunityWriteResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-opportunity-write": result}}, nil
}

// dex:field attribute-key:marketo-opportunity-write value-type:json editable:false description:"Synced Marketo opportunity"
// GetDexDisplay returns the synced opportunity ID.
func (*UpsertOpportunityFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readOpportunityWriteResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-opportunity-write": result}}, nil
}

func readOpportunityWriteResult(ctx dex.Context) (marketo.OpportunityWriteResult, error) {
	result, err := opportunityWriteAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.OpportunityWriteResult{}, nil
	}
	return result, err
}

func mapUpsertOpportunityInput(input marketo.UpsertOpportunityInput) marketo.UpsertOpportunityInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the external opportunity ID and name to Marketo."
type startUpsertOpportunity struct{ dex.StepDefaults }

func (startUpsertOpportunity) GetStepType() string { return "StartMarketoUpsertOpportunity" }
func (startUpsertOpportunity) WaitFor(dex.Context, marketo.UpsertOpportunityInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpsertOpportunity) Execute(_ dex.Context, input marketo.UpsertOpportunityInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.UpsertOpportunityInput](upsertOpportunityStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the synced opportunity ID and complete the Flow."
type completeUpsertOpportunity struct {
	dex.StepDefaultsNoWaitFor[marketo.UpsertOpportunityResult]
}

func (completeUpsertOpportunity) GetStepType() string { return "CompleteMarketoUpsertOpportunity" }
func (completeUpsertOpportunity) Execute(ctx dex.Context, result marketo.UpsertOpportunityResult) (*dex.StepDecision, error) {
	if err := opportunityWriteAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpsertOpportunityFlow)(nil)
