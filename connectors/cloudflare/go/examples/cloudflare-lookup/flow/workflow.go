// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cloudflarelookup demonstrates the Cloudflare listZones Step in a runnable Dex Flow.
package cloudflarelookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/cloudflare/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex identity for this example.
const FlowType = "CloudflareLookup"

// ConnectionName is the static Dex Web connection used by the example.
const ConnectionName = "cloudflare"
const operationStepType = "ListZonesCloudflare"

var (
	lookupRequestAttribute = dex.DefineAttribute[LookupRequest]("cloudflare-lookup-request")
	lookupOutputAttribute  = dex.DefineAttribute[LookupOutput]("cloudflare-lookup-output")
)

// LookupRequest is the provider input entered in Dex Web Start Flow.
type LookupRequest = cloudflare.ListZonesInput

// LookupOutput is the value returned when the provider call succeeds.
type LookupOutput = cloudflare.ZonesPage

// Flow invokes one Cloudflare operation and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection cloudflare.Connection
}

// NewFlow binds the connection at registration time.
func NewFlow(connection cloudflare.Connection) *Flow { return &Flow{connection: connection} }

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
// dex:field attribute-key:cloudflare-lookup-request value-type:object editable:false description:"Cloudflare lookup request"
// dex:field attribute-key:cloudflare-lookup-output value-type:object editable:false description:"Cloudflare lookup output"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"cloudflare-lookup-request": request, "cloudflare-lookup-output": output}}, nil
}

// GetDexDisplay returns the stored request and output when available.
// dex:field attribute-key:cloudflare-lookup-request value-type:object editable:false description:"Cloudflare lookup request"
// dex:field attribute-key:cloudflare-lookup-output value-type:object editable:false description:"Cloudflare lookup output"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"cloudflare-lookup-request": request, "cloudflare-lookup-output": output}}, nil
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
		dex.DefineStep(cloudflare.NewListZonesStep(cloudflare.ListZonesStepConfig[LookupRequest]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "cloudflare", GroupLabel: "Cloudflare", Explanation: "List the first page of Cloudflare zones."},
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeLookup{}),
		})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToOperationInput passes the typed start input to the connector Step.
func MapToOperationInput(request LookupRequest) cloudflare.ListZonesInput { return request }

// dex:group group-id:cloudflare group-label:"Cloudflare"
// dex:explanation text:"Start the Cloudflare lookup with its typed request."
type startLookup struct{ dex.StepDefaults }

func (startLookup) GetStepType() string { return "StartCloudflareLookup" }
func (startLookup) WaitFor(dex.Context, LookupRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLookup) Execute(ctx dex.Context, request LookupRequest) (*dex.StepDecision, error) {
	if err := lookupRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LookupRequest](operationStepType), request), nil
}

// dex:group group-id:cloudflare group-label:"Cloudflare"
// dex:explanation text:"Complete with the Cloudflare result."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[cloudflare.ListZonesResult]
}

func (completeLookup) GetStepType() string { return "CompleteCloudflareLookup" }
func (completeLookup) Execute(ctx dex.Context, result cloudflare.ListZonesResult) (*dex.StepDecision, error) {
	if err := lookupOutputAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
