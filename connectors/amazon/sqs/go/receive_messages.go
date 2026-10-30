// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	receiveMessagesOperationID = "receiveMessages"
	// maxReceiveRequestAttemptIDBytes is the SQS limit on ReceiveRequestAttemptId.
	maxReceiveRequestAttemptIDBytes = 128
)

// receivedSystemAttributeNames are the message system attributes every receive asks for.
var receivedSystemAttributeNames = []string{
	"ApproximateReceiveCount", "ApproximateFirstReceiveTimestamp", "SentTimestamp",
	"MessageGroupId", "MessageDeduplicationId", "SequenceNumber",
}

// messageAttributeNamePattern is the SQS rule for message attribute names, which receive returns unchanged.
var messageAttributeNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,256}$`)

// ReceiveMessagesInput selects one queue and how long and how many messages to wait for.
type ReceiveMessagesInput struct {
	// QueueURL is the queue URL, such as https://sqs.eu-west-1.amazonaws.com/123456789012/orders; blank uses
	// the connection's defaultQueueUrl.
	QueueURL string `json:"queueUrl,omitempty"`
	// MaxMessages is the most messages to receive, from 1 to 10; zero uses the connection's maxMessagesPerReceive.
	MaxMessages int `json:"maxMessages,omitempty"`
	// WaitTimeSeconds is how long to wait for a message to arrive, from 0 to 20 seconds; nil uses the
	// connection's receiveWaitSeconds, and 0 returns at once.
	WaitTimeSeconds *int `json:"waitTimeSeconds,omitempty"`
	// VisibilityTimeoutSeconds hides each received message from other consumers for this many seconds, from 0
	// to 43200; nil uses the queue's own visibility timeout.
	VisibilityTimeoutSeconds *int `json:"visibilityTimeoutSeconds,omitempty"`
}

// ReceivedMessages is one batch of received messages, in the order SQS returned them.
type ReceivedMessages struct {
	// QueueURL is the queue the batch came from.
	QueueURL string `json:"queueUrl"`
	// IsFIFO reports a FIFO queue, whose messages carry MessageGroupID and SequenceNumber.
	IsFIFO bool `json:"isFifo,omitempty"`
	// Messages lists the received messages; it is empty only on the empty branch.
	Messages []Message `json:"messages,omitempty"`
}

// Message is one received message. Pass ReceiptHandle to deleteMessage or changeMessageVisibility.
type Message struct {
	// MessageID is the ID SQS assigned when the message was sent; it stays the same on every delivery.
	MessageID string `json:"messageId"`
	// ReceiptHandle identifies this delivery of the message. Each receive returns a new handle, and only the
	// latest one is sure to delete the message or change its visibility.
	ReceiptHandle string `json:"receiptHandle"`
	// Body is the message body text, verified against the MD5 digest SQS sent with it.
	Body string `json:"body"`
	// MD5OfBody is the hex MD5 digest of Body.
	MD5OfBody string `json:"md5OfBody"`
	// SentAt is when SQS accepted the message.
	SentAt time.Time `json:"sentAt,omitzero"`
	// FirstReceivedAt is when any consumer first received the message.
	FirstReceivedAt time.Time `json:"firstReceivedAt,omitzero"`
	// ReceiveCount is how many times the message has been received, including this delivery. A count above 1
	// means an earlier delivery was not deleted, so processing must tolerate a repeat.
	ReceiveCount int `json:"receiveCount"`
	// MessageGroupID is the FIFO message group; empty for a standard queue.
	MessageGroupID string `json:"messageGroupId,omitempty"`
	// MessageDeduplicationID is the FIFO deduplication ID; empty for a standard queue.
	MessageDeduplicationID string `json:"messageDeduplicationId,omitempty"`
	// SequenceNumber is the FIFO sequence number, a decimal string of up to 128 bits; empty for a standard queue.
	SequenceNumber string `json:"sequenceNumber,omitempty"`
	// MessageAttributes are the sender's custom message attributes, keyed by name.
	MessageAttributes map[string]MessageAttributeValue `json:"messageAttributes,omitempty"`
}

// MessageAttributeValue is one custom message attribute.
type MessageAttributeValue struct {
	// DataType is String, Number, or Binary, optionally followed by a custom type such as Number.int.
	DataType string `json:"dataType"`
	// StringValue holds a String or Number value.
	StringValue string `json:"stringValue,omitempty"`
	// BinaryValue holds a Binary value, encoded as base64 in JSON.
	BinaryValue []byte `json:"binaryValue,omitempty"`
}

// ReceiveMessagesOperation implements the receiveMessages connector operation.
type ReceiveMessagesOperation struct{ client *Client }

// receiveMessageRequest is the ReceiveMessage JSON request.
type receiveMessageRequest struct {
	QueueURL            string `json:"QueueUrl"`
	MaxNumberOfMessages int    `json:"MaxNumberOfMessages"`
	WaitTimeSeconds     int    `json:"WaitTimeSeconds"`
	VisibilityTimeout   *int   `json:"VisibilityTimeout,omitempty"`
	// AttributeNames repeats MessageSystemAttributeNames for compatible servers that predate the rename.
	AttributeNames              []string `json:"AttributeNames"`
	MessageSystemAttributeNames []string `json:"MessageSystemAttributeNames"`
	MessageAttributeNames       []string `json:"MessageAttributeNames"`
	ReceiveRequestAttemptID     string   `json:"ReceiveRequestAttemptId,omitempty"`
}

// receiveMessageResponse is the ReceiveMessage JSON response.
type receiveMessageResponse struct {
	Messages []receivedMessage `json:"Messages"`
}

type receivedMessage struct {
	MessageID         string                            `json:"MessageId"`
	ReceiptHandle     string                            `json:"ReceiptHandle"`
	MD5OfBody         string                            `json:"MD5OfBody"`
	Body              *string                           `json:"Body"`
	Attributes        map[string]string                 `json:"Attributes"`
	MessageAttributes map[string]receivedAttributeValue `json:"MessageAttributes"`
}

type receivedAttributeValue struct {
	DataType    string `json:"DataType"`
	StringValue string `json:"StringValue"`
	BinaryValue []byte `json:"BinaryValue"`
}

// Definition returns the immutable connector operation definition.
func (ReceiveMessagesOperation) Definition() sdkgo.MutationDefinition {
	return ReceiveMessagesDefinition
}

// IdempotencyKey derives the key from the stable connector call ID, so every attempt of one Step execution
// sends the same FIFO ReceiveRequestAttemptId.
func (ReceiveMessagesOperation) IdempotencyKey(callID sdkgo.CallID, _ ReceiveMessagesInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one ReceiveMessage request and returns the verified batch. Every ambiguous outcome returns
// Retry: messages a lost response received stay hidden until their visibility timeout ends and are then
// delivered again, and a FIFO retry within five minutes gets the same batch back through its
// ReceiveRequestAttemptId.
func (operation ReceiveMessagesOperation) Invoke(call sdkgo.Call, input ReceiveMessagesInput) sdkgo.MutationAttempt[ReceivedMessages] {
	client := operation.client
	queue, request, err := operation.buildRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(ReceiveMessagesBranchDefect, ReceivedMessages{}, defectFailure(receiveMessagesOperationID, err), sdkgo.Receipt{})
	}
	if queue.isFIFO {
		attemptID := string(call.IdempotencyKey)
		if attemptID == "" || len(attemptID) > maxReceiveRequestAttemptIDBytes || !isPrintableToken(attemptID) {
			return sdkgo.NewMutationBranch(ReceiveMessagesBranchDefect, ReceivedMessages{}, failurePointer(receiveMessagesOperationID, sdkgo.FailureLocalDefect,
				"idempotency key is missing or not a valid ReceiveRequestAttemptId"), sdkgo.Receipt{})
		}
		request.ReceiveRequestAttemptID = attemptID
	}
	session, cancel, failure := client.startSession(call, receiveMessagesOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(ReceiveMessagesBranchDefect, ReceivedMessages{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	batch := ReceivedMessages{QueueURL: queue.url, IsFIFO: queue.isFIFO}
	response, err := client.exchange(session, "ReceiveMessage", request)
	if errors.Is(err, errRequestNotBuilt) {
		return sdkgo.NewMutationBranch(ReceiveMessagesBranchDefect, batch, failurePointer(receiveMessagesOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ReceivedMessages](transportFailure(receiveMessagesOperationID, "receive"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(receiveMessagesOperationID, "receive", client.readErrorResponse(response, session.credentials))
		return mutationAttemptForFailure(classified, client.receipt(call, classified.requestID, queue.url), "",
			ReceiveMessagesBranchProviderRejected, ReceiveMessagesBranchDefect, batch)
	}
	receipt := client.receipt(call, safeRequestID(response.Header), queue.url)
	contents, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(ReceiveMessagesBranchInvalidResponse, batch, failurePointer(receiveMessagesOperationID, sdkgo.FailureResponseTooLarge,
			"SQS receive batch exceeds maxResponseBytes; lower maxMessages"), receipt)
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ReceivedMessages](transportFailure(receiveMessagesOperationID, "receive"), 0)
	}
	if containsCredential(string(contents), session.credentials) {
		return sdkgo.NewMutationBranch(ReceiveMessagesBranchInvalidResponse, batch, failurePointer(receiveMessagesOperationID, sdkgo.FailureProtocol,
			"SQS receive batch contains the connection's credentials"), receipt)
	}
	messages, err := decodeReceiveMessageResponse(contents, request.MaxNumberOfMessages)
	if err != nil {
		return sdkgo.NewMutationBranch(ReceiveMessagesBranchInvalidResponse, batch, failurePointer(receiveMessagesOperationID, sdkgo.FailureProtocol,
			"SQS returned an invalid receive batch: "+err.Error()), receipt)
	}
	batch.Messages = messages
	if len(messages) == 0 {
		return sdkgo.NewMutationBranch(ReceiveMessagesBranchEmpty, batch, nil, receipt)
	}
	return sdkgo.NewMutationBranch(ReceiveMessagesBranchReceived, batch, nil, receipt)
}

// buildRequest resolves the queue and the connection defaults and checks every receive parameter.
func (operation ReceiveMessagesOperation) buildRequest(input ReceiveMessagesInput) (queueAddress, receiveMessageRequest, error) {
	client := operation.client
	queue, err := client.resolveQueue(input.QueueURL)
	if err != nil {
		return queueAddress{}, receiveMessageRequest{}, err
	}
	maxMessages := input.MaxMessages
	if maxMessages == 0 {
		maxMessages = client.maxMessagesPerReceive
	}
	if maxMessages < 1 || maxMessages > maxMessagesPerReceiveLimit {
		return queueAddress{}, receiveMessageRequest{}, fmt.Errorf("maxMessages must be from 1 to %d, or zero for the connection's maxMessagesPerReceive", maxMessagesPerReceiveLimit)
	}
	waitTimeSeconds := client.receiveWaitSeconds
	if input.WaitTimeSeconds != nil {
		waitTimeSeconds = *input.WaitTimeSeconds
	}
	if waitTimeSeconds < 0 || waitTimeSeconds > maxWaitTimeSeconds {
		return queueAddress{}, receiveMessageRequest{}, fmt.Errorf("waitTimeSeconds must be from 0 to %d", maxWaitTimeSeconds)
	}
	if timeout := input.VisibilityTimeoutSeconds; timeout != nil && (*timeout < 0 || *timeout > maxVisibilityTimeoutSeconds) {
		return queueAddress{}, receiveMessageRequest{}, fmt.Errorf("visibilityTimeoutSeconds must be from 0 to %d", maxVisibilityTimeoutSeconds)
	}
	return queue, receiveMessageRequest{
		QueueURL: queue.url, MaxNumberOfMessages: maxMessages, WaitTimeSeconds: waitTimeSeconds, VisibilityTimeout: input.VisibilityTimeoutSeconds,
		AttributeNames: receivedSystemAttributeNames, MessageSystemAttributeNames: receivedSystemAttributeNames,
		MessageAttributeNames: []string{"All"},
	}, nil
}

// decodeReceiveMessageResponse validates an untrusted ReceiveMessage document and each message's body digest.
func decodeReceiveMessageResponse(contents []byte, maxMessages int) ([]Message, error) {
	var document receiveMessageResponse
	if err := json.Unmarshal(contents, &document); err != nil {
		return nil, errors.New("the document is not a ReceiveMessage response")
	}
	if len(document.Messages) > maxMessages {
		return nil, errors.New("the batch holds more messages than requested")
	}
	messages := make([]Message, 0, len(document.Messages))
	for _, received := range document.Messages {
		message, err := decodeReceivedMessage(received)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func decodeReceivedMessage(received receivedMessage) (Message, error) {
	if received.MessageID == "" || validateMessageID(received.MessageID) != nil {
		return Message{}, errors.New("a message ID is missing or not printable")
	}
	if validateReceiptHandle(received.ReceiptHandle) != nil {
		return Message{}, errors.New("a receipt handle is missing or not printable")
	}
	if received.Body == nil || !utf8.ValidString(*received.Body) {
		return Message{}, errors.New("a message body is missing or not UTF-8")
	}
	digest := md5.Sum([]byte(*received.Body))
	if received.MD5OfBody != hex.EncodeToString(digest[:]) {
		return Message{}, errors.New("a message body does not match its MD5 digest")
	}
	message := Message{
		MessageID: received.MessageID, ReceiptHandle: received.ReceiptHandle, Body: *received.Body, MD5OfBody: received.MD5OfBody,
		MessageGroupID: received.Attributes["MessageGroupId"], MessageDeduplicationID: received.Attributes["MessageDeduplicationId"],
		SequenceNumber: received.Attributes["SequenceNumber"],
	}
	var err error
	if message.ReceiveCount, err = strconv.Atoi(received.Attributes["ApproximateReceiveCount"]); err != nil || message.ReceiveCount < 1 {
		return Message{}, errors.New("a receive count is not a positive integer")
	}
	if message.SentAt, err = parseEpochMilliseconds(received.Attributes["SentTimestamp"]); err != nil {
		return Message{}, errors.New("a sent timestamp is not epoch milliseconds")
	}
	if message.FirstReceivedAt, err = parseEpochMilliseconds(received.Attributes["ApproximateFirstReceiveTimestamp"]); err != nil {
		return Message{}, errors.New("a first-receive timestamp is not epoch milliseconds")
	}
	for name, value := range received.MessageAttributes {
		if !messageAttributeNamePattern.MatchString(name) || value.DataType == "" || !utf8.ValidString(value.DataType) ||
			!utf8.ValidString(value.StringValue) {
			return Message{}, errors.New("a message attribute has an invalid name, data type, or value")
		}
		if message.MessageAttributes == nil {
			message.MessageAttributes = map[string]MessageAttributeValue{}
		}
		message.MessageAttributes[name] = MessageAttributeValue{DataType: value.DataType, StringValue: value.StringValue, BinaryValue: value.BinaryValue}
	}
	return message, nil
}

// parseEpochMilliseconds reads an optional SQS timestamp attribute; blank is the zero time.
func parseEpochMilliseconds(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds < 0 {
		return time.Time{}, errors.New("timestamp is not epoch milliseconds")
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}
