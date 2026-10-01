// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package formsubmission starts one Flow per verified form submission from the webhook connector's
// requestReceived Trigger, records it, and forwards it to a downstream receiver with sendEvent.
package formsubmission

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "WebhookFormSubmission"
	// ConnectionName is the static Dex Web connection that verifies submissions and forwards them.
	ConnectionName = "webhook-form"
	// SubmissionTriggerBinding is the requestReceived binding whose events start this Flow.
	SubmissionTriggerBinding = "form-submission-received"
	// FlowIDPrefix precedes the event ID in every Flow ID, so a redelivered submission maps to one Flow.
	FlowIDPrefix = "webhook-form-submission-"
	// ForwardedEventType is the type field of the event that ForwardSubmission sends.
	ForwardedEventType = "form.submitted"

	forwardSubmissionStepType = "ForwardSubmission"
)

var (
	submissionAttribute = dex.DefineAttribute[Submission]("webhook-form-submission")
	forwardingAttribute = dex.DefineAttribute[ForwardingOutcome]("webhook-form-forwarding")
)

// Submission is the Flow's start input: one verified webhook request.
type Submission struct {
	// EventID is the webhook connector's stable event ID.
	EventID string `json:"eventId"`
	// ContentType is application/json or application/x-www-form-urlencoded.
	ContentType string `json:"contentType"`
	// ReceivedAt is when the webhook endpoint read the request.
	ReceivedAt time.Time `json:"receivedAt"`
	// JSONBody is the body of a JSON submission.
	JSONBody json.RawMessage `json:"jsonBody,omitempty"`
	// FormBody is the decoded body of a form-encoded submission.
	FormBody map[string][]string `json:"formBody,omitempty"`
}

// ForwardedEvent is the JSON payload that ForwardSubmission sends to the connection's deliveryUrl.
type ForwardedEvent struct {
	// Type is ForwardedEventType.
	Type string `json:"type"`
	// SubmissionID is the submission's event ID, so the receiver can correlate it with this Flow.
	SubmissionID string `json:"submissionId"`
	// ReceivedAt is when the webhook endpoint read the submission.
	ReceivedAt time.Time `json:"receivedAt"`
	// JSONBody is the body of a JSON submission.
	JSONBody json.RawMessage `json:"jsonBody,omitempty"`
	// FormBody is the decoded body of a form-encoded submission.
	FormBody map[string][]string `json:"formBody,omitempty"`
}

// ForwardingOutcome records the sendEvent branch; it completes the Flow when delivered.
type ForwardingOutcome struct {
	// Branch is the sendEvent branch: delivered, rejected, uncertain, or defect.
	Branch sdkgo.BranchID `json:"branch"`
	// WebhookID is the webhook-id the receiver saw, for reconciling an uncertain outcome.
	WebhookID string `json:"webhookId"`
	// StatusCode is the receiver's HTTP status, or zero when none arrived.
	StatusCode int `json:"statusCode,omitempty"`
	// FailureKind is the safe failure category of a branch other than delivered.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// FailureMessage is the safe failure message, which never holds secrets or response bodies.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// Flow records and forwards one form submission.
type Flow struct {
	dex.FlowDefaults
	connection webhook.Connection
}

// NewFlow binds the webhook Connection whose deliveryUrl receives forwarded submissions.
func NewFlow(connection webhook.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the record, forward, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSubmission{}),
		dex.DefineStep(webhook.NewSendEventStep(webhook.SendEventStepConfig[Submission]{
			StepType: forwardSubmissionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "forwarding", GroupLabel: "Forwarding",
				Explanation: "Send the submission to the connection's deliveryUrl, signed with Standard Webhooks and a stable webhook-id.",
			},
			Connection:          flow.connection,
			MapToOperationInput: MapToForwardedEvent,
			Delivered:           sdkgo.GoTo(recordForwarded{}),
			Rejected:            sdkgo.GoTo(recordForwardingFailure{}),
			Uncertain:           sdkgo.GoTo(recordForwardingFailure{}),
			Defect:              sdkgo.GoTo(recordForwardingFailure{}),
		})),
		dex.DefineStep(recordForwarded{}),
		dex.DefineStep(recordForwardingFailure{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the submission and forwarding Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{submissionAttribute, forwardingAttribute}}
}

// GetConnectorTriggerBindings declares the requestReceived binding that starts this Flow.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		webhook.DefineRequestReceivedTriggerBinding(webhook.RequestReceivedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: SubmissionTriggerBinding,
		}),
	}
}

