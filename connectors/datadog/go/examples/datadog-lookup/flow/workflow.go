// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package datadoglookup demonstrates the Datadog queryMetrics Step in a runnable Dex Flow.
package datadoglookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/datadog/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex identity for this example.
const FlowType = "DatadogLookup"

// ConnectionName is the static Dex Web connection used by the example.
const ConnectionName = "datadog"
const operationStepType = "QueryMetricsDatadog"

var (
	lookupRequestAttribute = dex.DefineAttribute[LookupRequest]("datadog-lookup-request")
	lookupOutputAttribute  = dex.DefineAttribute[LookupOutput]("datadog-lookup-output")
)

// LookupRequest is the provider input entered in Dex Web Start Flow.
type LookupRequest = datadog.QueryMetricsInput

// LookupOutput is the value returned when the provider call succeeds.
type LookupOutput = datadog.MetricsQuery

// Flow invokes one Datadog operation and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection datadog.Connection
}

// NewFlow binds the connection at registration time.
func NewFlow(connection datadog.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow type.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the request and output Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{lookupRequestAttribute, lookupOutputAttribute}}
}

// GetRPCs registers read-only summary and display RPCs for Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored request and output when available.
// dex:field attribute-key:datadog-lookup-request value-type:object editable:false description:"Datadog lookup request"
// dex:field attribute-key:datadog-lookup-output value-type:object editable:false description:"Datadog lookup output"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"datadog-lookup-request": request, "datadog-lookup-output": output}}, nil
}

// GetDexDisplay returns the stored request and output when available.
// dex:field attribute-key:datadog-lookup-request value-type:object editable:false description:"Datadog lookup request"
// dex:field attribute-key:datadog-lookup-output value-type:object editable:false description:"Datadog lookup output"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"datadog-lookup-request": request, "datadog-lookup-output": output}}, nil
}
func lookupInspection(ctx dex.Context) (LookupRequest, LookupOutput, error) {
	request, err := optionalAttribute(ctx, lookupRequestAttribute)
	if err != nil {
		return LookupRequest{}, LookupOutput{}, err
	}
	output, err := optionalAttribute(ctx, lookupOutputAttribute)
	return request, output, err
}
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		var zero T
		return zero, nil
	}
	return value, err
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLookup{}),
		dex.DefineStep(datadog.NewQueryMetricsStep(datadog.QueryMetricsStepConfig[LookupRequest]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "datadog", GroupLabel: "Datadog", Explanation: "Query Datadog timeseries points over an explicit Unix-second interval."},
			MapToOperationInput: MapToOperationInput,
			Queried:             sdkgo.GoTo(completeLookup{}),
		})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToOperationInput passes the typed start input to the connector Step.
func MapToOperationInput(request LookupRequest) datadog.QueryMetricsInput { return request }

// dex:group group-id:datadog group-label:"Datadog"
// dex:explanation text:"Start the Datadog lookup with its typed request."
type startLookup struct{ dex.StepDefaults }

func (startLookup) GetStepType() string { return "StartDatadogLookup" }
func (startLookup) WaitFor(dex.Context, LookupRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLookup) Execute(ctx dex.Context, request LookupRequest) (*dex.StepDecision, error) {
	if err := lookupRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LookupRequest](operationStepType), request), nil
}

// dex:group group-id:datadog group-label:"Datadog"
// dex:explanation text:"Complete with the Datadog result."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[datadog.QueryMetricsResult]
}

func (completeLookup) GetStepType() string { return "CompleteDatadogLookup" }
func (completeLookup) Execute(ctx dex.Context, result datadog.QueryMetricsResult) (*dex.StepDecision, error) {
	if err := lookupOutputAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
