// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sendbatchemails demonstrates keyed Resend batch sending.
package sendbatchemails

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendSendBatchEmails"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies an ordered batch of email messages.
type Request resend.SendBatchEmailsInput

var outcomeAttribute = dex.DefineAttribute[resend.SentEmailPage]("resend-sent-batch")

// Flow sends one Resend batch and completes with its email IDs.
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

// GetPersistenceSchema registers the sent batch result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the sent email IDs.
// dex:field attribute-key:resend-sent-batch value-type:json editable:false description:"Queued email IDs"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readBatchOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-sent-batch": value}}, nil
}

// GetDexDisplay returns the sent email IDs.
// dex:field attribute-key:resend-sent-batch value-type:json editable:false description:"Queued email IDs"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readBatchOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-sent-batch": value}}, nil
}

func readBatchOutcome(ctx dex.Context) (resend.SentEmailPage, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.SentEmailPage{}, nil
	}
	return value, err
}

// GetSteps registers the batch send path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareBatch{}),
		dex.DefineStep(resend.NewSendBatchEmailsStep(resend.SendBatchEmailsStepConfig[Request]{
			StepType: "SendBatchEmails", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-batch", GroupLabel: "Resend batch", Explanation: "Send the selected email messages together with one provider request key."},
			MapToOperationInput: func(request Request) resend.SendBatchEmailsInput { return resend.SendBatchEmailsInput(request) },
			Sent:             sdkgo.GoTo(completeBatch{}),
		})),
		dex.DefineStep(completeBatch{}),
	}
}

// dex:group group-id:resend-batch-prepare group-label:"Prepare batch"
// dex:explanation text:"Pass the typed batch request into the provider send."
type prepareBatch struct{ dex.StepDefaults }

func (prepareBatch) GetStepType() string { return "PrepareBatch" }
func (prepareBatch) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareBatch) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("SendBatchEmails"), request), nil
}

// dex:group group-id:resend-batch-complete group-label:"Complete batch send"
// dex:explanation text:"Complete with Resend's queued email identifiers."
type completeBatch struct {
	dex.StepDefaultsNoWaitFor[resend.SendBatchEmailsResult]
}

func (completeBatch) GetStepType() string { return "CompleteBatch" }
func (completeBatch) Execute(ctx dex.Context, result resend.SendBatchEmailsResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
