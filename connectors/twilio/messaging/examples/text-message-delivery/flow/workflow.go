// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package textmessagedelivery demonstrates the Twilio sendMessage Mutation and
// getMessage Query. The Flow sends one text, reads its delivery status after a
// durable Timer, and parks an uncertain send for an operator instead of
// sending it again.
package textmessagedelivery

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "TwilioTextMessageDelivery"
	// ConnectionName is the static Dex Web connection for Twilio Messaging.
	ConnectionName = "twilio-messaging"
	// ReconcileTextMessagePermission is required by both reconciliation Actions.
	ReconcileTextMessagePermission = "twilio-text-message.reconcile"

	recordTextMessageRequestStepType   = "RecordTextMessageRequest"
	sendTextMessageStepType            = "SendTextMessage"
	recordAcceptedTextMessageStepType  = "RecordAcceptedTextMessage"
	recordRejectedTextMessageStepType  = "RecordRejectedTextMessage"
	recordUncertainTextMessageStepType = "RecordUncertainTextMessage"
	waitForDeliveryStatusStepType      = "WaitForDeliveryStatus"
	getTextMessageStepType             = "GetTextMessage"
	recordDeliveryStatusStepType       = "RecordDeliveryStatus"
	recordMissingTextMessageStepType   = "RecordMissingTextMessage"
)

// Delivery phases stored in the twilio-text-message-phase Attribute.
const (
	// PhaseSending means a sendMessage Step is about to run or running.
	PhaseSending = "sending"
	// PhaseAwaitingDeliveryStatus means Twilio accepted the message and the Flow is reading its status.
	PhaseAwaitingDeliveryStatus = "awaitingDeliveryStatus"
	// PhaseNeedsReconciliation means the send outcome is unknown and an operator must confirm or resend.
	PhaseNeedsReconciliation = "needsReconciliation"
	// PhaseVerifyingReportedMessage means the Flow is reading the message SID an operator reported.
	PhaseVerifyingReportedMessage = "verifyingReportedMessage"
	// PhaseFinished means Twilio reported a final status: delivered, read, undelivered, failed, or canceled.
	PhaseFinished = "finished"
	// PhaseDeliveryStatusUnconfirmed means every allowed status read returned a non-final status.
	PhaseDeliveryStatusUnconfirmed = "deliveryStatusUnconfirmed"
	// PhaseRejected means Twilio conclusively rejected the message and sent nothing.
	PhaseRejected = "rejected"
)

var (
	deliveryPhaseAttribute = dex.DefineAttribute[string]("twilio-text-message-phase")
	deliveryAttribute      = dex.DefineAttribute[TextMessageDelivery]("twilio-text-message-delivery")
)

var errReportedMessageSIDInvalid = errors.New("message SID must be SM or MM followed by 32 lowercase hexadecimal characters")

// Input is the text message entered in Dex Web Start Flow.
type Input struct {
	// To is the recipient: an E.164 number such as +14155550100, or whatsapp:+14155550100.
	To string `json:"to"`
	// Body is the message text, at most 1600 characters.
	Body string `json:"body"`
	// Sender optionally overrides the connection's defaultSender; blank uses it.
	Sender string `json:"sender,omitempty"`
}

// DeliveryStatusCheck identifies the message whose status the Flow reads next.
type DeliveryStatusCheck struct {
	// MessageSID is the Twilio SM or MM message SID.
	MessageSID string `json:"messageSid"`
}

// UncertainSend records a dispatched send whose outcome Twilio did not confirm.
type UncertainSend struct {
	// CallID is the connector call identity of the uncertain send.
	CallID string `json:"callId"`
	// ObservedAt is when the connector observed the unknown outcome; search Twilio's log around it.
	ObservedAt time.Time `json:"observedAt"`
	// FailureKind is the safe connector failure category, such as TRANSPORT.
	FailureKind sdkgo.FailureKind `json:"failureKind"`
}

