// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package queueconsumer demonstrates every Amazon SQS operation in one Flow started from Dex Web Start Flow:
// receive one order event, acknowledge it by deleting it once it is processed, or release it back to the
// queue when it is not a valid order event, so the queue's redrive policy can move it to a dead-letter queue.
package queueconsumer

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "AmazonSQSQueueConsumer"
	// ConnectionName is the static Dex Web connection for Amazon SQS.
	ConnectionName = "amazon-sqs-orders"
	// ProcessingVisibilityTimeoutSeconds hides the received message from other consumers while this Flow
	// processes it; the Flow acknowledges or releases it well within this time.
	ProcessingVisibilityTimeoutSeconds = 300

	recordRequestStepType       = "RecordQueueConsumerRequest"
	receiveOrderEventStepType   = "ReceiveOrderEvent"
	inspectOrderEventStepType   = "InspectOrderEvent"
	acknowledgeEventStepType    = "AcknowledgeOrderEvent"
	releaseEventStepType        = "ReleaseOrderEvent"
	recordAcknowledgedStepType  = "RecordAcknowledgedOrderEvent"
	recordReleasedEventStepType = "RecordReleasedOrderEvent"
)

var (
	requestAttribute = dex.DefineAttribute[ConsumerRequest]("amazon-sqs-queue-consumer-request")
	outcomeAttribute = dex.DefineAttribute[Outcome]("amazon-sqs-queue-consumer-outcome")
	orderIDPattern   = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	orderEventKinds  = map[string]bool{"created": true, "paid": true, "shipped": true, "cancelled": true}
)

// Input is the typed request entered in Dex Web Start Flow.
type Input struct {
	// QueueURL is the queue to consume from, such as https://sqs.eu-west-1.amazonaws.com/123456789012/orders;
	// blank uses the connection's defaultQueueUrl.
	QueueURL string `json:"queueUrl,omitempty"`
}

// ConsumerRequest is the validated request the Flow records before receiving.
type ConsumerRequest struct {
	// QueueURL is the requested queue; blank uses the connection's defaultQueueUrl.
	QueueURL string `json:"queueUrl,omitempty"`
}

// OrderEvent is the message body this consumer accepts, such as {"orderId":"1042","event":"paid"}.
type OrderEvent struct {
	// OrderID is 1 to 64 letters, digits, and hyphens.
	OrderID string `json:"orderId"`
	// Event is created, paid, shipped, or cancelled.
	Event string `json:"event"`
}

// MessageReceipt is the application input of the acknowledgement Steps: the latest delivery of one message.
type MessageReceipt struct {
	// QueueURL is the message's queue.
	QueueURL string `json:"queueUrl"`
	// MessageID is the message's ID.
	MessageID string `json:"messageId"`
	// ReceiptHandle is the handle of this delivery, which the acknowledgement needs.
	ReceiptHandle string `json:"receiptHandle"`
}

// ReceivedMessageSummary records the delivery facts of the received message, without its receipt handle.
type ReceivedMessageSummary struct {
	// MessageID is the message's ID.
	MessageID string `json:"messageId"`
	// ReceiveCount is how many times SQS has delivered the message, including this delivery.
	ReceiveCount int `json:"receiveCount"`
	// SentAt is when SQS accepted the message.
	SentAt time.Time `json:"sentAt,omitzero"`
}

// Status is the business outcome of one consumer run.
type Status string

const (
	// StatusNoMessage means no message arrived within the connection's receive wait.
	StatusNoMessage Status = "noMessage"
	// StatusProcessed means the message was a valid order event and the Flow acknowledged it.
	StatusProcessed Status = "processed"
	// StatusRejected means the message was not a valid order event and the Flow released it to the queue.
	StatusRejected Status = "rejected"
)

// Outcome is the durable result of the Flow and its completion output.
type Outcome struct {
	// Status is the business outcome.
	Status Status `json:"status"`
	// Message describes the received delivery.
	Message *ReceivedMessageSummary `json:"message,omitempty"`
	// OrderEvent is the processed order event.
	OrderEvent *OrderEvent `json:"orderEvent,omitempty"`
	// RejectionReason explains why the message was not a valid order event.
	RejectionReason string `json:"rejectionReason,omitempty"`
	// IsAcknowledgementConfirmed reports that SQS accepted the deletion or release. On a processed or
	// rejected outcome, false means the handle was invalid and the message may be delivered again.
	IsAcknowledgementConfirmed bool `json:"isAcknowledgementConfirmed"`
}

