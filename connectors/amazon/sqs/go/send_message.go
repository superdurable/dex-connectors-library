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
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureAvailability, "SQS send checkpoint is unavailable"), 0)
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(SentMessage{}, newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS send outcome could not be confirmed"), sdkgo.Receipt{})
	}
	session, cancel, failure, shouldRetry := client.startSession(call, sendMessageOperationID)
	if failure != nil {
		if shouldRetry {
			return sdkgo.NewMutationRetry[SentMessage](*failure, 0)
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !errors.Is(checkpointErr, singledispatch.ErrDispatchNotAllowed) {
			return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureAvailability, "SQS unsent checkpoint could not be recorded"), 0)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationRetry[SentMessage](*failure, 0)
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
		return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureAvailability, "SQS send dispatch checkpoint is unavailable"), 0)
	}
	response, err := send.response, send.err
	if errors.Is(err, errRequestNotBuilt) {
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureAvailability, "SQS unsent checkpoint could not be recorded"), 0)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureLocalDefect, "SQS send request could not be built"), 0)
		}
		return sendMessageBranch(SendMessageBranchDefect, sdkgo.FailureLocalDefect, "SQS send request could not be built")
	}
	if err != nil {
		return limitedSQSUnknown(call, decision, transportFailure(sendMessageOperationID, "message send"))
	}
	defer closeResponseBody(response)
	if !isSuccessStatus(response.StatusCode) {
		errorResponse := client.readErrorResponse(response, session.credentials)
		classified := classifyErrorResponse(sendMessageOperationID, "message send", errorResponse)
		if classified.outcome == outcomeRetry {
			if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
				if fence.RecordCanResend() != nil {
					return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureAvailability, "SQS resend checkpoint could not be recorded"), 0)
				}
				return sdkgo.NewMutationRetry[SentMessage](classified.failure, classified.retryAfter)
			}
			return limitedSQSUnknown(call, decision, classified.failure)
		}
		if decision.MaximumPriorSends == 0 && isDefiniteSQSRefusal(errorResponse) {
			if fence.RecordCanResend() != nil {
				return sdkgo.NewMutationRetry[SentMessage](newFailure(sendMessageOperationID, sdkgo.FailureAvailability, "SQS refusal checkpoint is unavailable"), 0)
			}
			return sdkgo.NewMutationBranch(SendMessageBranchProviderRejected, SentMessage{}, &classified.failure, sdkgo.Receipt{})
		}
		return limitedSQSUnknown(call, decision, classified.failure)
	}
	contents, err := providerhttp.ReadBoundedBody(response.Body, 64*1024)
	if err != nil {
		return limitedSQSUnknown(call, decision, newFailure(sendMessageOperationID, sdkgo.FailureTransport, "SQS send response was interrupted"))
	}
	var accepted struct {
		MessageID        string `json:"MessageId"`
		MD5OfMessageBody string `json:"MD5OfMessageBody"`
	}
	if json.Unmarshal(contents, &accepted) != nil || accepted.MessageID == "" || validateMessageID(accepted.MessageID) != nil ||
		accepted.MD5OfMessageBody != bodyMD5(input.Body) || containsCredential(accepted.MessageID, session.credentials) {
		return limitedSQSUnknown(call, decision, newFailure(sendMessageOperationID, sdkgo.FailureProtocol, "SQS send response has no usable message ID or checksum"))
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

func limitedSQSUnknown(call sdkgo.Call, decision singledispatch.Decision, failure sdkgo.Failure) sdkgo.MutationAttempt[SentMessage] {
	if decision.MaximumPriorSends+1 >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(SentMessage{}, failure, sdkgo.Receipt{})
	}
	return sdkgo.NewMutationRetry[SentMessage](failure, 0)
}

func isDefiniteSQSRefusal(response providerErrorResponse) bool {
	switch response.statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return response.code != ""
	default:
		return false
	}
}
