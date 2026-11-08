// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package property runs Google Analytics 4 getProperty as one Dex Flow.
package property

import (
	"errors"

	analytics4 "github.com/superdurable/dex-connectors-library/connectors/google/analytics/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleAnalytics4Property"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-analytics-4"
	operationStepType = "CallGetProperty"
)

var propertyAttribute = dex.DefineAttribute[analytics4.Property]("google-analytics-4-property")

// Flow runs GetProperty once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection analytics4.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection analytics4.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{propertyAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(analytics4.NewGetPropertyStep(analytics4.GetPropertyStepConfig[analytics4.GetPropertyInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Analytics 4", Explanation: "Read the GA4 property display name and reporting settings."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-analytics-4-property value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProperty(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-analytics-4-property": outcome}}, nil
}

// dex:field attribute-key:google-analytics-4-property value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProperty(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-analytics-4-property": outcome}}, nil
}

func readProperty(ctx dex.Context) (analytics4.Property, error) {
	outcome, err := propertyAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return analytics4.Property{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input analytics4.GetPropertyInput) analytics4.GetPropertyInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetProperty" }
func (startRequest) WaitFor(dex.Context, analytics4.GetPropertyInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input analytics4.GetPropertyInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[analytics4.GetPropertyInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[analytics4.GetPropertyResult]
}

func (completeResult) GetStepType() string { return "CompleteGetProperty" }
func (completeResult) Execute(ctx dex.Context, result analytics4.GetPropertyResult) (*dex.StepDecision, error) {
	if err := propertyAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
