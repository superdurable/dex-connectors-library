// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sites runs Google Search Console listSites as one Dex Flow.
package sites

import (
	"errors"

	searchconsole "github.com/superdurable/dex-connectors-library/connectors/google/search-console/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleSearchConsoleSites"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-search-console"
	operationStepType = "CallListSites"
)

var sitesPageAttribute = dex.DefineAttribute[searchconsole.SitesPage]("google-search-console-sites-page")

// Flow runs ListSites once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection searchconsole.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection searchconsole.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sitesPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(searchconsole.NewListSitesStep(searchconsole.ListSitesStepConfig[searchconsole.ListSitesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Search Console", Explanation: "Call the provider's listSites operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-search-console-sites-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSitesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-search-console-sites-page": outcome}}, nil
}

// dex:field attribute-key:google-search-console-sites-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSitesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-search-console-sites-page": outcome}}, nil
}

func readSitesPage(ctx dex.Context) (searchconsole.SitesPage, error) {
	outcome, err := sitesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return searchconsole.SitesPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input searchconsole.ListSitesInput) searchconsole.ListSitesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListSites" }
func (startRequest) WaitFor(dex.Context, searchconsole.ListSitesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input searchconsole.ListSitesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[searchconsole.ListSitesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[searchconsole.ListSitesResult]
}

func (completeResult) GetStepType() string { return "CompleteListSites" }
func (completeResult) Execute(ctx dex.Context, result searchconsole.ListSitesResult) (*dex.StepDecision, error) {
	if err := sitesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
