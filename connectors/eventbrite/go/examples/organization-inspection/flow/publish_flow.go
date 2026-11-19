// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package organizationinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/eventbrite/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// EventPublicationFlowType is the stable example Flow identity for publishing one event.
const EventPublicationFlowType = "EventbriteEventPublication"

const publishEventStepType = "CallPublishEvent"

var publishedEventAttribute = dex.DefineAttribute[eventbrite.EventMetadata]("eventbrite-published-event")

// EventPublicationFlow publishes a draft Eventbrite event and records its public metadata.
type EventPublicationFlow struct {
	dex.FlowDefaults
	connection eventbrite.Connection
}

// NewEventPublicationFlow binds the static Eventbrite connection at registration time.
func NewEventPublicationFlow(connection eventbrite.Connection) *EventPublicationFlow {
	return &EventPublicationFlow{connection: connection}
}

// GetFlowType returns the stable event-publication Flow identity.
func (*EventPublicationFlow) GetFlowType() string { return EventPublicationFlowType }

// GetPersistenceSchema registers the published event Attribute used by the Flow.
func (*EventPublicationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{publishedEventAttribute}}
}

// GetSteps defines the start, provider mutation, and completion steps.
func (flow *EventPublicationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPublishEvent{}),
		dex.DefineStep(eventbrite.NewPublishEventStep(eventbrite.PublishEventStepConfig[eventbrite.PublishEventInput]{
			StepType: publishEventStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Eventbrite", Explanation: "Publish the selected draft event."},
			Connection:          flow.connection,
			MapToOperationInput: mapPublishEventInput,
			Published:           sdkgo.GoTo(completePublishEvent{}),
		})),
		dex.DefineStep(completePublishEvent{}),
	}
}

// GetRPCs exposes the published event to Dex Web.
func (flow *EventPublicationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:eventbrite-published-event value-type:json editable:false description:"Published event"
// GetDexSummary returns the published event for the summary view.
func (*EventPublicationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, err := readPublishedEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"eventbrite-published-event": event}}, nil
}

// dex:field attribute-key:eventbrite-published-event value-type:json editable:false description:"Published event"
// GetDexDisplay returns the published event for the detail view.
func (*EventPublicationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, err := readPublishedEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"eventbrite-published-event": event}}, nil
}

func readPublishedEvent(ctx dex.Context) (eventbrite.EventMetadata, error) {
	event, err := publishedEventAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return eventbrite.EventMetadata{}, nil
	}
	return event, err
}

func mapPublishEventInput(input eventbrite.PublishEventInput) eventbrite.PublishEventInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected event ID to the publication Step."
type startPublishEvent struct{ dex.StepDefaults }

func (startPublishEvent) GetStepType() string { return "StartPublishEvent" }
func (startPublishEvent) WaitFor(dex.Context, eventbrite.PublishEventInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPublishEvent) Execute(_ dex.Context, input eventbrite.PublishEventInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[eventbrite.PublishEventInput](publishEventStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the published event and complete the Flow."
type completePublishEvent struct {
	dex.StepDefaultsNoWaitFor[eventbrite.PublishEventResult]
}

func (completePublishEvent) GetStepType() string { return "CompletePublishEvent" }
func (completePublishEvent) Execute(ctx dex.Context, result eventbrite.PublishEventResult) (*dex.StepDecision, error) {
	if err := publishedEventAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*EventPublicationFlow)(nil)
