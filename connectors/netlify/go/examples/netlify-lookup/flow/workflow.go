// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package netlifylookup demonstrates Netlify listSites and getSite Steps in a runnable Dex Flow.
package netlifylookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/netlify/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex identity for this example.
const FlowType = "NetlifyLookup"

// ConnectionName is the static Dex Web connection used by the example.
const ConnectionName = "netlify"
const operationStepType = "ListSitesNetlify"
const siteStepType = "GetSiteNetlify"

var (
	lookupRequestAttribute = dex.DefineAttribute[LookupRequest]("netlify-lookup-request")
	lookupOutputAttribute  = dex.DefineAttribute[LookupOutput]("netlify-lookup-output")
)

// LookupRequest is the provider input entered in Dex Web Start Flow.
type LookupRequest = netlify.ListSitesInput

// LookupOutput is the value returned when the provider call succeeds.
type LookupOutput = netlify.Site

// Flow invokes one Netlify operation and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection netlify.Connection
}

// NewFlow binds the connection at registration time.
func NewFlow(connection netlify.Connection) *Flow { return &Flow{connection: connection} }

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
// dex:field attribute-key:netlify-lookup-request value-type:object editable:false description:"Netlify lookup request"
// dex:field attribute-key:netlify-lookup-output value-type:object editable:false description:"Netlify lookup output"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"netlify-lookup-request": request, "netlify-lookup-output": output}}, nil
}

// GetDexDisplay returns the stored request and output when available.
// dex:field attribute-key:netlify-lookup-request value-type:object editable:false description:"Netlify lookup request"
// dex:field attribute-key:netlify-lookup-output value-type:object editable:false description:"Netlify lookup output"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := lookupInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"netlify-lookup-request": request, "netlify-lookup-output": output}}, nil
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
		dex.DefineStep(netlify.NewListSitesStep(netlify.ListSitesStepConfig[LookupRequest]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "netlify", GroupLabel: "Netlify", Explanation: "List sites visible to the Netlify token."},
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(selectSite{}),
		})),
		dex.DefineStep(selectSite{}),
		dex.DefineStep(netlify.NewGetSiteStep(netlify.GetSiteStepConfig[netlify.GetSiteInput]{
			StepType: siteStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "netlify", GroupLabel: "Netlify", Explanation: "Read the first listed site by its Project ID."},
			MapToOperationInput: func(input netlify.GetSiteInput) netlify.GetSiteInput { return input },
			Found: sdkgo.GoTo(completeLookup{}),
		})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToOperationInput passes the typed start input to the connector Step.
func MapToOperationInput(request LookupRequest) netlify.ListSitesInput { return request }

// dex:group group-id:netlify group-label:"Netlify"
// dex:explanation text:"Select the first listed Netlify site for inspection."
type selectSite struct{ dex.StepDefaultsNoWaitFor[netlify.ListSitesResult] }

func (selectSite) Execute(_ dex.Context, result netlify.ListSitesResult) (*dex.StepDecision, error) {
	if len(result.Value.Sites) == 0 {
		return dex.ForceFail("Netlify returned no sites to inspect"), nil
	}
	return dex.GoTo(sdkgo.StepRef[netlify.GetSiteInput](siteStepType), netlify.GetSiteInput{SiteID: result.Value.Sites[0].ID}), nil
}

// dex:group group-id:netlify group-label:"Netlify"
// dex:explanation text:"Start the Netlify lookup with its typed request."
type startLookup struct{ dex.StepDefaults }

func (startLookup) GetStepType() string { return "StartNetlifyLookup" }
func (startLookup) WaitFor(dex.Context, LookupRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLookup) Execute(ctx dex.Context, request LookupRequest) (*dex.StepDecision, error) {
	if err := lookupRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[LookupRequest](operationStepType), request), nil
}

// dex:group group-id:netlify group-label:"Netlify"
// dex:explanation text:"Complete with the Netlify result."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[netlify.GetSiteResult]
}

func (completeLookup) GetStepType() string { return "CompleteNetlifyLookup" }
func (completeLookup) Execute(ctx dex.Context, result netlify.GetSiteResult) (*dex.StepDecision, error) {
	if err := lookupOutputAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
