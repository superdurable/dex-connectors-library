// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package profileevent defines a runnable Klaviyo event Flow.
package profileevent

import (
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/klaviyo"
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

func (*Flow) GetFlowType() string                         { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

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
func (accepted) Execute(_ dex.Context, result klaviyo.CreateEventResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
