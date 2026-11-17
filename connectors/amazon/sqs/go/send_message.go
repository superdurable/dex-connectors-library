// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const sendMessageOperationID = "sendMessage"

// SendMessageInput sends one UTF-8 message to a standard or FIFO queue.
type SendMessageInput struct {
	// QueueURL is the full URL from SQS Console > Queues > selected queue; blank uses defaultQueueUrl.
	QueueURL string `json:"queueUrl,omitempty"`
	// Body is nonempty UTF-8 text, at most 256 KiB for the default SQS queue size limit.
	Body string `json:"body"`
	// MessageGroupID is required for FIFO queues and optional for standard fair queues.
	MessageGroupID string `json:"messageGroupId,omitempty"`
}

// SentMessage identifies the message SQS accepted; delivery to a consumer is asynchronous.
type SentMessage struct {
	// QueueURL is the destination queue.
	QueueURL string `json:"queueUrl"`
	// MessageID is the SQS-assigned message identity.
	MessageID string `json:"messageId"`
	// MD5OfBody is the SQS checksum of the sent body.
	MD5OfBody string `json:"md5OfBody"`
}

// SendMessageOperation implements a bounded send to a standard or FIFO queue.
type SendMessageOperation struct{ client *Client }

// SendMessage returns the sendMessage Mutation bound to this client.
func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client: client} }

// Definition returns the generated Mutation contract.
func (SendMessageOperation) Definition() sdkgo.MutationDefinition { return SendMessageDefinition }

// IdempotencyKey identifies this Step; FIFO queues also receive it as MessageDeduplicationId.
func (SendMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fences each possible dispatch and returns uncertain after the send cap.
func (operation SendMessageOperation) Invoke(call sdkgo.Call, input SendMessageInput) sdkgo.MutationAttempt[SentMessage] {
	client := operation.client
	queue, err := client.resolveQueue(input.QueueURL)
	if err != nil || input.Body == "" || len(input.Body) > 256*1024 || !utf8.ValidString(input.Body) ||
		(queue.isFIFO && input.MessageGroupID == "") || len(input.MessageGroupID) > 128 ||
		(input.MessageGroupID != "" && !isPrintableToken(input.MessageGroupID)) {
		return sendMessageBranch(SendMessageBranchDefect, sdkgo.FailureValidation, "SQS queue URL, message body, or group ID is invalid")
	}
	fence, err := singledispatch.NewFence(call, SendMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sendMessageBranch(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "SQS send fence could not be initialized")
	}
	switch fence.Decide().Kind {
	case singledispatch.FailClosed, singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(SentMessage{}, newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS send outcome could not be confirmed"), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, sendMessageOperationID)
	if failure != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationUncertain(SentMessage{}, newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(SendMessageBranchDefect, SentMessage{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	request := sendMessageRequest{QueueURL: queue.url, MessageBody: input.Body, MessageGroupID: input.MessageGroupID}
	if queue.isFIFO {
		request.MessageDeduplicationID = string(call.IdempotencyKey)
	}
	send := &sqsMessageSend{client: client, session: session, request: request}
	if fence.Dispatch(send.dispatch) != nil {
		return sdkgo.NewMutationUncertain(SentMessage{}, newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS send dispatch could not be confirmed"), sdkgo.Receipt{})
	}
	response, err := send.response, send.err
	if errors.Is(err, errRequestNotBuilt) {
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationUncertain(SentMessage{}, newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS unsent checkpoint could not be recorded"), sdkgo.Receipt{})
		}
		return sendMessageBranch(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "SQS send request could not be built")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[SentMessage](transportFailure(sendMessageOperationID, "message send"), 0)
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		classified := classifyErrorResponse(sendMessageOperationID, "message send", client.readErrorResponse(response, session.credentials))
		if classified.outcome == outcomeRetry {
			if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
				if fence.RecordCanResend() != nil {
					return sdkgo.NewMutationUncertain(SentMessage{}, newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS resend checkpoint could not be recorded"), sdkgo.Receipt{})
				}
			}
			return sdkgo.NewMutationRetry[SentMessage](classified.failure, classified.retryAfter)
		}
		return sdkgo.NewMutationBranch(SendMessageBranchProviderRejected, SentMessage{}, &classified.failure, sdkgo.Receipt{})
	}
	contents, err := providerhttp.ReadBoundedBody(response.Body, 64*1024)
	if err != nil {
		return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS send response was interrupted"), 0)
	}
	var accepted struct {
		MessageID        string `json:"MessageId"`
		MD5OfMessageBody string `json:"MD5OfMessageBody"`
	}
	if json.Unmarshal(contents, &accepted) != nil || accepted.MessageID == "" || validateMessageID(accepted.MessageID) != nil ||
		accepted.MD5OfMessageBody != bodyMD5(input.Body) || containsCredential(accepted.MessageID, session.credentials) {
		return sdkgo.NewMutationUncertain(SentMessage{}, newFailure(sendMessageOperationID, sdkgo.FailureProtocol, "SQS accepted the send without a usable message ID or checksum"), sdkgo.Receipt{})
	}
	value := SentMessage{QueueURL: queue.url, MessageID: accepted.MessageID, MD5OfBody: accepted.MD5OfMessageBody}
	return sdkgo.NewMutationBranch(SendMessageBranchSent, value, nil, client.receipt(call, safeRequestID(response.Header), messageObjectID(queue, accepted.MessageID)))
}

type sendMessageRequest struct {
	QueueURL               string `json:"QueueUrl"`
	MessageBody            string `json:"MessageBody"`
	MessageGroupID         string `json:"MessageGroupId,omitempty"`
	MessageDeduplicationID string `json:"MessageDeduplicationId,omitempty"`
}

type sqsMessageSend struct {
	client   *Client
	session  *operationSession
	request  sendMessageRequest
	response *http.Response
	err      error
}

func (send *sqsMessageSend) dispatch(requestContext context.Context) error {
	send.session.context = requestContext
	send.response, send.err = send.client.exchange(send.session, "SendMessage", send.request)
	return nil
}

func bodyMD5(body string) string {
	digest := md5.Sum([]byte(body))
	return hex.EncodeToString(digest[:])
}

func sendMessageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentMessage] {
	failure := newFailure(sendMessageOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, SentMessage{}, &failure, sdkgo.Receipt{})
}