// TextMessageDelivery is the Flow's durable record of one text message.
type TextMessageDelivery struct {
	// Request is the validated Start Flow input.
	Request Input `json:"request"`
	// Phase mirrors the twilio-text-message-phase Attribute.
	Phase string `json:"phase"`
	// SendAttempts counts sendMessage Step executions: the first send plus each approved resend.
	SendAttempts int `json:"sendAttempts"`
	// StatusChecks counts getMessage reads for the current message.
	StatusChecks int `json:"statusChecks"`
	// Message is the latest Twilio message view; SID is empty until Twilio confirms one.
	Message messaging.Message `json:"message"`
	// UncertainSend describes the latest uncertain send while reconciliation is pending.
	UncertainSend *UncertainSend `json:"uncertainSend,omitempty"`
	// ReconciliationNote explains why a reported message SID was not adopted.
	ReconciliationNote string `json:"reconciliationNote,omitempty"`
}

// ConfirmSentTextMessageInput reports the SID an operator found in Twilio's message log.
type ConfirmSentTextMessageInput struct {
	// MessageSID is the SM or MM SID shown in Twilio Console > Monitor > Logs > Messaging.
	MessageSID string `json:"messageSid"`
}

// DeliveryStatusPolicy bounds how long the Flow reads status after Twilio accepts a message.
type DeliveryStatusPolicy struct {
	// CheckInterval is the durable Timer before each status read; at least one second.
	CheckInterval time.Duration
	// MaximumChecks is the number of reads before the Flow completes as deliveryStatusUnconfirmed.
	MaximumChecks int
}

// DefaultDeliveryStatusPolicy reads status every 15 seconds for two minutes.
func DefaultDeliveryStatusPolicy() DeliveryStatusPolicy {
	return DeliveryStatusPolicy{CheckInterval: 15 * time.Second, MaximumChecks: 8}
}

// Flow sends one text message and follows it to a final delivery status.
type Flow struct {
	dex.FlowDefaults
	connection messaging.Connection
	policy     DeliveryStatusPolicy
}

// NewFlow binds the Twilio Connection and the delivery status policy at registration time.
// It panics when policy is nil, its interval is below one second, or it allows no checks.
func NewFlow(connection messaging.Connection, policy *DeliveryStatusPolicy) *Flow {
	if policy == nil || policy.CheckInterval < time.Second || policy.MaximumChecks < 1 {
		panic("text message delivery requires a status interval of at least one second and at least one check")
	}
	return &Flow{connection: connection, policy: *policy}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, send, status, and reconciliation Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTextMessageRequest{}),
		dex.DefineStep(messaging.NewSendMessageStep(messaging.SendMessageStepConfig[Input]{
			StepType: sendTextMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "twilio", GroupLabel: "Twilio",
				Explanation: "Send the text once; an unknown outcome is reconciled, never resent automatically.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToSendMessageInput,
			Accepted:         sdkgo.GoTo(recordAcceptedTextMessage{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedTextMessage{}),
			Uncertain:        sdkgo.GoTo(recordUncertainTextMessage{}),
		})),
		dex.DefineStep(recordAcceptedTextMessage{}),
		dex.DefineStep(recordRejectedTextMessage{}),
		dex.DefineStep(recordUncertainTextMessage{}),
		dex.DefineStep(waitForDeliveryStatus{checkInterval: flow.policy.CheckInterval}),
		dex.DefineStep(messaging.NewGetMessageStep(messaging.GetMessageStepConfig[DeliveryStatusCheck]{
			StepType: getTextMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "twilio", GroupLabel: "Twilio",
				Explanation: "Read the message's current Twilio delivery status and error code.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetMessageInput,
			Found:    sdkgo.GoTo(recordDeliveryStatus{}),
			NotFound: sdkgo.GoTo(recordMissingTextMessage{}),
		})),
		dex.DefineStep(recordDeliveryStatus{maximumChecks: flow.policy.MaximumChecks}),
		dex.DefineStep(recordMissingTextMessage{}),
	}
}

