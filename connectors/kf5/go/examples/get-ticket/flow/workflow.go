// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package ticketread runs KF5 getTicket as one Dex Flow.
package ticketread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kf5/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "KF5TicketRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "kf5"
	operationStepType = "CallGetTicket"
)

var ticketOutcomeAttribute = dex.DefineAttribute[kf5.GetTicketOutput]("kf5-get-ticket-outcome")

// Flow runs GetTicket once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection kf5.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection kf5.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ticketOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(kf5.NewGetTicketStep(kf5.GetTicketStepConfig[kf5.GetTicketInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "KF5", Explanation: "Read one KF5 ticket status without its content or requester."},
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

// dex:field attribute-key:kf5-get-ticket-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetTicketOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kf5-get-ticket-outcome": outcome}}, nil
}

// dex:field attribute-key:kf5-get-ticket-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetTicketOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kf5-get-ticket-outcome": outcome}}, nil
}

func readGetTicketOutcome(ctx dex.Context) (kf5.GetTicketOutput, error) {
	outcome, err := ticketOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kf5.GetTicketOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input kf5.GetTicketInput) kf5.GetTicketInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the ticket ID to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTicket" }
func (startRequest) WaitFor(dex.Context, kf5.GetTicketInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input kf5.GetTicketInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kf5.GetTicketInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the ticket status."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[kf5.GetTicketResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTicket" }
func (completeResult) Execute(ctx dex.Context, result kf5.GetTicketResult) (*dex.StepDecision, error) {
	if err := ticketOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