// GetDexSummary returns the submission and forwarding outcome for the Dex Web run list.
//
// dex:field attribute-key:webhook-form-submission value-type:json editable:false description:"Verified submission"
// dex:field attribute-key:webhook-form-forwarding value-type:json editable:false description:"Forwarding outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	submission, outcome, err := submissionInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"webhook-form-submission": submission, "webhook-form-forwarding": outcome,
	}}, nil
}

// GetDexDisplay returns the submission and forwarding outcome for the Dex Web run detail.
//
// dex:field attribute-key:webhook-form-submission value-type:json editable:false description:"Event ID, content type, and body"
// dex:field attribute-key:webhook-form-forwarding value-type:json editable:false description:"sendEvent branch, webhook-id, and status"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	submission, outcome, err := submissionInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"webhook-form-submission": submission, "webhook-form-forwarding": outcome,
	}}, nil
}

// AcceptSubmission is the application's admission rule: a submission needs a JSON object or form fields.
func AcceptSubmission(event sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) bool {
	if len(event.Payload.FormBody) > 0 {
		return true
	}
	var fields map[string]json.RawMessage
	return json.Unmarshal(event.Payload.JSONBody, &fields) == nil && len(fields) > 0
}

// ResolveFlowID derives the Flow ID from the event ID, so every redelivery maps to one Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) string {
	return FlowIDPrefix + event.ID
}

// MapToFlowInput copies the verified request into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) Submission {
	return Submission{
		EventID: event.ID, ContentType: event.Payload.ContentType, ReceivedAt: event.Payload.ReceivedAt,
		JSONBody: event.Payload.JSONBody, FormBody: event.Payload.FormBody,
	}
}

// MapToForwardedEvent builds the sendEvent payload; an encoding failure sends an empty one, which selects defect.
func MapToForwardedEvent(submission Submission) webhook.SendEventInput {
	payload, err := json.Marshal(ForwardedEvent{
		Type: ForwardedEventType, SubmissionID: submission.EventID, ReceivedAt: submission.ReceivedAt,
		JSONBody: submission.JSONBody, FormBody: submission.FormBody,
	})
	if err != nil {
		return webhook.SendEventInput{}
	}
	return webhook.SendEventInput{Payload: payload}
}

func submissionInspection(ctx dex.Context) (Submission, ForwardingOutcome, error) {
	submission, err := optionalAttribute(ctx, submissionAttribute)
	if err != nil {
		return Submission{}, ForwardingOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, forwardingAttribute)
	if err != nil {
		return Submission{}, ForwardingOutcome{}, err
	}
	return submission, outcome, nil
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

// dex:group group-id:submission group-label:"Submission"
// dex:explanation text:"Persist the verified submission before forwarding it."
type recordSubmission struct {
	dex.StepDefaultsNoWaitFor[Submission]
}

func (recordSubmission) GetStepType() string { return "RecordSubmission" }

func (recordSubmission) Execute(ctx dex.Context, submission Submission) (*dex.StepDecision, error) {
	if submission.EventID == "" {
		return dex.ForceFail("form submission requires an event ID"), nil
	}
	if err := submissionAttribute.Set(ctx, submission); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Submission](forwardSubmissionStepType), submission), nil
}

// dex:group group-id:forwarding group-label:"Forwarding"
// dex:explanation text:"Persist the delivered outcome with its webhook-id and complete the Flow."
type recordForwarded struct {
	dex.StepDefaultsNoWaitFor[webhook.SendEventResult]
}

func (recordForwarded) GetStepType() string { return "RecordForwarded" }

func (recordForwarded) Execute(ctx dex.Context, result webhook.SendEventResult) (*dex.StepDecision, error) {
	outcome := ForwardingOutcome{Branch: result.Branch, WebhookID: result.Value.WebhookID, StatusCode: result.Value.StatusCode}
	if err := forwardingAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:forwarding group-label:"Forwarding"
// dex:explanation text:"Persist the rejected, uncertain, or defect outcome with its safe message and fail the Flow."
type recordForwardingFailure struct {
	dex.StepDefaultsNoWaitFor[webhook.SendEventResult]
}

func (recordForwardingFailure) GetStepType() string { return "RecordForwardingFailure" }

func (recordForwardingFailure) Execute(ctx dex.Context, result webhook.SendEventResult) (*dex.StepDecision, error) {
	outcome := ForwardingOutcome{Branch: result.Branch, WebhookID: result.Value.WebhookID, StatusCode: result.Value.StatusCode}
	message := "forwarding selected " + string(result.Branch)
	if result.Failure != nil {
		outcome.FailureKind, outcome.FailureMessage = result.Failure.Kind, result.Failure.Message
		message += ": " + result.Failure.Message
	}
	if err := forwardingAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
