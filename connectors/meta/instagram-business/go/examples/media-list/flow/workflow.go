// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package medialist runs Instagram Business listMedia as one Dex Flow.
package medialist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/instagram-business/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "InstagramBusinessMediaList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "instagram-business"
	operationStepType = "CallListMedia"
)

var mediaListPageAttribute = dex.DefineAttribute[instagrambusiness.MediaListPage]("instagram-business-media-list-page")

// Flow runs ListMedia once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection instagrambusiness.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection instagrambusiness.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{mediaListPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(instagrambusiness.NewListMediaStep(instagrambusiness.ListMediaStepConfig[instagrambusiness.ListMediaInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Instagram Business", Explanation: "Call the provider's listMedia operation."},
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

// dex:field attribute-key:instagram-business-media-list-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readMediaListPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"instagram-business-media-list-page": outcome}}, nil
}

// dex:field attribute-key:instagram-business-media-list-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readMediaListPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"instagram-business-media-list-page": outcome}}, nil
}

func readMediaListPage(ctx dex.Context) (instagrambusiness.MediaListPage, error) {
	outcome, err := mediaListPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return instagrambusiness.MediaListPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input instagrambusiness.ListMediaInput) instagrambusiness.ListMediaInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListMedia" }
func (startRequest) WaitFor(dex.Context, instagrambusiness.ListMediaInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input instagrambusiness.ListMediaInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[instagrambusiness.ListMediaInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[instagrambusiness.ListMediaResult]
}

func (completeResult) GetStepType() string { return "CompleteListMedia" }
func (completeResult) Execute(ctx dex.Context, result instagrambusiness.ListMediaResult) (*dex.StepDecision, error) {
	if err := mediaListPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
