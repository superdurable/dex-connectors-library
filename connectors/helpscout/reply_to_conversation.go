// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxReplyTextBytes bounds the text of one reply or note; it is the connector's limit, not Help Scout's.
	MaxReplyTextBytes = 64 << 10

	// replyDeadlineMargin leaves time to return uncertain before the Execute deadline.
	replyDeadlineMargin = 3 * time.Second
	// minimumReplyRequestTime is the least request time worth dispatching the thread with.
	minimumReplyRequestTime = 5 * time.Second
)

// ReplyToConversationInput is one customer reply or internal note to add to a conversation.
type ReplyToConversationInput struct {
	// ConversationID is the Help Scout conversation ID.
	ConversationID int64 `json:"conversationId"`
	// Text is the reply or note, sent unchanged as Help Scout's thread text; Help Scout stores thread
	// bodies as HTML. It is required and at most MaxReplyTextBytes.
	Text string `json:"text"`
	// IsInternalNote adds a note that only the team sees, through POST /v2/conversations/{id}/notes. False
	// adds a reply that Help Scout emails to the customer, through POST /v2/conversations/{id}/reply.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// CustomerID is the customer a reply is sent to. Zero reads the conversation first and uses its primary
	// customer; a note must leave it zero.
	CustomerID int64 `json:"customerId,omitempty"`
	// Status sets the conversation's status with the thread: active, pending, closed, or spam. Blank keeps
	// Help Scout's default, under which a reply reactivates the conversation.
	Status ConversationStatus `json:"status,omitempty"`
}

// ConversationReply is the thread replyToConversation added.
type ConversationReply struct {
	// ConversationID is the conversation the thread belongs to.
	ConversationID int64 `json:"conversationId"`
	// ThreadID is the new thread's ID from Help Scout's Resource-Id header, or zero when Help Scout
	// created the thread without naming it.
	ThreadID int64 `json:"threadId,omitempty"`
	// IsInternalNote reports that the thread is a note rather than a customer reply.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// CustomerID is the customer a reply was sent to; zero for a note.
	CustomerID int64 `json:"customerId,omitempty"`
}

// ReplyToConversationOperation implements the replyToConversation Mutation. Build it with
// Client.ReplyToConversation.
type ReplyToConversationOperation struct{ client *Client }

// replyDispatchCheckpoint is the Dex heartbeat value recorded before the thread request leaves the Worker.
type replyDispatchCheckpoint struct {
	IsDispatched bool `json:"isHelpScoutThreadDispatched"`
}

type replyRequestWire struct {
	Customer struct {
		ID int64 `json:"id"`
	} `json:"customer"`
	Text   string `json:"text"`
	Status string `json:"status,omitempty"`
}

type noteRequestWire struct {
	Text   string `json:"text"`
	Status string `json:"status,omitempty"`
}

// Definition returns the immutable replyToConversation operation definition.
func (ReplyToConversationOperation) Definition() sdkgo.MutationDefinition {
	return ReplyToConversationDefinition
}

// IdempotencyKey returns the call ID, recorded in the Receipt only. Help Scout documents no idempotency
// key and threads carry no client-supplied ID, which is why the operation sends at most once per Step
// execution instead.
func (ReplyToConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke records a Dex heartbeat checkpoint and then sends the reply or note once. A later attempt of the
// same Step execution that finds the checkpoint selects uncertain without sending. Only a 429, the 504
// that Help Scout documents as safe to retry, or a request that never left the process returns Retry and
// clears the checkpoint. A 5xx other than 504, a timeout, or a lost answer after the request was written
// selects uncertain. The request must answer three seconds before the attempt's deadline, so a slow Help
// Scout selects uncertain rather than a Dex retry.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ConversationReply] {
	attemptStart := time.Now()
	operationID := ReplyToConversationDefinition.Operation.OperationID
	client := operation.client
	output := ConversationReply{ConversationID: input.ConversationID, IsInternalNote: input.IsInternalNote, CustomerID: input.CustomerID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	if hasEarlierReplyDispatch(call) {
		return sdkgo.NewMutationUncertain(output, helpScoutFailure(operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step may have sent the thread, so it is not sent again"), client.receipt(call, input.ConversationID, ""))
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialMutationAttempt(operationID, ReplyToConversationBranchDefect, output, err)
	}
	if !input.IsInternalNote && input.CustomerID == 0 {
		customerID, attempt, isTerminal := client.readPrimaryCustomerID(call, &credentials, output)
		if isTerminal {
			return attempt
		}
		output.CustomerID = customerID
	}
	requestContext, cancel, isStartable := newReplyRequestContext(call.Context, attemptStart)
	defer cancel()
	if !isStartable {
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
			"too little of the Execute timeout remained to send the thread; nothing was sent"), 0)
	}
	if err := call.Context.RecordHeartbeat(replyDispatchCheckpoint{IsDispatched: true}); err != nil {
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
			"Dex did not record the dispatch checkpoint, so nothing was sent to Help Scout"), 0)
	}
	var isDispatched atomic.Bool
	response, err := client.send(requestContext, call, &credentials, buildThreadRequest(input, output.CustomerID), &isDispatched)
	return client.classifyReplyExchange(call, output, credentials, response, err, isDispatched.Load())
}

