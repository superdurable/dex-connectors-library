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

// Invoke fences one thread write across Dex retries under the manifest send cap. A later attempt reads
// back a saved thread ID and never resends it; without one, an unknown outcome resends while the cap allows.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ConversationReply] {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	client := operation.client
	output := ConversationReply{ConversationID: input.ConversationID, IsInternalNote: input.IsInternalNote, CustomerID: input.CustomerID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, ReplyToConversationDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
			"the dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
	case singledispatch.ReadBackRequired:
		return client.readBackReply(call, output, decision.Marker)
	case singledispatch.ReadBackThenResendAllowed:
		// Help Scout returns a thread ID only for a created thread, so a saved one is read back and never resent.
		if decision.Marker.ProviderID != "" {
			return client.readBackReply(call, output, decision.Marker)
		}
	}
	permit := replySendPermit{
		fence:                   fence,
		isFirstSend:             decision.MaximumPriorSends == 0,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		if !errors.Is(err, errHelpScoutCredentialsUnavailable) {
			if !permit.isFirstSend {
				return client.uncertainReply(call, output, "an earlier attempt may have sent the thread, and authorization failed before a resend")
			}
			return credentialMutationAttempt(operationID, ReplyToConversationBranchDefect, output, err)
		}
		// After a possible earlier send, notSent would hide that send, so nothing is recorded.
		if permit.isFirstSend {
			if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
				return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
					"the thread was not sent and Dex could not persist its retry checkpoint"), 0)
			}
		}
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability, err.Error()), 0)
	}
	if !input.IsInternalNote && input.CustomerID == 0 {
		customerID, attempt, isTerminal := client.readPrimaryCustomerID(call, &credentials, output, permit)
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
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) || errors.Is(err, singledispatch.ErrDispatchNotAllowed) {
		// Dispatch refused before sending, so nothing reached Help Scout in this attempt.
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
			"the thread was not sent before the Execute dispatch deadline"), 0)
	}
	return client.classifyReplyExchange(call, output, credentials, sendAttempt.response, err, &sendAttempt.observation, permit)
}

// replySendPermit is a fence that allows this attempt to send, with its send-cap facts.
type replySendPermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
	// canResendUnknownOutcome means this attempt's send leaves room under the cap for one more.
	canResendUnknownOutcome bool
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

// readPrimaryCustomerID reads the conversation before this attempt's send. A terminal read after a
// possible earlier send selects uncertain, because a refusal branch would hide that send.
func (client *Client) readPrimaryCustomerID(
	call sdkgo.Call, credentials *Credentials, output ConversationReply, permit replySendPermit,
) (int64, sdkgo.MutationAttempt[ConversationReply], bool) {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	fence := permit.fence
	readContext, cancel, canRead := boundedReplyReadContext(call.Context)
	defer cancel()
	if !canRead {
		_ = fence.RecordNotSent() // Refused after a possible earlier send, so that send stays counted.
		return 0, sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureAvailability,
			"too little attempt time remains to read the primary customer before sending"), 0), true
	}
	response, err := client.send(readContext, call, credentials, helpScoutRequest{method: http.MethodGet, path: conversationPath(output.ConversationID)})
	customerID, attempt, isTerminal, isRefusal := client.decodePrimaryCustomerID(call, credentials, output, fence, response, err)
	if isRefusal && !permit.isFirstSend {
		return 0, client.uncertainReply(call, output, "an earlier attempt may have sent the thread, and the conversation read before a resend failed"), true
	}
	return customerID, attempt, isTerminal
}

// decodePrimaryCustomerID maps the customer read; isRefusal marks a terminal refusal branch.
func (client *Client) decodePrimaryCustomerID(
	call sdkgo.Call, credentials *Credentials, output ConversationReply, fence *singledispatch.Fence, response helpScoutResponse, err error,
) (int64, sdkgo.MutationAttempt[ConversationReply], bool, bool) {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	if err == nil && response.statusCode == http.StatusMovedPermanently {
		message := "the conversation was merged into another; reply to that conversation"
		if mergedIntoID, locationErr := parseConversationLocation(response.header.Get("Location")); locationErr == nil {
			message = fmt.Sprintf("the conversation was merged into conversation %d; reply to that conversation", mergedIntoID)
		}
		return 0, sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureNotFound, message), client.receipt(call, output.ConversationID, "")), true, true
	}
	branches := failureBranches{
		notFound: ReplyToConversationBranchNotFound, providerRejected: ReplyToConversationBranchProviderRejected,
		invalidResponse: ReplyToConversationBranchProviderRejected,
	}
	if attempt, isTerminal, isRetry := classifyIdempotentMutationExchange(client, call, operationID, *credentials, response, err, branches, output.ConversationID, output); isTerminal {
		if isRetry {
			_ = fence.RecordNotSent() // Refused after a possible earlier send, so that send stays counted.
		}
		return 0, attempt, true, !isRetry
	}
	conversation, err := decodeConversationBody(response.body, output.ConversationID)
	if err != nil {
		return 0, sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureProtocol, "Help Scout returned an invalid conversation, so no reply was sent: "+err.Error()),
			client.receipt(call, output.ConversationID, "")), true, true
	}
	if conversation.PrimaryCustomer == nil {
		return 0, sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output,
			helpScoutFailurePointer(operationID, sdkgo.FailureValidation, "the conversation has no primary customer to reply to; pass customerId"),
			client.receipt(call, output.ConversationID, "")), true, true
	}
	return conversation.PrimaryCustomer.ID, sdkgo.MutationAttempt[ConversationReply]{}, false, false
}

