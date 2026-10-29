// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Webflow's listCollectionItems operation in a Dex Flow.
package flow

import (
	"github.com/superdurable/dex-connectors-library/connectors/webflow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow type.
	FlowType = "WebflowSample"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "webflow"
	callStepType   = "CallWebflow"
)

// Flow calls Webflow once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection webflow.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection webflow.Connection) *Flow { return &Flow{connection: connection} }

// GetPersistenceSchema declares no durable Attributes or Streams for this sample.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetFlowType returns the sample Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(webflow.NewListCollectionItemsStep(webflow.ListCollectionItemsStepConfig[webflow.ListCollectionItemsInput]{
			StepType: callStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "webflow", GroupLabel: "Webflow", Explanation: "List a page of staged CMS collection items."},
			MapToOperationInput: MapToOperationInput, Listed: sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}

// MapToOperationInput passes the validated Flow request to the connector.
func MapToOperationInput(request webflow.ListCollectionItemsInput) webflow.ListCollectionItemsInput {
	return request
}

// dex:group group-id:webflow-sample group-label:"Webflow sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartWebflowRequest" }
func (startRequest) WaitFor(dex.Context, webflow.ListCollectionItemsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request webflow.ListCollectionItemsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[webflow.ListCollectionItemsInput](callStepType), request), nil
}

// dex:group group-id:webflow-sample group-label:"Webflow sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[webflow.ListCollectionItemsResult]
}

func (completeRequest) GetStepType() string { return "CompleteWebflowRequest" }
func (completeRequest) Execute(_ dex.Context, result webflow.ListCollectionItemsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
