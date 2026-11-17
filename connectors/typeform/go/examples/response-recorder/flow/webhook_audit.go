// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package responserecorder

import (
	"github.com/superdurable/dex-connectors-library/connectors/typeform/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// WebhookAuditFlowType identifies the Flow that inspects one form's webhooks.
const WebhookAuditFlowType = "TypeformWebhookAudit"

var webhookAuditAttribute = dex.DefineAttribute[typeform.WebhookList]("typeform-webhooks")

// WebhookAuditInput identifies the form whose registered webhooks are inspected.
type WebhookAuditInput struct {
	// FormID is the form ID from its Typeform URL.
	FormID string `json:"formId"`
}

// WebhookAuditFlow lists the form's webhooks without persisting signing secrets.
type WebhookAuditFlow struct {
	dex.FlowDefaults
	connection typeform.Connection
}

// NewWebhookAuditFlow binds the Typeform connection to the audit Flow.
func NewWebhookAuditFlow(connection typeform.Connection) *WebhookAuditFlow {
	return &WebhookAuditFlow{connection: connection}
}

// GetFlowType returns WebhookAuditFlowType.
func (*WebhookAuditFlow) GetFlowType() string { return WebhookAuditFlowType }

// GetPersistenceSchema registers the webhook metadata Attribute.
func (*WebhookAuditFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{webhookAuditAttribute}}
}

// GetRPCs exposes the webhook metadata in Dex Web.
func (flow *WebhookAuditFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary reads the secret-free webhook metadata for the run list.
// dex:field attribute-key:typeform-webhooks value-type:json editable:false description:"Form webhook metadata without signing secrets"
func (*WebhookAuditFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	webhooks, err := optionalAttribute(ctx, webhookAuditAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"typeform-webhooks": webhooks}}, nil
}

// GetDexDisplay reads the secret-free webhook metadata for run detail.
// dex:field attribute-key:typeform-webhooks value-type:json editable:false description:"Registered form webhooks without signing secrets"
func (*WebhookAuditFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	webhooks, err := optionalAttribute(ctx, webhookAuditAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"typeform-webhooks": webhooks}}, nil
}

// GetSteps declares the start, provider read, and completion Steps.
func (flow *WebhookAuditFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startWebhookAudit{}),
		dex.DefineStep(typeform.NewListWebhooksStep(typeform.ListWebhooksStepConfig[WebhookAuditInput]{
			StepType: "ListWebhooks", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "typeform", GroupLabel: "Typeform", Explanation: "Read the form's registered webhooks without their signing secrets."},
			Connection:  flow.connection,
			MapToOperationInput: func(input WebhookAuditInput) typeform.ListWebhooksInput {
				return typeform.ListWebhooksInput{FormID: input.FormID}
			},
			Listed: sdkgo.GoTo(completeWebhookAudit{}),
		})),
		dex.DefineStep(completeWebhookAudit{}),
	}
}

// dex:group group-id:typeform group-label:"Typeform"
// dex:explanation text:"Start an audit of the selected Typeform form's webhooks."
type startWebhookAudit struct {
	dex.StepDefaultsNoWaitFor[WebhookAuditInput]
}

func (startWebhookAudit) GetStepType() string { return "StartWebhookAudit" }

func (startWebhookAudit) Execute(_ dex.Context, input WebhookAuditInput) (*dex.StepDecision, error) {
	if input.FormID == "" {
		return dex.ForceFail("a Typeform form ID is required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[WebhookAuditInput]("ListWebhooks"), input), nil
}

// dex:group group-id:typeform group-label:"Typeform"
// dex:explanation text:"Complete with the form's webhook metadata after the Typeform read succeeds."
type completeWebhookAudit struct {
	dex.StepDefaultsNoWaitFor[typeform.ListWebhooksResult]
}

func (completeWebhookAudit) GetStepType() string { return "CompleteWebhookAudit" }

func (completeWebhookAudit) Execute(ctx dex.Context, result typeform.ListWebhooksResult) (*dex.StepDecision, error) {
	if err := webhookAuditAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*WebhookAuditFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*WebhookAuditFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*WebhookAuditFlow)(nil).GetDexDisplay
