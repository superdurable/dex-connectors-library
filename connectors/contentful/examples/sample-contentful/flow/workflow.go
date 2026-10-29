// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Contentful's getEntry operation in a Dex Flow.
package flow

import (
	"errors"
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

var sampleResultAttribute = dex.DefineAttribute[contentful.GetEntryOutput]("contentful-sample-result")

// Flow calls Contentful once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection contentful.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection contentful.Connection) *Flow { return &Flow{connection: connection} }

// GetPersistenceSchema registers the completed provider result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sampleResultAttribute}}
}

// GetFlowType returns the sample Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes the result to Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the completed provider result when available.
// dex:field attribute-key:contentful-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"contentful-sample-result": result}}, nil
}

// GetDexDisplay returns the completed provider result when available.
// dex:field attribute-key:contentful-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"contentful-sample-result": result}}, nil
}

func inspectResult(ctx dex.Context) (contentful.GetEntryOutput, error) {
	result, err := sampleResultAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return contentful.GetEntryOutput{}, err
	}
	return result, nil
}

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

// dex:group group-id:contentful-start group-label:"Contentful sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartContentfulRequest" }
func (startRequest) WaitFor(dex.Context, contentful.GetEntryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request contentful.GetEntryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[contentful.GetEntryInput](callStepType), request), nil
}

// dex:group group-id:contentful-complete group-label:"Contentful sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[contentful.GetEntryResult]
}

func (completeRequest) GetStepType() string { return "CompleteContentfulRequest" }
func (completeRequest) Execute(ctx dex.Context, result contentful.GetEntryResult) (*dex.StepDecision, error) {
	if err := sampleResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
