// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createwebhook demonstrates Resend draft webhook creation.
package createwebhook

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateWebhook"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new webhook settings.
type Request resend.CreateWebhookInput

var outcomeAttribute = dex.DefineAttribute[resend.WebhookReference]("resend-webhook-created")

// Flow creates one Resend draft webhook and completes with its ID.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the verification result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created webhook ID.
// dex:field attribute-key:resend-webhook-created value-type:json editable:false description:"Created webhook"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readWebhookOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-webhook-created": value}}, nil
}

// GetDexDisplay returns the created webhook ID.
// dex:field attribute-key:resend-webhook-created value-type:json editable:false description:"Created webhook"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readWebhookOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-webhook-created": value}}, nil
}

func readWebhookOutcome(ctx dex.Context) (resend.WebhookReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.WebhookReference{}, nil
	}
	return value, err
}

// GetSteps registers the webhook creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareWebhook{}),
		dex.DefineStep(resend.NewCreateWebhookStep(resend.CreateWebhookStepConfig[Request]{
			StepType: "CreateWebhook", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-webhook", GroupLabel: "Resend webhook", Explanation: "Create the selected Resend draft webhook."},
			MapToOperationInput: func(request Request) resend.CreateWebhookInput { return resend.CreateWebhookInput(request) },
			Created:             sdkgo.GoTo(completeWebhook{}),
		})),
		dex.DefineStep(completeWebhook{}),
	}
}

// dex:group group-id:resend-webhook-prepare group-label:"Prepare webhook"
// dex:explanation text:"Pass the typed webhook request into the provider create."
type prepareWebhook struct{ dex.StepDefaults }

func (prepareWebhook) GetStepType() string { return "PrepareWebhook" }
func (prepareWebhook) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareWebhook) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateWebhook"), request), nil
}

// dex:group group-id:resend-webhook-complete group-label:"Complete webhook creation"
// dex:explanation text:"Complete after Resend creates the webhook."
type completeWebhook struct {
	dex.StepDefaultsNoWaitFor[resend.CreateWebhookResult]
}

func (completeWebhook) GetStepType() string { return "CompleteWebhook" }
func (completeWebhook) Execute(ctx dex.Context, result resend.CreateWebhookResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
