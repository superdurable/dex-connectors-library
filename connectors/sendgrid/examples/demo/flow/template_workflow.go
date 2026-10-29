// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sendgrid"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// TemplateFlowType is the stable example identity for template listing.
	TemplateFlowType          = "SendGridTemplateListDemo"
	templateOperationStepType = "CallListTemplates"
)

var templateOutcomeAttribute = dex.DefineAttribute[sendgrid.TemplatesOutput]("sendgrid-template-demo-outcome")

// TemplateFlow lists one page of transactional templates.
type TemplateFlow struct {
	dex.FlowDefaults
	connection sendgrid.Connection
}

// NewTemplateFlow binds the provider connection at registration time.
func NewTemplateFlow(connection sendgrid.Connection) *TemplateFlow {
	return &TemplateFlow{connection: connection}
}

// GetFlowType returns the stable template-list Flow identity.
func (*TemplateFlow) GetFlowType() string { return TemplateFlowType }

// GetPersistenceSchema registers the template page shown by Dex Web RPCs.
func (*TemplateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{templateOutcomeAttribute}}
}

// GetSteps registers the template request, provider call, and completion Steps.
func (flow *TemplateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTemplateRequest{}),
		dex.DefineStep(sendgrid.NewListTemplatesStep(sendgrid.ListTemplatesStepConfig[sendgrid.ListTemplatesInput]{
			StepType: templateOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SendGrid", Explanation: "List one page of transactional templates."},
			Connection:          flow.connection,
			MapToOperationInput: MapToTemplateOperationInput,
			Listed:              sdkgo.GoTo(completeTemplateResult{}),
		})),
		dex.DefineStep(completeTemplateResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *TemplateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sendgrid-template-demo-outcome value-type:json editable:false description:"Template page"
// GetDexSummary shows the last template page in Dex Web.
func (*TemplateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTemplateOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-template-demo-outcome": outcome}}, nil
}

// dex:field attribute-key:sendgrid-template-demo-outcome value-type:json editable:false description:"Template page"
// GetDexDisplay shows the last template page in Dex Web.
func (*TemplateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTemplateOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendgrid-template-demo-outcome": outcome}}, nil
}

func readTemplateOutcome(ctx dex.Context) (sendgrid.TemplatesOutput, error) {
	outcome, err := templateOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sendgrid.TemplatesOutput{}, nil
	}
	return outcome, err
}

// MapToTemplateOperationInput preserves the typed start input for the provider call.
func MapToTemplateOperationInput(input sendgrid.ListTemplatesInput) sendgrid.ListTemplatesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the template page selection to the provider Step."
type startTemplateRequest struct{ dex.StepDefaults }

func (startTemplateRequest) GetStepType() string { return "StartListTemplates" }
func (startTemplateRequest) WaitFor(dex.Context, sendgrid.ListTemplatesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTemplateRequest) Execute(_ dex.Context, input sendgrid.ListTemplatesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sendgrid.ListTemplatesInput](templateOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the returned template page."
type completeTemplateResult struct {
	dex.StepDefaultsNoWaitFor[sendgrid.ListTemplatesResult]
}

func (completeTemplateResult) GetStepType() string { return "CompleteListTemplates" }
func (completeTemplateResult) Execute(ctx dex.Context, result sendgrid.ListTemplatesResult) (*dex.StepDecision, error) {
	if err := templateOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TemplateFlow)(nil)