// classifyReplyExchange maps the fenced write. Only a definite refusal selects a refusal branch; any
// other outcome may have created the thread, so it resends while the send cap allows.
func (client *Client) classifyReplyExchange(
	call sdkgo.Call, output ConversationReply, credentials Credentials, response helpScoutResponse, err error,
	observation *dispatchObservation, permit replySendPermit,
) sdkgo.MutationAttempt[ConversationReply] {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	isSuccess := response.statusCode >= 200 && response.statusCode < 300
	if err != nil && isSuccess {
		// The Resource-Id header proves creation even when the body is interrupted or unusable.
		err = nil
	}
	switch {
	case err != nil && errors.Is(err, errHelpScoutRequestInvalid):
		failure := helpScoutFailure(operationID, sdkgo.FailureLocalDefect, errHelpScoutRequestInvalid.Error())
		if !permit.isFirstSend {
			// defect would hide an earlier possible send.
			return client.resendOrSelectUncertain(call, output, permit, failure, 0, sdkgo.Receipt{})
		}
		return client.rejectReply(call, output, permit, ReplyToConversationBranchDefect, failure, sdkgo.Receipt{})
	case err != nil && observation.isProvablyUndispatched():
		_ = permit.fence.RecordCanResend() // A lost checkpoint leaves the claim, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(operationID, sdkgo.FailureTransport,
			"Help Scout could not be reached, so no thread was sent"), 0)
	case err != nil:
		return client.resendOrSelectUncertain(call, output, permit, helpScoutFailure(operationID, sdkgo.FailureTransport,
			"the thread may exist because Help Scout's answer was lost"), 0, client.receipt(call, output.ConversationID, ""))
	case isSuccess:
		output.ThreadID = parseResourceID(response.header.Get(resourceIDHeader))
		if output.ThreadID == 0 {
			// Without a thread ID the answer cannot be read back, so it counts as an unknown outcome.
			return client.resendOrSelectUncertain(call, output, permit, helpScoutFailure(operationID, sdkgo.FailureTransport,
				"Help Scout answered the thread without a valid Resource-Id; the thread may exist"), 0, client.receipt(call, output.ConversationID, ""))
		}
		_ = permit.fence.RecordProviderID(strconv.FormatInt(output.ThreadID, 10)) // A lost ID still forbids another send.
		return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, client.receipt(call, output.ThreadID, ""))
	}
	outcome := client.classifyFailure(operationID, response, credentials)
	receipt := client.receipt(call, output.ConversationID, outcome.logRef)
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable:
		_ = permit.fence.RecordCanResend() // A lost checkpoint leaves the claim, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[ConversationReply](outcome.failure, outcome.retryAfter)
	case status == http.StatusNotFound:
		return client.rejectReply(call, output, permit, ReplyToConversationBranchNotFound, outcome.failure, receipt)
	case isDefiniteRefusalStatus(status):
		return client.rejectReply(call, output, permit, ReplyToConversationBranchProviderRejected, outcome.failure, receipt)
	default:
		// A 3xx, 408, 409, 410, another unlisted status, or a 5xx other than 503 can follow a created thread.
		failure := outcome.failure
		failure.Message += "; the thread may exist"
		return client.resendOrSelectUncertain(call, output, permit, failure, outcome.retryAfter, receipt)
	}
}

// isDefiniteRefusalStatus reports a status that proves Help Scout created nothing. Help Scout
// documents 412 and 423 as a locked conversation or customer that refuses the change.
func isDefiniteRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusPreconditionFailed, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity, http.StatusLocked:
		return true
	default:
		return false
	}
}

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func (client *Client) resendOrSelectUncertain(call sdkgo.Call, output ConversationReply, permit replySendPermit, failure sdkgo.Failure, retryAfter time.Duration, receipt sdkgo.Receipt) sdkgo.MutationAttempt[ConversationReply] {
	if permit.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[ConversationReply](failure, retryAfter)
	}
	if receipt.CallID == "" {
		receipt = client.receipt(call, output.ConversationID, "")
	}
	return sdkgo.NewMutationUncertain(output, failure, receipt)
}

// rejectReply selects a refusal branch only when no earlier send of this call may have applied.
func (client *Client) rejectReply(call sdkgo.Call, output ConversationReply, permit replySendPermit, branch sdkgo.BranchID, failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[ConversationReply] {
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(output, failure, client.receipt(call, output.ConversationID, ""))
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[ConversationReply](helpScoutFailure(ReplyToConversationDefinition.Operation.OperationID,
			sdkgo.FailureAvailability, "the refusal checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationBranch(branch, output, &failure, receipt)
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
