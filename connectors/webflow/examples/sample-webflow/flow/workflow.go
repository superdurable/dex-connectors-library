// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Webflow's listCollectionItems operation in a Dex Flow.
package flow

import (
	"errors"
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

var sampleResultAttribute = dex.DefineAttribute[webflow.ListCollectionItemsOutput]("webflow-sample-result")

// Flow calls Webflow once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection webflow.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection webflow.Connection) *Flow { return &Flow{connection: connection} }

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
// dex:field attribute-key:webflow-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"webflow-sample-result": result}}, nil
}

// GetDexDisplay returns the completed provider result when available.
// dex:field attribute-key:webflow-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"webflow-sample-result": result}}, nil
}

func inspectResult(ctx dex.Context) (webflow.ListCollectionItemsOutput, error) {
	result, err := sampleResultAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return webflow.ListCollectionItemsOutput{}, err
	}
	return result, nil
}

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

// dex:group group-id:webflow-start group-label:"Webflow sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartWebflowRequest" }
func (startRequest) WaitFor(dex.Context, webflow.ListCollectionItemsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request webflow.ListCollectionItemsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[webflow.ListCollectionItemsInput](callStepType), request), nil
}

// dex:group group-id:webflow-complete group-label:"Webflow sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[webflow.ListCollectionItemsResult]
}

func (completeRequest) GetStepType() string { return "CompleteWebflowRequest" }
func (completeRequest) Execute(ctx dex.Context, result webflow.ListCollectionItemsResult) (*dex.StepDecision, error) {
	if err := sampleResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
