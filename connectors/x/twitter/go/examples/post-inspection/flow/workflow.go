// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postinspection runs X getPost as one Dex Flow.
package postinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/x/twitter/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "XPostInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "x-twitter"
	operationStepType = "CallGetPost"
)

var postAttribute = dex.DefineAttribute[x.PostMetadata]("x-post")

// Flow runs GetPost once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection x.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection x.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{postAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(x.NewGetPostStep(x.GetPostStepConfig[x.GetPostInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "X", Explanation: "Read a post from X."},
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

// dex:field attribute-key:x-post value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"x-post": outcome}}, nil
}

// dex:field attribute-key:x-post value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"x-post": outcome}}, nil
}

func readPost(ctx dex.Context) (x.PostMetadata, error) {
	outcome, err := postAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return x.PostMetadata{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input x.GetPostInput) x.GetPostInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPost" }
func (startRequest) WaitFor(dex.Context, x.GetPostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input x.GetPostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[x.GetPostInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[x.GetPostResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPost" }
func (completeResult) Execute(ctx dex.Context, result x.GetPostResult) (*dex.StepDecision, error) {
	if err := postAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
