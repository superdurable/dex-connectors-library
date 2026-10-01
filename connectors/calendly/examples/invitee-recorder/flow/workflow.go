// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package inviteerecorder starts one Flow per booked Calendly invitee from the inviteeEventReceived Trigger,
// reads the invitee's scheduled event back with getScheduledEvent, and records both.
package inviteerecorder

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "CalendlyInviteeRecorder"
	// ConnectionName is the static Dex Web connection that verifies webhooks and reads scheduled events.
	ConnectionName = "calendly-scheduling"
	// InviteeCreatedTriggerBinding is the inviteeEventReceived binding whose invitee.created events start
	// this Flow.
	InviteeCreatedTriggerBinding = "invitee-created"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered webhook maps to one Flow.
	FlowIDPrefix = "calendly-"

	readScheduledEventStepType = "ReadScheduledEvent"
)

var (
	bookingAttribute        = dex.DefineAttribute[Booking]("calendly-booking")
	scheduledEventAttribute = dex.DefineAttribute[RecordedScheduledEvent]("calendly-scheduled-event")
)

// Booking is the Flow's start input: one verified invitee.created webhook.
type Booking struct {
	// EventID is the Trigger event ID, such as invitee.created:<event id>:<invitee id>.
	EventID string `json:"eventId"`
	// InviteeURI is the invitee's stable Calendly URI.
	InviteeURI string `json:"inviteeUri"`
	// InviteeEmail is the invitee's email address.
	InviteeEmail string `json:"inviteeEmail"`
	// InviteeName is the invitee's name.
	InviteeName string `json:"inviteeName,omitempty"`
	// ScheduledEventURI is the scheduled event the invitee booked.
	ScheduledEventURI string `json:"scheduledEventUri"`
	// BookedAt is when Calendly created the webhook event.
	BookedAt time.Time `json:"bookedAt"`
}

// RecordedScheduledEvent is the getScheduledEvent outcome the Flow stores; it completes the Flow when found.
type RecordedScheduledEvent struct {
	// Branch is the getScheduledEvent branch: found, notFound, providerRejected, invalidResponse, or defect.
	Branch sdkgo.BranchID `json:"branch"`
	// ScheduledEvent is the event Calendly returned on the found branch.
	ScheduledEvent *calendly.ScheduledEvent `json:"scheduledEvent,omitempty"`
	// FailureKind is the safe failure category of a branch other than found.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// FailureMessage is the safe failure message, which never holds tokens or Calendly response text.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// Flow records one booked invitee and its scheduled event.
type Flow struct {
	dex.FlowDefaults
	connection calendly.Connection
}

// NewFlow binds the Calendly Connection that reads scheduled events.
func NewFlow(connection calendly.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the record, read, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordBooking{}),
		dex.DefineStep(calendly.NewGetScheduledEventStep(calendly.GetScheduledEventStepConfig[Booking]{
			StepType: readScheduledEventStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "calendly", GroupLabel: "Calendly",
				Explanation: "Read the booked scheduled event back from Calendly by its URI.",
			},
			Connection:          flow.connection,
			MapToOperationInput: MapToGetScheduledEventInput,
			Found:               sdkgo.GoTo(recordScheduledEvent{}),
			NotFound:            sdkgo.GoTo(recordReadFailure{}),
			ProviderRejected:    sdkgo.GoTo(recordReadFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordReadFailure{}),
			Defect:              sdkgo.GoTo(recordReadFailure{}),
		})),
		dex.DefineStep(recordScheduledEvent{}),
		dex.DefineStep(recordReadFailure{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the booking and scheduled-event Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{bookingAttribute, scheduledEventAttribute}}
}

// GetConnectorTriggerBindings declares the inviteeEventReceived binding that starts this Flow.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		calendly.DefineInviteeEventReceivedTriggerBinding(calendly.InviteeEventReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: InviteeCreatedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "eventType", UnitID: calendly.UIUnitEventTypePicker, Label: "Event type",
				Description: "Select the Calendly event type whose new bookings start this Flow; the picker lists the connected user's event types and stores the event type URI. Leave it empty to record bookings of every event type.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: calendly.UIEventTypePickerPortEventTypeURI, JSONPointer: "/eventTypeUri"}},
			}}},
		}),
	}
}

// GetDexSummary returns the booking and scheduled event for the Dex Web run list.
//
// dex:field attribute-key:calendly-booking value-type:json editable:false description:"Booked invitee"
// dex:field attribute-key:calendly-scheduled-event value-type:json editable:false description:"Scheduled event read back from Calendly"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	booking, recorded, err := bookingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"calendly-booking": booking, "calendly-scheduled-event": recorded}}, nil
}

