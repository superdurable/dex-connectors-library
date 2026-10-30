// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"fmt"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const changeMessageVisibilityOperationID = "changeMessageVisibility"

// ChangeMessageVisibilityInput releases one received message or hides it for longer.
type ChangeMessageVisibilityInput struct {
	// QueueURL is the queue the message came from; blank uses the connection's defaultQueueUrl.
	QueueURL string `json:"queueUrl,omitempty"`
	// ReceiptHandle is the handle of the message's latest delivery, from receiveMessages.
	ReceiptHandle string `json:"receiptHandle"`
	// MessageID optionally names the message in the Result and receipt; it is not sent to SQS.
	MessageID string `json:"messageId,omitempty"`
	// VisibilityTimeoutSeconds hides the message for this many seconds from now, from 0 to 43200. Zero
	// releases it to other consumers at once. SQS caps a message's total in-flight time at 12 hours from its
	// first receive.
	VisibilityTimeoutSeconds int `json:"visibilityTimeoutSeconds"`
}

// ChangedMessageVisibility records the visibility timeout SQS accepted.
type ChangedMessageVisibility struct {
	// QueueURL is the message's queue.
	QueueURL string `json:"queueUrl"`
	// MessageID is the input MessageID.
	MessageID string `json:"messageId,omitempty"`
	// VisibilityTimeoutSeconds is the timeout SQS accepted.
	VisibilityTimeoutSeconds int `json:"visibilityTimeoutSeconds"`
	// VisibleAfter is when the message becomes visible again, measured from the Worker clock when SQS answered.
	VisibleAfter time.Time `json:"visibleAfter"`
}

// ChangeMessageVisibilityOperation implements the changeMessageVisibility connector operation.
type ChangeMessageVisibilityOperation struct{ client *Client }

// changeMessageVisibilityRequest is the ChangeMessageVisibility JSON request.
type changeMessageVisibilityRequest struct {
	QueueURL          string `json:"QueueUrl"`
	ReceiptHandle     string `json:"ReceiptHandle"`
	VisibilityTimeout int    `json:"VisibilityTimeout"`
}

// Definition returns the immutable connector operation definition.
func (ChangeMessageVisibilityOperation) Definition() sdkgo.MutationDefinition {
	return ChangeMessageVisibilityDefinition
}

// IdempotencyKey derives the key from the stable connector call ID; SQS needs none, because a repeated
// change sets the same timeout again.
func (ChangeMessageVisibilityOperation) IdempotencyKey(callID sdkgo.CallID, _ ChangeMessageVisibilityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one ChangeMessageVisibility request. Every ambiguous outcome returns Retry: a repeated change
// restarts the same timeout from the later attempt, which only delays a release by the time between attempts.
func (operation ChangeMessageVisibilityOperation) Invoke(call sdkgo.Call, input ChangeMessageVisibilityInput) sdkgo.MutationAttempt[ChangedMessageVisibility] {
	client := operation.client
	queue, err := validateAcknowledgementInput(client, input.QueueURL, input.ReceiptHandle, input.MessageID)
	if err == nil && (input.VisibilityTimeoutSeconds < 0 || input.VisibilityTimeoutSeconds > maxVisibilityTimeoutSeconds) {
		err = fmt.Errorf("visibilityTimeoutSeconds must be from 0 to %d", maxVisibilityTimeoutSeconds)
	}
	if err != nil {
		return sdkgo.NewMutationBranch(ChangeMessageVisibilityBranchDefect, ChangedMessageVisibility{},
			defectFailure(changeMessageVisibilityOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, changeMessageVisibilityOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(ChangeMessageVisibilityBranchDefect, ChangedMessageVisibility{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	changed := ChangedMessageVisibility{QueueURL: queue.url, MessageID: input.MessageID, VisibilityTimeoutSeconds: input.VisibilityTimeoutSeconds}
	requestID, classified := client.sendAcknowledgement(session, changeMessageVisibilityOperationID, "visibility change", "ChangeMessageVisibility",
		changeMessageVisibilityRequest{QueueURL: queue.url, ReceiptHandle: input.ReceiptHandle, VisibilityTimeout: input.VisibilityTimeoutSeconds})
	receipt := client.receipt(call, requestID, messageObjectID(queue, input.MessageID))
	if classified != nil {
		return mutationAttemptForFailure(*classified, receipt, ChangeMessageVisibilityBranchReceiptHandleInvalid,
			ChangeMessageVisibilityBranchProviderRejected, ChangeMessageVisibilityBranchDefect, changed)
	}
	changed.VisibleAfter = receipt.ObservedAt.Add(time.Duration(input.VisibilityTimeoutSeconds) * time.Second)
	return sdkgo.NewMutationBranch(ChangeMessageVisibilityBranchChanged, changed, nil, receipt)
}
