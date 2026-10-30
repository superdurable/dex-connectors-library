// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package profileevent defines a runnable Klaviyo event Flow.
package profileevent

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/klaviyo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "KlaviyoProfileEvent"
	// ConnectionName is the static Dex Web connection used by this Flow.
	ConnectionName = "klaviyo"
	eventStepType  = "CreateKlaviyoEvent"
)

var outcomeAttribute = dex.DefineAttribute[klaviyo.EventAcceptance]("klaviyo-profile-event-outcome")

// Request is the Dex Web Start Flow input.
type Request struct {
	// Email identifies the profile associated with the event.
	Email string `json:"email"`
	// MetricName names the event, such as Signed Up.
	MetricName string `json:"metricName"`
}

// Flow creates one Klaviyo profile event with a stable deduplication ID.
type Flow struct {
	dex.FlowDefaults
	connection klaviyo.Connection
}

// NewFlow binds the configured Klaviyo connection.
func NewFlow(connection klaviyo.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(klaviyo.NewCreateEventStep(klaviyo.CreateEventStepConfig[Request]{
			StepType: eventStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "profile-event", GroupLabel: "Profile event", Explanation: "Submit one event for a Klaviyo profile with stable deduplication."},
			MapToOperationInput: MapToCreateEventInput,
			Accepted:            sdkgo.GoTo(accepted{}),
		})),
		dex.DefineStep(accepted{}),
	}
}

// MapToCreateEventInput maps the validated start request to the event operation.
func MapToCreateEventInput(request Request) klaviyo.CreateEventInput {
	return klaviyo.CreateEventInput{Email: request.Email, MetricName: request.MetricName}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetDexSummary returns the completed provider outcome.
// dex:field attribute-key:klaviyo-profile-event-outcome value-type:json editable:false description:"Event acceptance"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"klaviyo-profile-event-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:klaviyo-profile-event-outcome value-type:json editable:false description:"Event acceptance"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"klaviyo-profile-event-outcome": outcome}}, nil
}

func optionalOutcome(ctx dex.Context) (klaviyo.EventAcceptance, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return klaviyo.EventAcceptance{}, nil
	}
	return outcome, err
}

// dex:group group-id:profile-event group-label:"Profile event"
// dex:explanation text:"Validate the profile email and metric before calling Klaviyo."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string                             { return "StartKlaviyoProfileEvent" }
func (start) WaitFor(dex.Context, Request) (*dex.Wait, error) { return dex.SkipWaitImmediately(), nil }
func (start) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	request.Email = strings.TrimSpace(request.Email)
	request.MetricName = strings.TrimSpace(request.MetricName)
	if request.Email == "" || request.MetricName == "" {
		return dex.ForceFail("email and metricName are required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[Request](eventStepType), request), nil
}

// dex:group group-id:profile-event group-label:"Profile event"
// dex:explanation text:"Complete after Klaviyo accepts the event for asynchronous processing."
type accepted struct {
	dex.StepDefaultsNoWaitFor[klaviyo.CreateEventResult]
}

func (accepted) GetStepType() string { return "KlaviyoEventAccepted" }
func (accepted) Execute(ctx dex.Context, result klaviyo.CreateEventResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
