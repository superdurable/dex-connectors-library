// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postinspection runs Facebook Pages getPost as one Dex Flow.
package postinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-pages/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FacebookPagesPostInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "facebook-pages"
	operationStepType = "CallGetPost"
)

var postMetadataAttribute = dex.DefineAttribute[facebookpages.PagePostMetadata]("facebook-pages-post-metadata")

// Flow runs GetPost once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection facebookpages.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection facebookpages.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{postMetadataAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(facebookpages.NewGetPostStep(facebookpages.GetPostStepConfig[facebookpages.GetPostInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Facebook Pages", Explanation: "Call the provider's getPost operation."},
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

// dex:field attribute-key:facebook-pages-post-metadata value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPagePostMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-post-metadata": outcome}}, nil
}

// dex:field attribute-key:facebook-pages-post-metadata value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPagePostMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-post-metadata": outcome}}, nil
}

func readPagePostMetadata(ctx dex.Context) (facebookpages.PagePostMetadata, error) {
	outcome, err := postMetadataAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return facebookpages.PagePostMetadata{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input facebookpages.GetPostInput) facebookpages.GetPostInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPost" }
func (startRequest) WaitFor(dex.Context, facebookpages.GetPostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input facebookpages.GetPostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[facebookpages.GetPostInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[facebookpages.GetPostResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPost" }
func (completeResult) Execute(ctx dex.Context, result facebookpages.GetPostResult) (*dex.StepDecision, error) {
	if err := postMetadataAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