// GetRPCs returns the reconciliation Actions, the delivery read RPC, and the Dex Web views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	reconciliationLocks := []dex.AttributeLock{dex.LockAttribute(deliveryPhaseAttribute), dex.LockAttribute(deliveryAttribute)}
	return []dex.RPCDef{
		dex.DefineRPC(flow.ConfirmSentTextMessage, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Confirm sent message",
				dex.WhenAttributeMatches(deliveryPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileTextMessagePermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.ApproveTextMessageResend, &dex.RPCOptions{
			Action: dex.DefineAction(
				"Resend message",
				dex.WhenAttributeMatches(deliveryPhaseAttribute, dex.AttributeMatchEqual(PhaseNeedsReconciliation)),
				dex.ActionRequiresPermission(ReconcileTextMessagePermission),
			),
			LockAttributes: reconciliationLocks,
		}),
		dex.DefineRPC(flow.GetTextMessageDelivery, nil),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the phase and delivery Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deliveryPhaseAttribute, deliveryAttribute}}
}

// MapToSendMessageInput maps the recorded request to one Twilio send.
func (*Flow) MapToSendMessageInput(input Input) messaging.SendMessageInput {
	return messaging.SendMessageInput{To: input.To, Body: input.Body, Sender: input.Sender}
}

// MapToGetMessageInput maps a status check to one Twilio message read.
func (*Flow) MapToGetMessageInput(check DeliveryStatusCheck) messaging.GetMessageInput {
	return messaging.GetMessageInput{MessageSID: check.MessageSID}
}

// ConfirmSentTextMessage adopts a message an operator found in Twilio's log after an uncertain send.
// The Flow reads that SID with getMessage and adopts it only when its recipient matches the request.
//
// dex:input field-name:messageSid value-type:string source:user required:true description:"SM or MM SID from Twilio Console > Monitor > Logs > Messaging"
func (*Flow) ConfirmSentTextMessage(ctx dex.Context, input ConfirmSentTextMessageInput) (*dex.RPCResult[dex.None], error) {
	phase, err := deliveryPhaseAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	messageSID := strings.TrimSpace(input.MessageSID)
	if !isMessageSID(messageSID) {
		return nil, errReportedMessageSIDInvalid
	}
	delivery.Phase = PhaseVerifyingReportedMessage
	delivery.StatusChecks = 0
	delivery.ReconciliationNote = ""
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[DeliveryStatusCheck](getTextMessageStepType), DeliveryStatusCheck{MessageSID: messageSID})},
	}, nil
}

// ApproveTextMessageResend sends the text again after an operator confirmed Twilio has no such message.
// The resend is a new Step execution with a new connector call ID.
func (*Flow) ApproveTextMessageResend(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	phase, err := deliveryPhaseAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if phase != PhaseNeedsReconciliation {
		return &dex.RPCResult[dex.None]{}, nil
	}
	delivery.Phase = PhaseSending
	delivery.SendAttempts++
	delivery.UncertainSend = nil
	delivery.ReconciliationNote = ""
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[Input](sendTextMessageStepType), delivery.Request)},
	}, nil
}

// GetTextMessageDelivery returns the current delivery record.
func (*Flow) GetTextMessageDelivery(ctx dex.Context, _ dex.None) (*dex.RPCResult[TextMessageDelivery], error) {
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[TextMessageDelivery]{Output: delivery}, nil
}

// GetDexSummary returns the delivery phase and record for Dex Web lists.
//
// dex:field attribute-key:twilio-text-message-phase value-type:string editable:false description:"Delivery phase"
// dex:field attribute-key:twilio-text-message-delivery value-type:json editable:false description:"Recipient, message SID, Twilio status, and error code"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, deliveryPhaseAttribute)
	if err != nil {
		return nil, err
	}
	delivery, err := optionalAttribute(ctx, deliveryAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"twilio-text-message-phase":    phase,
		"twilio-text-message-delivery": delivery,
	}}, nil
}

