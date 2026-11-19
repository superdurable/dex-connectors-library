// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TemplateSendFlowType is the stable identity of the example email Flow.
const TemplateSendFlowType = "PostmarkSendEmailWithTemplate"

const templateSendStepType = "SendEmail"

var templateSentEmailAttribute = dex.DefineAttribute[postmark.SentEmail]("postmark-template-sent-email")

// TemplateSendRequest is the typed start input for one transactional email.
type TemplateSendRequest postmark.SendEmailWithTemplateInput

// TemplateSendFlow sends one transactional email and stores its Postmark identity.
type TemplateSendFlow struct {
	dex.FlowDefaults
	connection postmark.Connection
}

// NewTemplateSendFlow binds the configured Postmark connection.
func NewTemplateSendFlow(connection postmark.Connection) *TemplateSendFlow {
	return &TemplateSendFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*TemplateSendFlow) GetFlowType() string { return TemplateSendFlowType }

// GetSteps defines the prepare, provider send, and completion Steps.
func (flow *TemplateSendFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareTemplateSend{}),
		dex.DefineStep(postmark.NewSendEmailWithTemplateStep(postmark.SendEmailWithTemplateStepConfig[TemplateSendRequest]{
			StepType: templateSendStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "postmark-template-send", GroupLabel: "Send email", Explanation: "Send a transactional email using a Postmark template."},
			MapToOperationInput: func(request TemplateSendRequest) postmark.SendEmailWithTemplateInput {
				return postmark.SendEmailWithTemplateInput(request)
			},
			Sent: sdkgo.GoTo(completeTemplateSend{}),
		})),
		dex.DefineStep(completeTemplateSend{}),
	}
}

// GetRPCs exposes the accepted message in Dex Web.
func (flow *TemplateSendFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the stored provider result.
func (*TemplateSendFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{templateSentEmailAttribute}}
}

// GetDexSummary returns the accepted message metadata.
// dex:field attribute-key:postmark-template-sent-email value-type:json editable:false description:"Accepted Postmark message"
func (*TemplateSendFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sent, err := readTemplateSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-template-sent-email": sent}}, nil
}

// GetDexDisplay returns the accepted message metadata.
// dex:field attribute-key:postmark-template-sent-email value-type:json editable:false description:"Accepted Postmark message"
func (*TemplateSendFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sent, err := readTemplateSentEmail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-template-sent-email": sent}}, nil
}

func readTemplateSentEmail(ctx dex.Context) (postmark.SentEmail, error) {
	sent, err := templateSentEmailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return postmark.SentEmail{}, nil
	}
	return sent, err
}

// dex:group group-id:postmark-template-send-prepare group-label:"Prepare send"
// dex:explanation text:"Pass validated start input to the provider send Step."
type prepareTemplateSend struct{ dex.StepDefaults }

func (prepareTemplateSend) GetStepType() string { return "PrepareTemplateSend" }
func (prepareTemplateSend) WaitFor(dex.Context, TemplateSendRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareTemplateSend) Execute(_ dex.Context, request TemplateSendRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[TemplateSendRequest](templateSendStepType), request), nil
}

// dex:group group-id:postmark-template-send-complete group-label:"Complete send"
// dex:explanation text:"Persist the accepted message identity and complete the Flow."
type completeTemplateSend struct {
	dex.StepDefaultsNoWaitFor[postmark.SendEmailWithTemplateResult]
}

func (completeTemplateSend) GetStepType() string { return "CompleteTemplateSend" }
func (completeTemplateSend) Execute(ctx dex.Context, result postmark.SendEmailWithTemplateResult) (*dex.StepDecision, error) {
	if err := templateSentEmailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TemplateSendFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*TemplateSendFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*TemplateSendFlow)(nil).GetDexDisplay
