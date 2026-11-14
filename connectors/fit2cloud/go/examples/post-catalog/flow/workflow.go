// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postcatalog runs Halo listPosts as one Dex Flow.
package postcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/fit2cloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "Fit2cloudPostCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "fit2cloud"
	operationStepType = "CallListPosts"
)

var postCatalogOutcomeAttribute = dex.DefineAttribute[fit2cloud.ListPostsOutput]("fit2cloud-post-catalog-outcome")

// Flow runs ListPosts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection fit2cloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection fit2cloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{postCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(fit2cloud.NewListPostsStep(fit2cloud.ListPostsStepConfig[fit2cloud.ListPostsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Halo", Explanation: "Read one Halo article page with titles, publication state, and next-page number."},
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

// dex:field attribute-key:fit2cloud-post-catalog-outcome value-type:json editable:false description:"Article catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPostCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fit2cloud-post-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:fit2cloud-post-catalog-outcome value-type:json editable:false description:"Article catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPostCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fit2cloud-post-catalog-outcome": outcome}}, nil
}

func readPostCatalogOutcome(ctx dex.Context) (fit2cloud.ListPostsOutput, error) {
	outcome, err := postCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return fit2cloud.ListPostsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input fit2cloud.ListPostsInput) fit2cloud.ListPostsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListPosts" }
func (startRequest) WaitFor(dex.Context, fit2cloud.ListPostsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input fit2cloud.ListPostsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[fit2cloud.ListPostsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[fit2cloud.ListPostsResult]
}

func (completeResult) GetStepType() string { return "CompleteListPosts" }
func (completeResult) Execute(ctx dex.Context, result fit2cloud.ListPostsResult) (*dex.StepDecision, error) {
	if err := postCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
