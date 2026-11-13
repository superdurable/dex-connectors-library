// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tagcatalog runs JINGdigital listTags as one Dex Flow.
package tagcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/jingdigital/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JINGdigitalTagCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jingdigital"
	operationStepType = "CallListTags"
)

var tagCatalogOutcomeAttribute = dex.DefineAttribute[jingdigital.ListTagsOutput]("jingdigital-tag-directory-outcome")

// Flow runs ListTags once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jingdigital.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jingdigital.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tagCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jingdigital.NewListTagsStep(jingdigital.ListTagsStepConfig[jingdigital.ListTagsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "JINGdigital", Explanation: "Read one tag page with IDs, names, hierarchy levels, and next page."},
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

// dex:field attribute-key:jingdigital-tag-directory-outcome value-type:json editable:false description:"Tag catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTagCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jingdigital-tag-directory-outcome": outcome}}, nil
}

// dex:field attribute-key:jingdigital-tag-directory-outcome value-type:json editable:false description:"Tag catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTagCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jingdigital-tag-directory-outcome": outcome}}, nil
}

func readTagCatalogOutcome(ctx dex.Context) (jingdigital.ListTagsOutput, error) {
	outcome, err := tagCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jingdigital.ListTagsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input jingdigital.ListTagsInput) jingdigital.ListTagsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTags" }
func (startRequest) WaitFor(dex.Context, jingdigital.ListTagsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jingdigital.ListTagsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jingdigital.ListTagsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jingdigital.ListTagsResult]
}

func (completeResult) GetStepType() string { return "CompleteListTags" }
func (completeResult) Execute(ctx dex.Context, result jingdigital.ListTagsResult) (*dex.StepDecision, error) {
	if err := tagCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