// GetDexDisplay returns the booking and scheduled event for the Dex Web run detail.
//
// dex:field attribute-key:calendly-booking value-type:json editable:false description:"Invitee URI, email, and scheduled event URI"
// dex:field attribute-key:calendly-scheduled-event value-type:json editable:false description:"getScheduledEvent branch and event"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	booking, recorded, err := bookingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"calendly-booking": booking, "calendly-scheduled-event": recorded}}, nil
}

// AcceptBooking is the application's admission rule: only an active invitee.created event starts a Flow.
func AcceptBooking(event sdkgo.TriggerEvent[calendly.InviteeEvent]) bool {
	return event.Payload.Event == calendly.WebhookEventInviteeCreated && event.Payload.Invitee.Status == calendly.ScheduledEventStatusActive
}

// ResolveFlowID derives the Flow ID from the event ID, so every redelivery maps to one Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[calendly.InviteeEvent]) string {
	return FlowIDPrefix + strings.ReplaceAll(event.ID, ":", "-")
}

// MapToFlowInput copies the verified booking into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[calendly.InviteeEvent]) Booking {
	invitee := event.Payload.Invitee
	return Booking{
		EventID: event.ID, InviteeURI: invitee.URI, InviteeEmail: invitee.Email, InviteeName: invitee.Name,
		ScheduledEventURI: invitee.ScheduledEventURI, BookedAt: event.OccurredAt,
	}
}

// MapToGetScheduledEventInput reads the event the invitee booked.
func MapToGetScheduledEventInput(booking Booking) calendly.GetScheduledEventInput {
	return calendly.GetScheduledEventInput{ScheduledEventURI: booking.ScheduledEventURI}
}

func bookingInspection(ctx dex.Context) (Booking, RecordedScheduledEvent, error) {
	booking, err := optionalAttribute(ctx, bookingAttribute)
	if err != nil {
		return Booking{}, RecordedScheduledEvent{}, err
	}
	recorded, err := optionalAttribute(ctx, scheduledEventAttribute)
	if err != nil {
		return Booking{}, RecordedScheduledEvent{}, err
	}
	return booking, recorded, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:booking group-label:"Booking"
// dex:explanation text:"Persist the verified booking before reading its scheduled event."
type recordBooking struct {
	dex.StepDefaultsNoWaitFor[Booking]
}

func (recordBooking) GetStepType() string { return "RecordBooking" }

func (recordBooking) Execute(ctx dex.Context, booking Booking) (*dex.StepDecision, error) {
	if booking.EventID == "" || booking.ScheduledEventURI == "" {
		return dex.ForceFail("a booking requires its event ID and scheduled event URI"), nil
	}
	if err := bookingAttribute.Set(ctx, booking); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Booking](readScheduledEventStepType), booking), nil
}

// dex:group group-id:calendly group-label:"Calendly"
// dex:explanation text:"Persist the scheduled event Calendly returned and complete the Flow."
type recordScheduledEvent struct {
	dex.StepDefaultsNoWaitFor[calendly.GetScheduledEventResult]
}

func (recordScheduledEvent) GetStepType() string { return "RecordScheduledEvent" }

func (recordScheduledEvent) Execute(ctx dex.Context, result calendly.GetScheduledEventResult) (*dex.StepDecision, error) {
	event := result.Value
	recorded := RecordedScheduledEvent{Branch: result.Branch, ScheduledEvent: &event}
	if err := scheduledEventAttribute.Set(ctx, recorded); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(recorded), nil
}

// dex:group group-id:calendly group-label:"Calendly"
// dex:explanation text:"Persist the notFound, providerRejected, invalidResponse, or defect outcome with its safe message and fail the Flow."
type recordReadFailure struct {
	dex.StepDefaultsNoWaitFor[calendly.GetScheduledEventResult]
}

func (recordReadFailure) GetStepType() string { return "RecordReadFailure" }

func (recordReadFailure) Execute(ctx dex.Context, result calendly.GetScheduledEventResult) (*dex.StepDecision, error) {
	recorded := RecordedScheduledEvent{Branch: result.Branch}
	message := "reading the scheduled event selected " + string(result.Branch)
	if result.Failure != nil {
		recorded.FailureKind, recorded.FailureMessage = result.Failure.Kind, result.Failure.Message
		message += ": " + result.Failure.Message
	}
	if err := scheduledEventAttribute.Set(ctx, recorded); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
