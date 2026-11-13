// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tags runs Weiban listTags as one Dex Flow.
package tags

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/weiban/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WeibanTags"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "weiban"
	operationStepType = "CallListTags"
)

var tagsOutcomeAttribute = dex.DefineAttribute[weiban.TagsResult]("weiban-tags-outcome")

// Flow runs ListTags once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection weiban.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection weiban.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tagsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(weiban.NewListTagsStep(weiban.ListTagsStepConfig[weiban.ListTagsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Weiban", Explanation: "List the first ten Weiban customer tags."},
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

// dex:field attribute-key:weiban-tags-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTagsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"weiban-tags-outcome": outcome}}, nil
}

// dex:field attribute-key:weiban-tags-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTagsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"weiban-tags-outcome": outcome}}, nil
}

func readTagsOutcome(ctx dex.Context) (weiban.TagsResult, error) {
	outcome, err := tagsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return weiban.TagsResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input weiban.ListTagsInput) weiban.ListTagsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTags" }
func (startRequest) WaitFor(dex.Context, weiban.ListTagsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input weiban.ListTagsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[weiban.ListTagsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[weiban.ListTagsResult]
}

func (completeResult) GetStepType() string { return "CompleteListTags" }
func (completeResult) Execute(ctx dex.Context, result weiban.ListTagsResult) (*dex.StepDecision, error) {
	if err := tagsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
