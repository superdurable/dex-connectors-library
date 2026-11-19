// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createtemplate demonstrates Resend draft template creation.
package createtemplate

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateTemplate"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new template settings.
type Request resend.CreateTemplateInput

var outcomeAttribute = dex.DefineAttribute[resend.TemplateReference]("resend-template-created")

// Flow creates one Resend draft template and completes with its ID.
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

// GetDexSummary returns the created template ID.
// dex:field attribute-key:resend-template-created value-type:json editable:false description:"Created template"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readTemplateOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-template-created": value}}, nil
}

// GetDexDisplay returns the created template ID.
// dex:field attribute-key:resend-template-created value-type:json editable:false description:"Created template"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readTemplateOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-template-created": value}}, nil
}

func readTemplateOutcome(ctx dex.Context) (resend.TemplateReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.TemplateReference{}, nil
	}
	return value, err
}

// GetSteps registers the template creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareTemplate{}),
		dex.DefineStep(resend.NewCreateTemplateStep(resend.CreateTemplateStepConfig[Request]{
			StepType: "CreateTemplate", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-template", GroupLabel: "Resend template", Explanation: "Create the selected Resend draft template."},
			MapToOperationInput: func(request Request) resend.CreateTemplateInput { return resend.CreateTemplateInput(request) },
			Created:             sdkgo.GoTo(completeTemplate{}),
		})),
		dex.DefineStep(completeTemplate{}),
	}
}

// dex:group group-id:resend-template-prepare group-label:"Prepare template"
// dex:explanation text:"Pass the typed template request into the provider create."
type prepareTemplate struct{ dex.StepDefaults }

func (prepareTemplate) GetStepType() string { return "PrepareTemplate" }
func (prepareTemplate) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareTemplate) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateTemplate"), request), nil
}

// dex:group group-id:resend-template-complete group-label:"Complete template creation"
// dex:explanation text:"Complete after Resend creates the template."
type completeTemplate struct {
	dex.StepDefaultsNoWaitFor[resend.CreateTemplateResult]
}

func (completeTemplate) GetStepType() string { return "CompleteTemplate" }
func (completeTemplate) Execute(ctx dex.Context, result resend.CreateTemplateResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