// Flow consumes one order event from Amazon SQS and acknowledges or releases it.
type Flow struct {
	dex.FlowDefaults
	connection sqs.Connection
}

// NewFlow binds the Amazon SQS Connection.
func NewFlow(connection sqs.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, Amazon SQS, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordConsumerRequest{}),
		dex.DefineStep(sqs.NewReceiveMessagesStep(sqs.ReceiveMessagesStepConfig[ConsumerRequest]{
			StepType: receiveOrderEventStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "receive-order-event", GroupLabel: "Receive order event",
				Explanation: "Long-poll the queue for one order event and hide it from other consumers while it is processed.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReceiveMessagesInput,
			Received: sdkgo.GoTo(inspectOrderEvent{}),
			// An empty queue is routine; this poll completes without processing a message.
			Empty: sdkgo.GoTo(inspectOrderEvent{}),
		})),
		dex.DefineStep(inspectOrderEvent{}),
		dex.DefineStep(sqs.NewDeleteMessageStep(sqs.DeleteMessageStepConfig[MessageReceipt]{
			StepType: acknowledgeEventStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "acknowledge-order-event", GroupLabel: "Acknowledge order event",
				Explanation: "Acknowledge the processed order event by deleting it, so SQS never delivers it again.",
			},
			Connection: flow.connection, MapToOperationInput: MapToDeleteMessageInput,
			Deleted: sdkgo.GoTo(recordAcknowledgedOrderEvent{}),
			// A repeat may find the message already deleted; keep processing idempotent.
			ReceiptHandleInvalid: sdkgo.GoTo(recordAcknowledgedOrderEvent{}),
		})),
		dex.DefineStep(recordAcknowledgedOrderEvent{}),
		dex.DefineStep(sqs.NewChangeMessageVisibilityStep(sqs.ChangeMessageVisibilityStepConfig[MessageReceipt]{
			StepType: releaseEventStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "release-order-event", GroupLabel: "Release order event",
				Explanation: "Release the invalid message to the queue at once, so its redrive policy can move it to a dead-letter queue.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReleaseMessageInput,
			Changed: sdkgo.GoTo(recordReleasedOrderEvent{}),
			// A repeat may find the message already released; keep processing idempotent.
			ReceiptHandleInvalid: sdkgo.GoTo(recordReleasedOrderEvent{}),
		})),
		dex.DefineStep(recordReleasedOrderEvent{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, outcomeAttribute}}
}

// GetDexSummary returns the request and outcome.
//
// dex:field attribute-key:amazon-sqs-queue-consumer-request value-type:json editable:false description:"Queue URL"
// dex:field attribute-key:amazon-sqs-queue-consumer-outcome value-type:json editable:false description:"Consumer outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := queueConsumerInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"amazon-sqs-queue-consumer-request": request,
		"amazon-sqs-queue-consumer-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the request and outcome.
//
// dex:field attribute-key:amazon-sqs-queue-consumer-request value-type:json editable:false description:"Queue URL"
// dex:field attribute-key:amazon-sqs-queue-consumer-outcome value-type:json editable:false description:"Received message, order event, and acknowledgement"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := queueConsumerInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"amazon-sqs-queue-consumer-request": request,
		"amazon-sqs-queue-consumer-outcome": outcome,
	}}, nil
}

// MapToReceiveMessagesInput receives one message and hides it while the Flow processes it.
func MapToReceiveMessagesInput(request ConsumerRequest) sqs.ReceiveMessagesInput {
	visibilityTimeoutSeconds := ProcessingVisibilityTimeoutSeconds
	return sqs.ReceiveMessagesInput{QueueURL: request.QueueURL, MaxMessages: 1, VisibilityTimeoutSeconds: &visibilityTimeoutSeconds}
}

// MapToDeleteMessageInput acknowledges the processed message.
func MapToDeleteMessageInput(receipt MessageReceipt) sqs.DeleteMessageInput {
	return sqs.DeleteMessageInput{QueueURL: receipt.QueueURL, ReceiptHandle: receipt.ReceiptHandle, MessageID: receipt.MessageID}
}

// MapToReleaseMessageInput makes the rejected message visible again at once.
func MapToReleaseMessageInput(receipt MessageReceipt) sqs.ChangeMessageVisibilityInput {
	return sqs.ChangeMessageVisibilityInput{
		QueueURL: receipt.QueueURL, ReceiptHandle: receipt.ReceiptHandle, MessageID: receipt.MessageID, VisibilityTimeoutSeconds: 0,
	}
}

