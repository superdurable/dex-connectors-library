// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package incidentreview demonstrates one PagerDuty operation in a Dex Flow.
package incidentreview

import (
	"github.com/superdurable/dex-connectors-library/connectors/pagerduty"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "PagerDutyIncidentReview"
	// ConnectionName is the static Dex Web connection for this example.
	ConnectionName    = "pagerduty"
	operationStepType = "ListIncidents"
)

// Request is the typed start input for this example.
type Request pagerduty.ListIncidentsInput

// Flow runs one PagerDuty request and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection pagerduty.Connection
}

// NewFlow binds a configured PagerDuty connection.
func NewFlow(connection pagerduty.Connection) *Flow { return &Flow{connection: connection} }
func (*Flow) GetFlowType() string                   { return FlowType }
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareRequest{}),
		dex.DefineStep(pagerduty.NewListIncidentsStep(pagerduty.ListIncidentsStepConfig[Request]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "pagerduty", GroupLabel: "PagerDuty", Explanation: "Call the PagerDuty ListIncidents endpoint."},
			MapToOperationInput: func(request Request) pagerduty.ListIncidentsInput { return pagerduty.ListIncidentsInput(request) },
			Listed:              sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// dex:group group-id:pagerduty-example group-label:"PagerDuty example"
// dex:explanation text:"Persist the typed start input before the provider operation."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareRequest" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](operationStepType), request), nil
}

// dex:group group-id:pagerduty-example group-label:"PagerDuty example"
// dex:explanation text:"Complete the Flow with the provider result."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[pagerduty.ListIncidentsResult]
}

func (completeRequest) GetStepType() string { return "CompleteRequest" }
func (completeRequest) Execute(_ dex.Context, result pagerduty.ListIncidentsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
