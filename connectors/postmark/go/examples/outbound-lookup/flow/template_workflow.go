// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TemplateFlowType identifies the runnable template-list Flow.
const TemplateFlowType = "PostmarkTemplateList"

const templateStepType = "ListTemplates"

var templatePageAttribute = dex.DefineAttribute[postmark.TemplatePage]("postmark-template-page")

// TemplateRequest is a typed template page request.
type TemplateRequest postmark.ListTemplatesInput

// TemplateFlow lists server templates and stores the result.
type TemplateFlow struct {
	dex.FlowDefaults
	connection postmark.Connection
}

// NewTemplateFlow binds the configured Postmark connection.
func NewTemplateFlow(connection postmark.Connection) *TemplateFlow {
	return &TemplateFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*TemplateFlow) GetFlowType() string { return TemplateFlowType }

// GetSteps defines preparation, provider lookup, and completion Steps.
func (flow *TemplateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareTemplateList{}),
		dex.DefineStep(postmark.NewListTemplatesStep(postmark.ListTemplatesStepConfig[TemplateRequest]{
			StepType: templateStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "postmark-templates", GroupLabel: "List templates", Explanation: "List Postmark server templates."},
			MapToOperationInput: func(request TemplateRequest) postmark.ListTemplatesInput { return postmark.ListTemplatesInput(request) },
			Listed:              sdkgo.GoTo(completeTemplateList{}),
		})),
		dex.DefineStep(completeTemplateList{}),
	}
}

// GetRPCs exposes the template page in Dex Web.
func (flow *TemplateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the stored template page.
func (*TemplateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{templatePageAttribute}}
}

// GetDexSummary returns the template page.
// dex:field attribute-key:postmark-template-page value-type:json editable:false description:"Postmark template page"
func (*TemplateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTemplatePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-template-page": page}}, nil
}

// GetDexDisplay returns the template page.
// dex:field attribute-key:postmark-template-page value-type:json editable:false description:"Postmark template page"
func (*TemplateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTemplatePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-template-page": page}}, nil
}

func readTemplatePage(ctx dex.Context) (postmark.TemplatePage, error) {
	page, err := templatePageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return postmark.TemplatePage{}, nil
	}
	return page, err
}

// dex:group group-id:postmark-template-prepare group-label:"Prepare template list"
// dex:explanation text:"Pass the requested page to the provider query."
type prepareTemplateList struct{ dex.StepDefaults }

func (prepareTemplateList) GetStepType() string { return "PrepareTemplateList" }
func (prepareTemplateList) WaitFor(dex.Context, TemplateRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareTemplateList) Execute(_ dex.Context, request TemplateRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[TemplateRequest](templateStepType), request), nil
}

// dex:group group-id:postmark-template-complete group-label:"Complete template list"
// dex:explanation text:"Persist the page and complete the Flow."
type completeTemplateList struct {
	dex.StepDefaultsNoWaitFor[postmark.ListTemplatesResult]
}

func (completeTemplateList) GetStepType() string { return "CompleteTemplateList" }
func (completeTemplateList) Execute(ctx dex.Context, result postmark.ListTemplatesResult) (*dex.StepDecision, error) {
	if err := templatePageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TemplateFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*TemplateFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*TemplateFlow)(nil).GetDexDisplay
