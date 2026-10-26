// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	// MaxReplyTextBytes bounds the text of one reply or note; it is the connector's limit, not Help Scout's.
	MaxReplyTextBytes     = 64 << 10
	maxReplyReadBackPages = 10
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
	// ThreadID is the confirmed thread's ID. It is zero when no thread was confirmed.
	ThreadID int64 `json:"threadId,omitempty"`
	// IsInternalNote reports that the thread is a note rather than a customer reply.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// CustomerID is the customer a reply was sent to; zero for a note or when read-back cannot identify the customer.
	CustomerID int64 `json:"customerId,omitempty"`
}

// ReplyToConversationOperation implements the replyToConversation Mutation. Build it with
// Client.ReplyToConversation.
type ReplyToConversationOperation struct{ client *Client }

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
// key; the fence permits another send only after the immediately preceding attempt proved non-application.
func (ReplyToConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fences one thread write across Dex retries. A later attempt reads back a saved thread ID;
// without one, an ambiguous sent write selects uncertain.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ConversationReply] {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	client := operation.client
	output := ConversationReply{ConversationID: input.ConversationID, IsInternalNote: input.IsInternalNote, CustomerID: input.CustomerID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, ReplyToConversationDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	switch decision := fence.Decide(); decision.Kind {
	case singledispatch.FailClosed:
		return client.uncertainReply(call, output, "the dispatch checkpoint is unavailable, so the thread is not sent again")
	case singledispatch.ReadBackRequired:
		return client.readBackReply(call, output, decision.Marker)
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		if !errors.Is(err, errHelpScoutCredentialsUnavailable) {
			return credentialMutationAttempt(operationID, ReplyToConversationBranchDefect, output, err)
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
				"the thread was not sent and Dex could not persist its retry checkpoint"), 0)
		}
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability, err.Error()), 0)
	}
	if !input.IsInternalNote && input.CustomerID == 0 {
		customerID, attempt, isTerminal := client.readPrimaryCustomerID(call, &credentials, output, fence)
		if isTerminal {
			return attempt
		}
		output.CustomerID = customerID
	}
	request := buildThreadRequest(input, output.CustomerID)
	encodedBody, err := json.Marshal(request.payload)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureLocalDefect, errHelpScoutRequestInvalid.Error()), sdkgo.Receipt{})
	}
	sendAttempt := &replySendAttempt{client: client, credentials: credentials, request: request, encodedBody: encodedBody}
	err = fence.Dispatch(sendAttempt.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
			"the thread was not sent before the Execute dispatch deadline"), 0)
	}
	return client.classifyReplyExchange(call, output, credentials, sendAttempt.response, err, &sendAttempt.observation, fence)
}

type replySendAttempt struct {
	client      *Client
	credentials Credentials
	request     helpScoutRequest
	encodedBody []byte
	response    helpScoutResponse
	observation dispatchObservation
}

func (attempt *replySendAttempt) send(requestContext context.Context) error {
	tracedContext := httptrace.WithClientTrace(requestContext, attempt.observation.clientTrace())
	var err error
	attempt.response, err = attempt.client.sendOnce(tracedContext, attempt.credentials.AccessToken.Reveal(), attempt.request.method,
		helpScoutAPIBaseURL+attempt.request.path, attempt.encodedBody)
	return err
}

// readPrimaryCustomerID reads the conversation before any thread is sent, so every failure is safe to retry.
func (client *Client) readPrimaryCustomerID(
	call sdkgo.Call, credentials *Credentials, output ConversationReply, fence *singledispatch.Fence,
) (int64, sdkgo.MutationAttempt[ConversationReply], bool) {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	readContext, cancel, canRead := boundedReplyReadContext(call.Context)
	defer cancel()
	if !canRead {
		_ = fence.RecordNotSent() // A lost checkpoint fails closed on the next attempt.
		return 0, sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
			"too little attempt time remains to read the primary customer before sending"), 0), true
	}
	response, err := client.send(readContext, call, credentials, helpScoutRequest{method: http.MethodGet, path: conversationPath(output.ConversationID)})
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
	if attempt, isTerminal, isRetry := classifyIdempotentMutationExchange(client, call, operationID, *credentials, response, err, branches, output.ConversationID, output); isTerminal {
		if isRetry {
			_ = fence.RecordNotSent() // A lost checkpoint fails closed on the next attempt.
		}
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

// classifyReplyExchange maps the fenced write without granting resend after evidence of acceptance.
func (client *Client) classifyReplyExchange(
	call sdkgo.Call, output ConversationReply, credentials Credentials, response helpScoutResponse, err error,
	observation *dispatchObservation, fence *singledispatch.Fence,
) sdkgo.MutationAttempt[ConversationReply] {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	isSuccess := response.statusCode >= 200 && response.statusCode < 300
	if err != nil && isSuccess {
		// The Resource-Id header proves creation even when the body is interrupted or unusable.
		err = nil
	}
	switch {
	case err != nil && errors.Is(err, errHelpScoutRequestInvalid):
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureLocalDefect, errHelpScoutRequestInvalid.Error()), sdkgo.Receipt{})
	case err != nil && observation.isProvablyUndispatched():
		_ = fence.RecordCanResend() // A lost checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureTransport,
			"Help Scout could not be reached, so no thread was sent"), 0)
	case err != nil:
		return client.uncertainReply(call, output, "the thread may exist because Help Scout's answer was lost")
	case isSuccess:
		output.ThreadID = parseResourceID(response.header.Get(resourceIDHeader))
		if output.ThreadID == 0 {
			return client.uncertainReply(call, output, "Help Scout accepted the thread without a valid Resource-Id; the thread may exist")
		}
		_ = fence.RecordProviderID(strconv.FormatInt(output.ThreadID, 10)) // A lost ID still forbids another send.
		return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, client.receipt(call, output.ThreadID, ""))
	}
	outcome := client.classifyFailure(operationID, response, credentials)
	receipt := client.receipt(call, output.ConversationID, outcome.logRef)
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout:
		_ = fence.RecordCanResend() // A lost checkpoint fails closed on the next attempt.
		return sdkgo.NewMutationRetry[ConversationReply](outcome.failure, outcome.retryAfter)
	case status == http.StatusNotFound || status == http.StatusGone:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output, &outcome.failure, receipt)
	case status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusConflict:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output, &outcome.failure, receipt)
	default:
		// A 3xx, 408, or 5xx other than 504 can follow a thread Help Scout already created.
		failure := outcome.failure
		failure.Message += "; the thread may exist"
		return sdkgo.NewMutationUncertain(output, failure, receipt)
	}
}

