// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SendFlowType is the stable identity of the example email Flow.
const SendFlowType = "PostmarkSendEmail"

const sendStepType = "SendEmail"

var sentEmailAttribute = dex.DefineAttribute[postmark.SentEmail]("postmark-sent-email")

// SendRequest is the typed start input for one transactional email.
type SendRequest postmark.SendEmailInput

// SendFlow sends one transactional email and stores its Postmark identity.
type SendFlow struct {
	dex.FlowDefaults
	connection postmark.Connection
}

// NewSendFlow binds the configured Postmark connection.
func NewSendFlow(connection postmark.Connection) *SendFlow { return &SendFlow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*SendFlow) GetFlowType() string { return SendFlowType }

// GetSteps defines the prepare, provider send, and completion Steps.
func (flow *SendFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareSend{}),
		dex.DefineStep(postmark.NewSendEmailStep(postmark.SendEmailStepConfig[SendRequest]{
			StepType: sendStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "postmark-send", GroupLabel: "Send email", Explanation: "Send a transactional email through Postmark."},
			MapToOperationInput: func(request SendRequest) postmark.SendEmailInput { return postmark.SendEmailInput(request) },
			Sent:                sdkgo.GoTo(completeSend{}),
		})),
		dex.DefineStep(completeSend{}),
	}
}

// GetRPCs exposes the accepted message in Dex Web.
func (flow *SendFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the stored provider result.
func (*SendFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentEmailAttribute}}
}

// GetDexSummary returns the accepted message metadata.
// dex:field attribute-key:postmark-sent-email value-type:json editable:false description:"Accepted Postmark message"
func (*SendFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sent, err := readSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-sent-email": sent}}, nil
}

// GetDexDisplay returns the accepted message metadata.
// dex:field attribute-key:postmark-sent-email value-type:json editable:false description:"Accepted Postmark message"
func (*SendFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sent, err := readSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-sent-email": sent}}, nil
}

func readSentEmail(ctx dex.Context) (postmark.SentEmail, error) {
	sent, err := sentEmailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return postmark.SentEmail{}, nil
	}
	return sent, err
}

// dex:group group-id:postmark-send-prepare group-label:"Prepare send"
// dex:explanation text:"Pass validated start input to the provider send Step."
type prepareSend struct{ dex.StepDefaults }

func (prepareSend) GetStepType() string { return "PrepareSend" }
func (prepareSend) WaitFor(dex.Context, SendRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareSend) Execute(_ dex.Context, request SendRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[SendRequest](sendStepType), request), nil
}

// dex:group group-id:postmark-send-complete group-label:"Complete send"
// dex:explanation text:"Persist the accepted message identity and complete the Flow."
type completeSend struct {
	dex.StepDefaultsNoWaitFor[postmark.SendEmailResult]
}

func (completeSend) GetStepType() string { return "CompleteSend" }
func (completeSend) Execute(ctx dex.Context, result postmark.SendEmailResult) (*dex.StepDecision, error) {
	if err := sentEmailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SendFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*SendFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*SendFlow)(nil).GetDexDisplay