// GetDexDisplay returns the delivery phase and record for the Dex Web run view.
//
// dex:field attribute-key:twilio-text-message-phase value-type:string editable:false description:"Delivery phase" ui-slot:status
// dex:field attribute-key:twilio-text-message-delivery value-type:json editable:false description:"Request, Twilio message, status reads, and any uncertain send awaiting reconciliation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	phase, err := optionalAttribute(ctx, deliveryPhaseAttribute)
	if err != nil {
		return nil, err
	}
	delivery, err := optionalAttribute(ctx, deliveryAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"twilio-text-message-phase":    phase,
		"twilio-text-message-delivery": delivery,
	}}, nil
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate and record the text message before calling Twilio."
type recordTextMessageRequest struct {
	dex.StepDefaults
}

func (recordTextMessageRequest) GetStepType() string { return recordTextMessageRequestStepType }

func (recordTextMessageRequest) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(deliveryPhaseAttribute), dex.LockAttribute(deliveryAttribute)}}
}

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordTextMessageRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordTextMessageRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.To = strings.TrimSpace(input.To)
	input.Sender = strings.TrimSpace(input.Sender)
	if input.To == "" || strings.TrimSpace(input.Body) == "" {
		return dex.ForceFail("to and body are required"), nil
	}
	delivery := TextMessageDelivery{Request: input, Phase: PhaseSending, SendAttempts: 1}
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](sendTextMessageStepType), input), nil
}

// dex:group group-id:twilio group-label:"Twilio"
// dex:explanation text:"Record the SID Twilio accepted and schedule the first status read."
type recordAcceptedTextMessage struct {
	dex.StepDefaultsNoWaitFor[messaging.SendMessageResult]
}

func (recordAcceptedTextMessage) GetStepType() string { return recordAcceptedTextMessageStepType }

func (recordAcceptedTextMessage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(deliveryPhaseAttribute), dex.LockAttribute(deliveryAttribute)}}
}

func (recordAcceptedTextMessage) Execute(ctx dex.Context, result messaging.SendMessageResult) (*dex.StepDecision, error) {
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	delivery.Phase = PhaseAwaitingDeliveryStatus
	delivery.Message = result.Value
	delivery.StatusChecks = 0
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	return dex.GoTo(waitForDeliveryStatus{}, DeliveryStatusCheck{MessageSID: result.Value.SID}), nil
}

// dex:group group-id:twilio group-label:"Twilio"
// dex:explanation text:"Complete with the Twilio error code after a conclusive rejection; nothing was sent."
type recordRejectedTextMessage struct {
	dex.StepDefaultsNoWaitFor[messaging.SendMessageResult]
}

func (recordRejectedTextMessage) GetStepType() string { return recordRejectedTextMessageStepType }

func (recordRejectedTextMessage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(deliveryPhaseAttribute), dex.LockAttribute(deliveryAttribute)}}
}

func (recordRejectedTextMessage) Execute(ctx dex.Context, result messaging.SendMessageResult) (*dex.StepDecision, error) {
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	delivery.Phase = PhaseRejected
	delivery.Message = result.Value
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(delivery), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Park an unknown send for an operator instead of sending the text again."
type recordUncertainTextMessage struct {
	dex.StepDefaultsNoWaitFor[messaging.SendMessageResult]
}

func (recordUncertainTextMessage) GetStepType() string { return recordUncertainTextMessageStepType }

func (recordUncertainTextMessage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(deliveryPhaseAttribute), dex.LockAttribute(deliveryAttribute)}}
}

func (recordUncertainTextMessage) Execute(ctx dex.Context, result messaging.SendMessageResult) (*dex.StepDecision, error) {
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	delivery.Phase = PhaseNeedsReconciliation
	delivery.Message = result.Value
	delivery.UncertainSend = &UncertainSend{CallID: string(result.Receipt.CallID), ObservedAt: result.Receipt.ObservedAt}
	if result.Failure != nil {
		delivery.UncertainSend.FailureKind = result.Failure.Kind
	}
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:delivery group-label:"Delivery"
// dex:explanation text:"Wait on a durable Timer before reading the delivery status again."
type waitForDeliveryStatus struct {
	dex.StepDefaults
	checkInterval time.Duration
}

func (waitForDeliveryStatus) GetStepType() string { return waitForDeliveryStatusStepType }

func (step waitForDeliveryStatus) WaitFor(dex.Context, DeliveryStatusCheck) (*dex.Wait, error) {
	return dex.Until(dex.Timer(step.checkInterval)), nil
}

func (waitForDeliveryStatus) Execute(_ dex.Context, check DeliveryStatusCheck) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[DeliveryStatusCheck](getTextMessageStepType), check), nil
}

