// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createsuppression demonstrates Resend draft suppression creation.
package createsuppression

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateSuppression"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new suppression settings.
type Request resend.CreateSuppressionInput

var outcomeAttribute = dex.DefineAttribute[resend.SuppressionReference]("resend-suppression-created")

// Flow creates one Resend draft suppression and completes with its ID.
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

// GetDexSummary returns the created suppression ID.
// dex:field attribute-key:resend-suppression-created value-type:json editable:false description:"Created suppression"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readSuppressionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-suppression-created": value}}, nil
}

// GetDexDisplay returns the created suppression ID.
// dex:field attribute-key:resend-suppression-created value-type:json editable:false description:"Created suppression"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readSuppressionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-suppression-created": value}}, nil
}

func readSuppressionOutcome(ctx dex.Context) (resend.SuppressionReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.SuppressionReference{}, nil
	}
	return value, err
}

// GetSteps registers the suppression creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareSuppression{}),
		dex.DefineStep(resend.NewCreateSuppressionStep(resend.CreateSuppressionStepConfig[Request]{
			StepType: "CreateSuppression", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-suppression", GroupLabel: "Resend suppression", Explanation: "Create the selected Resend draft suppression."},
			MapToOperationInput: func(request Request) resend.CreateSuppressionInput { return resend.CreateSuppressionInput(request) },
			Created:             sdkgo.GoTo(completeSuppression{}),
		})),
		dex.DefineStep(completeSuppression{}),
	}
}

// dex:group group-id:resend-suppression-prepare group-label:"Prepare suppression"
// dex:explanation text:"Pass the typed suppression request into the provider create."
type prepareSuppression struct{ dex.StepDefaults }

func (prepareSuppression) GetStepType() string { return "PrepareSuppression" }
func (prepareSuppression) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareSuppression) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateSuppression"), request), nil
}

// dex:group group-id:resend-suppression-complete group-label:"Complete suppression creation"
// dex:explanation text:"Complete after Resend creates the suppression."
type completeSuppression struct {
	dex.StepDefaultsNoWaitFor[resend.CreateSuppressionResult]
}

func (completeSuppression) GetStepType() string { return "CompleteSuppression" }
func (completeSuppression) Execute(ctx dex.Context, result resend.CreateSuppressionResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
