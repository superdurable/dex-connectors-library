// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package welcomeemail demonstrates one Resend operation in a Dex Flow.
package welcomeemail

import (
	"github.com/superdurable/dex-connectors-library/connectors/resend"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "ResendWelcomeEmail"
	// ConnectionName is the static Dex Web connection for this example.
	ConnectionName    = "resend"
	operationStepType = "SendEmail"
)

// Request is the typed start input for this example.
type Request resend.SendEmailInput

// Flow runs one Resend request and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a configured Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }
func (*Flow) GetFlowType() string                { return FlowType }
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareRequest{}),
		dex.DefineStep(resend.NewSendEmailStep(resend.SendEmailStepConfig[Request]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend", GroupLabel: "Resend", Explanation: "Call the Resend SendEmail endpoint."},
			MapToOperationInput: func(request Request) resend.SendEmailInput { return resend.SendEmailInput(request) },
			Sent:                sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// dex:group group-id:resend-example group-label:"Resend example"
// dex:explanation text:"Persist the typed start input before the provider operation."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareRequest" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](operationStepType), request), nil
}

// dex:group group-id:resend-example group-label:"Resend example"
// dex:explanation text:"Complete the Flow with the provider result."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[resend.SendEmailResult]
}

func (completeRequest) GetStepType() string { return "CompleteRequest" }
func (completeRequest) Execute(_ dex.Context, result resend.SendEmailResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
