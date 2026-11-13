// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package qrcodecatalog runs Caoliao listQRCodes as one Dex Flow.
package qrcodecatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/caoliao/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "CaoliaoQRCodeCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "caoliao"
	operationStepType = "CallListQRCodes"
)

var qrcodeCatalogOutcomeAttribute = dex.DefineAttribute[caoliao.ListQRCodesOutput]("caoliao-qrcode-directory-outcome")

// Flow runs ListQRCodes once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection caoliao.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection caoliao.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{qrcodeCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(caoliao.NewListQRCodesStep(caoliao.ListQRCodesStepConfig[caoliao.ListQRCodesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Caoliao", Explanation: "List QR-code names and coding identifiers for one organization page."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:caoliao-qrcode-directory-outcome value-type:json editable:false description:"QRCode catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readQRCodeCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"caoliao-qrcode-directory-outcome": outcome}}, nil
}

// dex:field attribute-key:caoliao-qrcode-directory-outcome value-type:json editable:false description:"QRCode catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readQRCodeCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"caoliao-qrcode-directory-outcome": outcome}}, nil
}

func readQRCodeCatalogOutcome(ctx dex.Context) (caoliao.ListQRCodesOutput, error) {
	outcome, err := qrcodeCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return caoliao.ListQRCodesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input caoliao.ListQRCodesInput) caoliao.ListQRCodesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListQRCodes" }
func (startRequest) WaitFor(dex.Context, caoliao.ListQRCodesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input caoliao.ListQRCodesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[caoliao.ListQRCodesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[caoliao.ListQRCodesResult]
}

func (completeResult) GetStepType() string { return "CompleteListQRCodes" }
func (completeResult) Execute(ctx dex.Context, result caoliao.ListQRCodesResult) (*dex.StepDecision, error) {
	if err := qrcodeCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