// ParseOrderEvent decodes and validates one message body.
func ParseOrderEvent(body string) (OrderEvent, error) {
	var event OrderEvent
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		return OrderEvent{}, errors.New("the message body is not a JSON order event")
	}
	if !orderIDPattern.MatchString(event.OrderID) {
		return OrderEvent{}, errors.New("orderId must be 1 to 64 letters, digits, and hyphens")
	}
	if !orderEventKinds[event.Event] {
		return OrderEvent{}, errors.New("event must be created, paid, shipped, or cancelled")
	}
	return event, nil
}

func queueConsumerInspection(ctx dex.Context) (ConsumerRequest, Outcome, error) {
	request, err := optionalAttribute(ctx, requestAttribute)
	if err != nil {
		return ConsumerRequest{}, Outcome{}, err
	}
	outcome, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return ConsumerRequest{}, Outcome{}, err
	}
	return request, outcome, nil
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

// dex:group group-id:receive-order-event group-label:"Receive order event"
// dex:explanation text:"Record the queue to consume from before receiving."
type recordConsumerRequest struct {
	dex.StepDefaults
}

func (recordConsumerRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordConsumerRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordConsumerRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request := ConsumerRequest{QueueURL: input.QueueURL}
	if err := requestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ConsumerRequest](receiveOrderEventStepType), request), nil
}

// dex:group group-id:receive-order-event group-label:"Receive order event"
// dex:explanation text:"Validate the received order event and choose to acknowledge or release it."
type inspectOrderEvent struct {
	dex.StepDefaultsNoWaitFor[sqs.ReceiveMessagesResult]
}

func (inspectOrderEvent) GetStepType() string { return inspectOrderEventStepType }

func (inspectOrderEvent) Execute(ctx dex.Context, result sqs.ReceiveMessagesResult) (*dex.StepDecision, error) {
	if result.Branch == sqs.ReceiveMessagesBranchEmpty {
		outcome := Outcome{Status: StatusNoMessage}
		if err := outcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	message := result.Value.Messages[0]
	receipt := MessageReceipt{QueueURL: result.Value.QueueURL, MessageID: message.MessageID, ReceiptHandle: message.ReceiptHandle}
	outcome := Outcome{Message: &ReceivedMessageSummary{MessageID: message.MessageID, ReceiveCount: message.ReceiveCount, SentAt: message.SentAt}}
	event, err := ParseOrderEvent(message.Body)
	if err != nil {
		outcome.Status, outcome.RejectionReason = StatusRejected, err.Error()
		if err := outcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[MessageReceipt](releaseEventStepType), receipt), nil
	}
	outcome.Status, outcome.OrderEvent = StatusProcessed, &event
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[MessageReceipt](acknowledgeEventStepType), receipt), nil
}

// dex:group group-id:acknowledge-order-event group-label:"Acknowledge order event"
// dex:explanation text:"Record whether SQS accepted the acknowledgement and complete the Flow."
type recordAcknowledgedOrderEvent struct {
	dex.StepDefaultsNoWaitFor[sqs.DeleteMessageResult]
}

func (recordAcknowledgedOrderEvent) GetStepType() string { return recordAcknowledgedStepType }

func (recordAcknowledgedOrderEvent) Execute(ctx dex.Context, result sqs.DeleteMessageResult) (*dex.StepDecision, error) {
	outcome, err := recordAcknowledgement(ctx, result.Branch == sqs.DeleteMessageBranchDeleted)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:release-order-event group-label:"Release order event"
// dex:explanation text:"Record whether SQS accepted the release and complete the Flow."
type recordReleasedOrderEvent struct {
	dex.StepDefaultsNoWaitFor[sqs.ChangeMessageVisibilityResult]
}

func (recordReleasedOrderEvent) GetStepType() string { return recordReleasedEventStepType }

func (recordReleasedOrderEvent) Execute(ctx dex.Context, result sqs.ChangeMessageVisibilityResult) (*dex.StepDecision, error) {
	outcome, err := recordAcknowledgement(ctx, result.Branch == sqs.ChangeMessageVisibilityBranchChanged)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// recordAcknowledgement stores whether SQS accepted the deletion or release and returns the final outcome.
func recordAcknowledgement(ctx dex.Context, isConfirmed bool) (Outcome, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return Outcome{}, err
	}
	outcome.IsAcknowledgementConfirmed = isConfirmed
	return outcome, outcomeAttribute.Set(ctx, outcome)
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
