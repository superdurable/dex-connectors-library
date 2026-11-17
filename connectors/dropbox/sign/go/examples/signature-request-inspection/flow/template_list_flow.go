// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package signaturerequestinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TemplateListFlowType is the stable template page example identity.
const TemplateListFlowType = "DropboxSignTemplateList"

var templateListAttribute = dex.DefineAttribute[dropboxsign.TemplatesPage]("dropbox-sign-template-list")

// TemplateListFlow reads one page of templates and signer roles.
type TemplateListFlow struct {
	dex.FlowDefaults
	connection dropboxsign.Connection
}

// NewTemplateListFlow binds the provider connection at registration time.
func NewTemplateListFlow(connection dropboxsign.Connection) *TemplateListFlow {
	return &TemplateListFlow{connection: connection}
}

// GetFlowType preserves the template list Flow identity.
func (*TemplateListFlow) GetFlowType() string { return TemplateListFlowType }

// GetPersistenceSchema registers the template page Attribute.
func (*TemplateListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{templateListAttribute}}
}

// GetSteps registers the operation-specific template list Step.
func (flow *TemplateListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTemplateList{}),
		dex.DefineStep(dropboxsign.NewListTemplatesStep(dropboxsign.ListTemplatesStepConfig[dropboxsign.ListTemplatesInput]{
			StepType: "CallListTemplates", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox Sign", Explanation: "Read one page of accessible templates."},
			Connection:          flow.connection,
			MapToOperationInput: mapTemplateListInput,
			Listed:              sdkgo.GoTo(completeTemplateList{}),
		})),
		dex.DefineStep(completeTemplateList{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *TemplateListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-sign-template-list value-type:json editable:false description:"Template page"
// GetDexSummary shows the last template page in Dex Web.
func (*TemplateListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTemplateList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-template-list": page}}, nil
}

// dex:field attribute-key:dropbox-sign-template-list value-type:json editable:false description:"Template page"
// GetDexDisplay shows the last template page in Dex Web.
func (*TemplateListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTemplateList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-template-list": page}}, nil
}

func readTemplateList(ctx dex.Context) (dropboxsign.TemplatesPage, error) {
	page, err := templateListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropboxsign.TemplatesPage{}, nil
	}
	return page, err
}

func mapTemplateListInput(input dropboxsign.ListTemplatesInput) dropboxsign.ListTemplatesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the template page request to Dropbox Sign."
type startTemplateList struct{ dex.StepDefaults }

func (startTemplateList) GetStepType() string { return "StartTemplateList" }
func (startTemplateList) WaitFor(dex.Context, dropboxsign.ListTemplatesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTemplateList) Execute(_ dex.Context, input dropboxsign.ListTemplatesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropboxsign.ListTemplatesInput]("CallListTemplates"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with one page of template identities and signer roles."
type completeTemplateList struct {
	dex.StepDefaultsNoWaitFor[dropboxsign.ListTemplatesResult]
}

func (completeTemplateList) GetStepType() string { return "CompleteTemplateList" }
func (completeTemplateList) Execute(ctx dex.Context, result dropboxsign.ListTemplatesResult) (*dex.StepDecision, error) {
	if err := templateListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TemplateListFlow)(nil)