// readPrimaryCustomerID reads the conversation before any thread is sent, so every failure is safe to retry.
func (client *Client) readPrimaryCustomerID(
	call sdkgo.Call, credentials *Credentials, output ConversationReply,
) (int64, sdkgo.MutationAttempt[ConversationReply], bool) {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	response, err := client.send(call.Context, call, credentials, helpScoutRequest{method: http.MethodGet, path: conversationPath(output.ConversationID)}, nil)
	if err == nil && response.statusCode == http.StatusMovedPermanently {
		message := "the conversation was merged into another; reply to that conversation"
		if mergedIntoID, locationErr := parseConversationLocation(response.header.Get("Location")); locationErr == nil {
			message = fmt.Sprintf("the conversation was merged into conversation %d; reply to that conversation", mergedIntoID)
		}
		return 0, sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureNotFound, message), client.receipt(call, output.ConversationID, "")), true
	}
	branches := failureBranches{
		notFound: ReplyToConversationBranchNotFound, providerRejected: ReplyToConversationBranchProviderRejected,
		invalidResponse: ReplyToConversationBranchProviderRejected,
	}
	if attempt, isTerminal := classifyIdempotentMutationExchange(client, call, operationID, *credentials, response, err, branches, output.ConversationID, output); isTerminal {
		return 0, attempt, true
	}
	conversation, err := decodeConversationBody(response.body, output.ConversationID)
	if err != nil {
		return 0, sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout returned an invalid conversation, so no reply was sent: "+err.Error()),
			client.receipt(call, output.ConversationID, "")), true
	}
	if conversation.PrimaryCustomer == nil {
		return 0, sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, "the conversation has no primary customer to reply to; pass customerId"),
			client.receipt(call, output.ConversationID, "")), true
	}
	return conversation.PrimaryCustomer.ID, sdkgo.MutationAttempt[ConversationReply]{}, false
}

// classifyReplyExchange maps the sent thread: only a provable non-application is retried.
func (client *Client) classifyReplyExchange(
	call sdkgo.Call, output ConversationReply, credentials Credentials, response helpScoutResponse, err error, isDispatched bool,
) sdkgo.MutationAttempt[ConversationReply] {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	isSuccess := response.statusCode >= 200 && response.statusCode < 300
	if err != nil && isSuccess && (errors.Is(err, errHelpScoutResponseTooLarge) || errors.Is(err, errHelpScoutResponseMalformed)) {
		// The thread exists; its body is never read, so an oversized or credential-echoing body changes nothing.
		err = nil
	}
	switch {
	case err != nil && errors.Is(err, errHelpScoutRequestInvalid):
		clearReplyDispatch(call)
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureLocalDefect, errHelpScoutRequestInvalid.Error()), sdkgo.Receipt{})
	case err != nil && !isDispatched:
		clearReplyDispatch(call)
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureTransport,
			"Help Scout could not be reached, so no thread was sent"), 0)
	case err != nil:
		return sdkgo.NewMutationUncertain(output, helpScoutFailure(operationID, sdkgo.FailureTransport,
			"the thread was sent but Help Scout's answer was lost; the thread may exist"), client.receipt(call, output.ConversationID, ""))
	case isSuccess:
		output.ThreadID = parseResourceID(response.header.Get(resourceIDHeader))
		return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, client.receipt(call, output.ThreadID, ""))
	}
	outcome := client.classifyFailure(operationID, response, credentials)
	receipt := client.receipt(call, output.ConversationID, outcome.logRef)
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		clearReplyDispatch(call)
		return sdkgo.NewMutationRetry[ConversationReply](outcome.failure, outcome.retryAfter)
	case status == http.StatusGatewayTimeout:
		// Help Scout documents a 504 as an internal timeout that did not complete and is safe to retry.
		clearReplyDispatch(call)
		return sdkgo.NewMutationRetry[ConversationReply](outcome.failure, outcome.retryAfter)
	case status == http.StatusNotFound || status == http.StatusGone:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output, &outcome.failure, receipt)
	case status >= 400 && status < 500 && status != http.StatusRequestTimeout:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output, &outcome.failure, receipt)
	default:
		// A 3xx, 408, or 5xx other than 504 can follow a thread Help Scout already created.
		failure := outcome.failure
		failure.Message += "; the thread may exist"
		return sdkgo.NewMutationUncertain(output, failure, receipt)
	}
}

