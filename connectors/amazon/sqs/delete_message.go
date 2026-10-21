// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqs

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const deleteMessageOperationID = "deleteMessage"

// DeleteMessageInput acknowledges one received message.
type DeleteMessageInput struct {
	// QueueURL is the queue the message came from; blank uses the connection's defaultQueueUrl.
	QueueURL string `json:"queueUrl,omitempty"`
	// ReceiptHandle is the handle of the message's latest delivery, from receiveMessages.
	ReceiptHandle string `json:"receiptHandle"`
	// MessageID optionally names the message in the Result and receipt; it is not sent to SQS.
	MessageID string `json:"messageId,omitempty"`
}

// DeletedMessage identifies the acknowledged message.
type DeletedMessage struct {
	// QueueURL is the message's queue.
	QueueURL string `json:"queueUrl"`
	// MessageID is the input MessageID.
	MessageID string `json:"messageId,omitempty"`
}

// DeleteMessageOperation implements the deleteMessage connector operation.
type DeleteMessageOperation struct{ client *Client }

// deleteMessageRequest is the DeleteMessage JSON request.
type deleteMessageRequest struct {
	QueueURL      string `json:"QueueUrl"`
	ReceiptHandle string `json:"ReceiptHandle"`
}

// Definition returns the immutable connector operation definition.
func (DeleteMessageOperation) Definition() sdkgo.MutationDefinition { return DeleteMessageDefinition }

// IdempotencyKey derives the key from the stable connector call ID; SQS takes none, because a repeated
// deletion with the same receipt handle leaves the message deleted.
func (DeleteMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one DeleteMessage request. Every ambiguous outcome returns Retry, because a repeated deletion
// cannot undo the first. A repeat after a lost success response selects receiptHandleInvalid on a server
// that forgets the handle of a deleted message, such as ElasticMQ.
func (operation DeleteMessageOperation) Invoke(call sdkgo.Call, input DeleteMessageInput) sdkgo.MutationAttempt[DeletedMessage] {
	client := operation.client
	queue, err := validateAcknowledgementInput(client, input.QueueURL, input.ReceiptHandle, input.MessageID)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteMessageBranchDefect, DeletedMessage{}, defectFailure(deleteMessageOperationID, err), sdkgo.Receipt{})
	}
	session, cancel, failure := client.startSession(call, deleteMessageOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteMessageBranchDefect, DeletedMessage{}, failure, sdkgo.Receipt{})
	}
	defer cancel()
	deleted := DeletedMessage{QueueURL: queue.url, MessageID: input.MessageID}
	requestID, classified := client.sendAcknowledgement(session, deleteMessageOperationID, "message deletion", "DeleteMessage",
		deleteMessageRequest{QueueURL: queue.url, ReceiptHandle: input.ReceiptHandle})
	receipt := client.receipt(call, requestID, messageObjectID(queue, input.MessageID))
	if classified != nil {
		return mutationAttemptForFailure(*classified, receipt, DeleteMessageBranchReceiptHandleInvalid, DeleteMessageBranchProviderRejected,
			DeleteMessageBranchDefect, deleted)
	}
	return sdkgo.NewMutationBranch(DeleteMessageBranchDeleted, deleted, nil, receipt)
}

// validateAcknowledgementInput resolves the queue and checks the receipt handle and optional message ID.
func validateAcknowledgementInput(client *Client, queueURL string, receiptHandle string, messageID string) (queueAddress, error) {
	queue, err := client.resolveQueue(queueURL)
	if err != nil {
		return queueAddress{}, err
	}
	if err := validateReceiptHandle(receiptHandle); err != nil {
		return queueAddress{}, err
	}
	if err := validateMessageID(messageID); err != nil {
		return queueAddress{}, err
	}
	return queue, nil
}

// messageObjectID names the message in a receipt as <queue URL>#<message ID>, or the queue URL alone.
func messageObjectID(queue queueAddress, messageID string) string {
	if messageID == "" {
		return queue.url
	}
	return queue.url + "#" + messageID
}
