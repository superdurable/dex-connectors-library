// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package incidentreview

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/pagerduty/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// IncidentUpdateFlowType identifies the status update example.
const IncidentUpdateFlowType = "PagerDutyIncidentUpdate"

const incidentUpdateStepType = "UpdateIncident"

var updatedIncidentAttribute = dex.DefineAttribute[pagerduty.Incident]("pagerduty-updated-incident")

// IncidentUpdateRequest supplies a status and PagerDuty actor.
type IncidentUpdateRequest pagerduty.UpdateIncidentInput

// IncidentUpdateFlow changes an incident's absolute status.
type IncidentUpdateFlow struct {
	dex.FlowDefaults
	connection pagerduty.Connection
}

// NewIncidentUpdateFlow binds a PagerDuty connection to the example.
func NewIncidentUpdateFlow(connection pagerduty.Connection) *IncidentUpdateFlow {
	return &IncidentUpdateFlow{connection: connection}
}

// GetFlowType preserves the status update Flow identity.
func (*IncidentUpdateFlow) GetFlowType() string { return IncidentUpdateFlowType }

// GetSteps registers the generated update operation and completion.
func (flow *IncidentUpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareIncidentUpdate{}),
		dex.DefineStep(pagerduty.NewUpdateIncidentStep(pagerduty.UpdateIncidentStepConfig[IncidentUpdateRequest]{
			StepType: incidentUpdateStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "pagerduty-update", GroupLabel: "PagerDuty incident", Explanation: "Set the incident to the selected status."},
			MapToOperationInput: func(request IncidentUpdateRequest) pagerduty.UpdateIncidentInput {
				return pagerduty.UpdateIncidentInput(request)
			},
			Updated: sdkgo.GoTo(completeIncidentUpdate{}),
		})),
		dex.DefineStep(completeIncidentUpdate{}),
	}
}

// GetRPCs registers summary and display views.
func (flow *IncidentUpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the updated incident Attribute.
func (*IncidentUpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedIncidentAttribute}}
}

// GetDexSummary returns the incident status after the provider update.
// dex:field attribute-key:pagerduty-updated-incident value-type:json editable:false description:"Updated incident"
func (*IncidentUpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	incident, err := readUpdatedIncident(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pagerduty-updated-incident": incident}}, nil
}

// GetDexDisplay returns the incident status after the provider update.
// dex:field attribute-key:pagerduty-updated-incident value-type:json editable:false description:"Updated incident"
func (*IncidentUpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	incident, err := readUpdatedIncident(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"pagerduty-updated-incident": incident}}, nil
}

func readUpdatedIncident(ctx dex.Context) (pagerduty.Incident, error) {
	incident, err := updatedIncidentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pagerduty.Incident{}, nil
	}
	return incident, err
}

// dex:group group-id:pagerduty-update-prepare group-label:"Prepare incident update"
// dex:explanation text:"Pass the incident status request to the connector."
type prepareIncidentUpdate struct{ dex.StepDefaults }

func (prepareIncidentUpdate) GetStepType() string { return "PrepareIncidentUpdate" }
func (prepareIncidentUpdate) WaitFor(dex.Context, IncidentUpdateRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareIncidentUpdate) Execute(_ dex.Context, request IncidentUpdateRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[IncidentUpdateRequest](incidentUpdateStepType), request), nil
}

// dex:group group-id:pagerduty-update-complete group-label:"Complete incident update"
// dex:explanation text:"Store and return the updated incident."
type completeIncidentUpdate struct {
	dex.StepDefaultsNoWaitFor[pagerduty.UpdateIncidentResult]
}

func (completeIncidentUpdate) GetStepType() string { return "CompleteIncidentUpdate" }
func (completeIncidentUpdate) Execute(ctx dex.Context, result pagerduty.UpdateIncidentResult) (*dex.StepDecision, error) {
	if err := updatedIncidentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*IncidentUpdateFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*IncidentUpdateFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*IncidentUpdateFlow)(nil).GetDexDisplay
