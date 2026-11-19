// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package profileinspection runs Braze getUserProfile as one Dex Flow.
package profileinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/braze/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BrazeProfileInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName        = "braze"
	operationStepType     = "CallGetUserProfile"
	listCampaignsStepType = "CallListCampaigns"
	campaignSendStepType  = "CallTriggerCampaignSend"
	canvasSendStepType    = "CallTriggerCanvasSend"
)

var profileAttribute = dex.DefineAttribute[braze.UserProfile]("braze-profile")
var externalIDAttribute = dex.DefineAttribute[string]("braze-external-id")
var campaignIDAttribute = dex.DefineAttribute[string]("braze-campaign-id")
var canvasIDAttribute = dex.DefineAttribute[string]("braze-canvas-id")

// Request selects a profile and optional targeted campaign or Canvas sends.
type Request struct {
	// ExternalID identifies the existing Braze user.
	ExternalID string `json:"externalId"`
	// CampaignID optionally triggers an API-enabled campaign for that user.
	CampaignID string `json:"campaignId,omitempty"`
	// CanvasID optionally triggers an API-enabled Canvas for that user.
	CanvasID string `json:"canvasId,omitempty"`
}

// Flow runs GetUserProfile once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection braze.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection braze.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{profileAttribute, externalIDAttribute, campaignIDAttribute, canvasIDAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(braze.NewListCampaignsStep(braze.ListCampaignsStepConfig[Request]{
			StepType: listCampaignsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braze", Explanation: "List campaign identities from Braze."},
			Connection:          flow.connection,
			MapToOperationInput: func(Request) braze.ListCampaignsInput { return braze.ListCampaignsInput{Page: 0} },
			Listed:              sdkgo.GoTo(continueAfterCampaigns{}),
		})),
		dex.DefineStep(continueAfterCampaigns{}),
		dex.DefineStep(braze.NewGetUserProfileStep(braze.GetUserProfileStepConfig[braze.GetUserProfileInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braze", Explanation: "Read a profile from Braze."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
		dex.DefineStep(braze.NewTriggerCampaignSendStep(braze.TriggerCampaignSendStepConfig[braze.TriggerCampaignSendInput]{
			StepType: campaignSendStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braze", Explanation: "Trigger the selected campaign for the inspected user."},
			MapToOperationInput: func(input braze.TriggerCampaignSendInput) braze.TriggerCampaignSendInput { return input },
			Sent:                sdkgo.GoTo(completeAfterCampaignSend{}),
		})),
		dex.DefineStep(completeAfterCampaignSend{}),
		dex.DefineStep(braze.NewTriggerCanvasSendStep(braze.TriggerCanvasSendStepConfig[braze.TriggerCanvasSendInput]{
			StepType: canvasSendStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braze", Explanation: "Trigger the selected Canvas for the inspected user."},
			MapToOperationInput: func(input braze.TriggerCanvasSendInput) braze.TriggerCanvasSendInput { return input },
			Sent:                sdkgo.GoTo(completeAfterCanvasSend{}),
		})),
		dex.DefineStep(completeAfterCanvasSend{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braze-profile value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braze-profile": outcome}}, nil
}

// dex:field attribute-key:braze-profile value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braze-profile": outcome}}, nil
}

func readProfile(ctx dex.Context) (braze.UserProfile, error) {
	outcome, err := profileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braze.UserProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input braze.GetUserProfileInput) braze.GetUserProfileInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetUserProfile" }
func (startRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(ctx dex.Context, input Request) (*dex.StepDecision, error) {
	if err := externalIDAttribute.Set(ctx, input.ExternalID); err != nil {
		return nil, err
	}
	if input.CampaignID != "" {
		if err := campaignIDAttribute.Set(ctx, input.CampaignID); err != nil {
			return nil, err
		}
	}
	if input.CanvasID != "" {
		if err := canvasIDAttribute.Set(ctx, input.CanvasID); err != nil {
			return nil, err
		}
	}
	return dex.GoTo(sdkgo.StepRef[Request](listCampaignsStepType), input), nil
}

// dex:group group-id:provider group-label:"Braze"
// dex:explanation text:"Continue to profile lookup after campaign discovery."
type continueAfterCampaigns struct {
	dex.StepDefaultsNoWaitFor[braze.ListCampaignsResult]
}

func (continueAfterCampaigns) Execute(ctx dex.Context, _ braze.ListCampaignsResult) (*dex.StepDecision, error) {
	externalID, err := externalIDAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[braze.GetUserProfileInput](operationStepType), braze.GetUserProfileInput{ExternalID: externalID}), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[braze.GetUserProfileResult]
}

func (completeResult) GetStepType() string { return "CompleteGetUserProfile" }
func (completeResult) Execute(ctx dex.Context, result braze.GetUserProfileResult) (*dex.StepDecision, error) {
	if err := profileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	var missing *dex.AttributeNotFoundError
	campaignID, err := campaignIDAttribute.Get(ctx)
	if err == nil {
		return dex.GoTo(sdkgo.StepRef[braze.TriggerCampaignSendInput](campaignSendStepType), braze.TriggerCampaignSendInput{CampaignID: campaignID, ExternalID: result.Value.ExternalID}), nil
	}
	if !errors.As(err, &missing) {
		return nil, err
	}
	canvasID, err := canvasIDAttribute.Get(ctx)
	if err == nil {
		return dex.GoTo(sdkgo.StepRef[braze.TriggerCanvasSendInput](canvasSendStepType), braze.TriggerCanvasSendInput{CanvasID: canvasID, ExternalID: result.Value.ExternalID}), nil
	}
	if !errors.As(err, &missing) {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Continue to a selected Canvas after campaign dispatch."
type completeAfterCampaignSend struct {
	dex.StepDefaultsNoWaitFor[braze.TriggerCampaignSendResult]
}

func (completeAfterCampaignSend) Execute(ctx dex.Context, _ braze.TriggerCampaignSendResult) (*dex.StepDecision, error) {
	var missing *dex.AttributeNotFoundError
	canvasID, err := canvasIDAttribute.Get(ctx)
	if err == nil {
		externalID, err := externalIDAttribute.Get(ctx)
		if err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[braze.TriggerCanvasSendInput](canvasSendStepType), braze.TriggerCanvasSendInput{CanvasID: canvasID, ExternalID: externalID}), nil
	}
	if !errors.As(err, &missing) {
		return nil, err
	}
	profile, err := profileAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(profile), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after the selected Canvas dispatch."
type completeAfterCanvasSend struct {
	dex.StepDefaultsNoWaitFor[braze.TriggerCanvasSendResult]
}

func (completeAfterCanvasSend) Execute(ctx dex.Context, _ braze.TriggerCanvasSendResult) (*dex.StepDecision, error) {
	profile, err := profileAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(profile), nil
}

var _ dex.Flow = (*Flow)(nil)
