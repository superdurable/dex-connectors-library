// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package signaturerequestinspection runs Dropbox Sign getSignatureRequest as one Dex Flow.
package signaturerequestinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dropbox/sign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DropboxSignSignatureRequestInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "dropbox-sign"
	operationStepType = "CallGetSignatureRequest"
)

var signatureRequestAttribute = dex.DefineAttribute[dropboxsign.SignatureRequestStatus]("dropbox-sign-signature-request")

// Flow runs GetSignatureRequest once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dropboxsign.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dropboxsign.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{signatureRequestAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dropboxsign.NewGetSignatureRequestStep(dropboxsign.GetSignatureRequestStepConfig[dropboxsign.GetSignatureRequestInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dropbox Sign", Explanation: "Read a signature request from Dropbox Sign."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dropbox-sign-signature-request value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSignatureRequest(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-signature-request": outcome}}, nil
}

// dex:field attribute-key:dropbox-sign-signature-request value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSignatureRequest(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dropbox-sign-signature-request": outcome}}, nil
}

func readSignatureRequest(ctx dex.Context) (dropboxsign.SignatureRequestStatus, error) {
	outcome, err := signatureRequestAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dropboxsign.SignatureRequestStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input dropboxsign.GetSignatureRequestInput) dropboxsign.GetSignatureRequestInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetSignatureRequest" }
func (startRequest) WaitFor(dex.Context, dropboxsign.GetSignatureRequestInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dropboxsign.GetSignatureRequestInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dropboxsign.GetSignatureRequestInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dropboxsign.GetSignatureRequestResult]
}

func (completeResult) GetStepType() string { return "CompleteGetSignatureRequest" }
func (completeResult) Execute(ctx dex.Context, result dropboxsign.GetSignatureRequestResult) (*dex.StepDecision, error) {
	if err := signatureRequestAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