func validateReplyToConversationInput(input ReplyToConversationInput) error {
	switch {
	case input.ConversationID < 1:
		return errors.New("conversationId must be a positive Help Scout conversation ID")
	case strings.TrimSpace(input.Text) == "":
		return errors.New("text is required")
	case len(input.Text) > MaxReplyTextBytes:
		return fmt.Errorf("text is longer than %d bytes", MaxReplyTextBytes)
	case input.CustomerID < 0:
		return errors.New("customerId must be a positive Help Scout customer ID, or zero for the primary customer")
	case input.IsInternalNote && input.CustomerID != 0:
		return errors.New("customerId applies only to a customer reply, not to an internal note")
	}
	if input.Status != "" {
		return validateConversationStatus("status", input.Status)
	}
	return nil
}

func buildThreadRequest(input ReplyToConversationInput, customerID int64) helpScoutRequest {
	if input.IsInternalNote {
		return helpScoutRequest{method: http.MethodPost, path: conversationPath(input.ConversationID) + "/notes",
			payload: noteRequestWire{Text: input.Text, Status: string(input.Status)}}
	}
	payload := replyRequestWire{Text: input.Text, Status: string(input.Status)}
	payload.Customer.ID = customerID
	return helpScoutRequest{method: http.MethodPost, path: conversationPath(input.ConversationID) + "/reply", payload: payload}
}

// newReplyRequestContext ends the request before the attempt's deadline; false means too little time is left.
func newReplyRequestContext(stepContext context.Context, attemptStart time.Time) (context.Context, context.CancelFunc, bool) {
	if stepContext == nil {
		stepContext = context.Background()
	}
	deadline := attemptStart.Add(ReplyToConversationDefinition.StepDefaults.ExecuteMethodTimeout)
	if contextDeadline, hasDeadline := stepContext.Deadline(); hasDeadline && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	requestDeadline := deadline.Add(-replyDeadlineMargin)
	if time.Until(requestDeadline) < minimumReplyRequestTime {
		return stepContext, func() {}, false
	}
	requestContext, cancel := context.WithDeadline(stepContext, requestDeadline)
	return requestContext, cancel, true
}

// hasEarlierReplyDispatch reports an earlier attempt's checkpoint; an unreadable one counts, avoiding a duplicate.
func hasEarlierReplyDispatch(call sdkgo.Call) bool {
	var checkpoint replyDispatchCheckpoint
	isFound, err := call.Context.GetLastHeartbeatValue(&checkpoint)
	return err != nil || (isFound && checkpoint.IsDispatched)
}

// clearReplyDispatch removes the checkpoint after Help Scout provably added nothing.
func clearReplyDispatch(call sdkgo.Call) {
	// A lost clear leaves the checkpoint set, so the next attempt reports uncertain instead of sending twice.
	_ = call.Context.RecordHeartbeat(nil)
}

// parseResourceID reads a positive thread ID, or zero when Help Scout omits or garbles the header.
func parseResourceID(value string) int64 {
	resourceID, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || resourceID < 1 {
		return 0
	}
	return resourceID
}
