// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactimport demonstrates Resend contact import retrieval.
package contactimport

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendGetContactImport"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the import ID.
type Request resend.GetContactImportInput

var outcomeAttribute = dex.DefineAttribute[resend.ContactImport]("resend-contact-import")

// Flow retrieves one Resend contact import status.
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

// GetPersistenceSchema registers the metrics result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the contact import.
// dex:field attribute-key:resend-contact-import value-type:json editable:false description:"Contact import"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactImportOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-contact-import": value}}, nil
}

// GetDexDisplay returns the contact import.
// dex:field attribute-key:resend-contact-import value-type:json editable:false description:"Contact import"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readContactImportOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-contact-import": value}}, nil
}

func readContactImportOutcome(ctx dex.Context) (resend.ContactImport, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.ContactImport{}, nil
	}
	return value, err
}

// GetSteps registers the contact import query path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareContactImport{}),
		dex.DefineStep(resend.NewGetContactImportStep(resend.GetContactImportStepConfig[Request]{
			StepType: "GetContactImport", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-contact-import", GroupLabel: "Resend contact import", Explanation: "Retrieve the selected contact import's progress."},
			MapToOperationInput: func(request Request) resend.GetContactImportInput { return resend.GetContactImportInput(request) },
			Found:               sdkgo.GoTo(completeContactImport{}),
		})),
		dex.DefineStep(completeContactImport{}),
	}
}

// dex:group group-id:resend-contact-import-prepare group-label:"Prepare contact import"
// dex:explanation text:"Pass the selected import ID into the provider query."
type prepareContactImport struct{ dex.StepDefaults }

func (prepareContactImport) GetStepType() string { return "PrepareContactImport" }
func (prepareContactImport) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareContactImport) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("GetContactImport"), request), nil
}

// dex:group group-id:resend-contact-import-complete group-label:"Complete contact import query"
// dex:explanation text:"Complete after Resend returns import progress."
type completeContactImport struct {
	dex.StepDefaultsNoWaitFor[resend.GetContactImportResult]
}

func (completeContactImport) GetStepType() string { return "CompleteContactImport" }
func (completeContactImport) Execute(ctx dex.Context, result resend.GetContactImportResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