func (client *Client) uncertainReply(call sdkgo.Call, output ConversationReply, message string) sdkgo.MutationAttempt[ConversationReply] {
	return sdkgo.NewMutationUncertain(output, helpScoutFailure(ReplyToConversationDefinition.Operation.OperationID,
		sdkgo.FailureTransport, message), client.receipt(call, output.ConversationID, ""))
}

func (client *Client) readBackReply(call sdkgo.Call, output ConversationReply, marker singledispatch.Marker) sdkgo.MutationAttempt[ConversationReply] {
	threadID := parseResourceID(marker.ProviderID)
	if marker.CallID != call.ID || threadID == 0 {
		return client.uncertainReply(call, output, "an earlier attempt may have sent the thread without a confirmable ID")
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		if errors.Is(err, errHelpScoutCredentialsUnavailable) {
			return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(ReplyToConversationDefinition.Operation.OperationID,
				sdkgo.FailureAvailability, err.Error()), 0)
		}
		return client.uncertainReply(call, output, "the accepted thread could not be confirmed because authorization failed")
	}
	for pageNumber := 1; pageNumber <= maxReplyReadBackPages; pageNumber++ {
		readContext, cancel, canRead := boundedReplyReadContext(call.Context)
		if !canRead {
			cancel()
			return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(ReplyToConversationDefinition.Operation.OperationID,
				sdkgo.FailureAvailability, "too little attempt time remains to confirm the accepted thread"), 0)
		}
		response, readErr := client.send(readContext, call, &credentials, helpScoutRequest{method: http.MethodGet,
			path: conversationPath(output.ConversationID) + "/threads", query: url.Values{"page": {strconv.Itoa(pageNumber)}}})
		cancel()
		if errors.Is(readErr, errHelpScoutResponseTooLarge) || errors.Is(readErr, errHelpScoutResponseMalformed) || errors.Is(readErr, errHelpScoutRequestInvalid) {
			return client.uncertainReply(call, output, "Help Scout returned an unusable thread read-back")
		}
		if readErr != nil || response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusRequestTimeout || response.statusCode >= 500 {
			return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(ReplyToConversationDefinition.Operation.OperationID,
				sdkgo.FailureAvailability, "Help Scout thread read-back is temporarily unavailable"), 0)
		}
		if response.statusCode != http.StatusOK {
			return client.uncertainReply(call, output, "Help Scout did not confirm the accepted thread")
		}
		var page threadPageWire
		if decodeHelpScoutJSON(response.body, &page) != nil || len(page.Embedded.Threads) > maxThreadsPerPage {
			return client.uncertainReply(call, output, "Help Scout returned an unusable thread read-back")
		}
		for _, thread := range page.Embedded.Threads {
			if thread.ID == threadID {
				output.ThreadID = threadID
				if !output.IsInternalNote && output.CustomerID == 0 {
					if thread.Customer != nil && thread.Customer.ID > 0 {
						output.CustomerID = thread.Customer.ID
					} else {
						output.CustomerID = client.readBackPrimaryCustomerID(call, &credentials, output.ConversationID)
					}
				}
				return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, client.receipt(call, threadID, ""))
			}
		}
		if page.Links.Next == nil {
			break
		}
	}
	return client.uncertainReply(call, output, "Help Scout did not confirm the accepted thread in its thread pages")
}

func (client *Client) readBackPrimaryCustomerID(call sdkgo.Call, credentials *Credentials, conversationID int64) int64 {
	readContext, cancel, canRead := boundedReplyReadContext(call.Context)
	defer cancel()
	if !canRead {
		return 0
	}
	response, err := client.send(readContext, call, credentials, helpScoutRequest{method: http.MethodGet, path: conversationPath(conversationID)})
	if err != nil || response.statusCode != http.StatusOK {
		return 0
	}
	conversation, err := decodeConversationBody(response.body, conversationID)
	if err != nil || conversation.PrimaryCustomer == nil {
		return 0
	}
	return conversation.PrimaryCustomer.ID
}

func boundedReplyReadContext(stepContext context.Context) (context.Context, context.CancelFunc, bool) {
	if stepContext == nil {
		stepContext = context.Background()
	}
	deadline := time.Now().Add(helpScoutRequestTimeout)
	if attemptDeadline, hasDeadline := stepContext.Deadline(); hasDeadline && attemptDeadline.Add(-time.Second).Before(deadline) {
		deadline = attemptDeadline.Add(-time.Second)
	}
	if time.Until(deadline) < time.Second {
		return stepContext, func() {}, false
	}
	readContext, cancel := context.WithDeadline(stepContext, deadline)
	return readContext, cancel, true
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

// parseResourceID reads a positive thread ID, or zero when Help Scout omits or garbles the header.
func parseResourceID(value string) int64 {
	resourceID, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || resourceID < 1 {
		return 0
	}
	return resourceID
}