// dex:group group-id:delivery group-label:"Delivery"
// dex:explanation text:"Record the Twilio status, then finish, give up after the last read, or read again."
type recordDeliveryStatus struct {
	dex.StepDefaultsNoWaitFor[messaging.GetMessageResult]
	maximumChecks int
}

func (recordDeliveryStatus) GetStepType() string { return recordDeliveryStatusStepType }

func (recordDeliveryStatus) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(deliveryPhaseAttribute), dex.LockAttribute(deliveryAttribute)}}
}

func (step recordDeliveryStatus) Execute(ctx dex.Context, result messaging.GetMessageResult) (*dex.StepDecision, error) {
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if delivery.Phase == PhaseVerifyingReportedMessage && result.Value.To != delivery.Request.To {
		delivery.Phase = PhaseNeedsReconciliation
		delivery.ReconciliationNote = "the reported message was sent to a different recipient"
		if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
			return nil, err
		}
		if err := deliveryAttribute.Set(ctx, delivery); err != nil {
			return nil, err
		}
		return dex.DeadEnd(), nil
	}
	delivery.Message = result.Value
	delivery.UncertainSend = nil
	delivery.StatusChecks++
	switch {
	case isFinalDeliveryStatus(result.Value.Status):
		delivery.Phase = PhaseFinished
	case delivery.StatusChecks >= step.maximumChecks:
		delivery.Phase = PhaseDeliveryStatusUnconfirmed
	default:
		delivery.Phase = PhaseAwaitingDeliveryStatus
	}
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	if delivery.Phase == PhaseAwaitingDeliveryStatus {
		return dex.GoTo(waitForDeliveryStatus{}, DeliveryStatusCheck{MessageSID: result.Value.SID}), nil
	}
	return dex.GracefulComplete(delivery), nil
}

// dex:group group-id:reconciliation group-label:"Reconciliation"
// dex:explanation text:"Return a reported SID that Twilio does not know to the operator."
type recordMissingTextMessage struct {
	dex.StepDefaultsNoWaitFor[messaging.GetMessageResult]
}

func (recordMissingTextMessage) GetStepType() string { return recordMissingTextMessageStepType }

func (recordMissingTextMessage) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(deliveryPhaseAttribute), dex.LockAttribute(deliveryAttribute)}}
}

func (recordMissingTextMessage) Execute(ctx dex.Context, _ messaging.GetMessageResult) (*dex.StepDecision, error) {
	delivery, err := deliveryAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if delivery.Phase != PhaseVerifyingReportedMessage {
		return dex.ForceFail("Twilio no longer reports the accepted message"), nil
	}
	delivery.Phase = PhaseNeedsReconciliation
	delivery.ReconciliationNote = "Twilio has no message with the reported SID"
	if err := deliveryPhaseAttribute.Set(ctx, delivery.Phase); err != nil {
		return nil, err
	}
	if err := deliveryAttribute.Set(ctx, delivery); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

func isFinalDeliveryStatus(status messaging.MessageStatus) bool {
	switch status {
	case messaging.MessageStatusDelivered, messaging.MessageStatusRead, messaging.MessageStatusUndelivered,
		messaging.MessageStatusFailed, messaging.MessageStatusCanceled:
		return true
	default:
		return false
	}
}

func isMessageSID(value string) bool {
	if len(value) != 34 || (!strings.HasPrefix(value, "SM") && !strings.HasPrefix(value, "MM")) {
		return false
	}
	for _, character := range value[2:] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
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

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[ConfirmSentTextMessageInput, dex.None] = (*Flow)(nil).ConfirmSentTextMessage
var _ dex.RPC[dex.None, dex.None] = (*Flow)(nil).ApproveTextMessageResend
var _ dex.RPC[dex.None, TextMessageDelivery] = (*Flow)(nil).GetTextMessageDelivery
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
