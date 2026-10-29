// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Contentful's getEntry operation in a Dex Flow.
package flow

import (
	"github.com/superdurable/dex-connectors-library/connectors/contentful"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow type.
	FlowType = "ContentfulSample"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "contentful"
	callStepType   = "CallContentful"
)

// Flow calls Contentful once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection contentful.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection contentful.Connection) *Flow { return &Flow{connection: connection} }

// GetPersistenceSchema declares no durable Attributes or Streams for this sample.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetFlowType returns the sample Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(contentful.NewGetEntryStep(contentful.GetEntryStepConfig[contentful.GetEntryInput]{
			StepType: callStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "contentful", GroupLabel: "Contentful", Explanation: "Get one published Contentful entry."},
			MapToOperationInput: MapToOperationInput, Found: sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}

// MapToOperationInput passes the validated Flow request to the connector.
func MapToOperationInput(request contentful.GetEntryInput) contentful.GetEntryInput { return request }

// dex:group group-id:contentful-sample group-label:"Contentful sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartContentfulRequest" }
func (startRequest) WaitFor(dex.Context, contentful.GetEntryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request contentful.GetEntryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[contentful.GetEntryInput](callStepType), request), nil
}

// dex:group group-id:contentful-sample group-label:"Contentful sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[contentful.GetEntryResult]
}

func (completeRequest) GetStepType() string { return "CompleteContentfulRequest" }
func (completeRequest) Execute(_ dex.Context, result contentful.GetEntryResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
