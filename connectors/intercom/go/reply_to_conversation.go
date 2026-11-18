// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	replyToConversationOperation = "replyToConversation"

	// replyReconciliationSkew tolerates a difference between the Worker's and Intercom's clocks.
	replyReconciliationSkew = 5 * time.Minute
)

// ReplyMessageType is Intercom's admin reply type.
type ReplyMessageType string

const (
	// ReplyMessageTypeComment is a reply the customer sees; Intercom delivers it in the conversation's channel, such as email.
	ReplyMessageTypeComment ReplyMessageType = "comment"
	// ReplyMessageTypeNote is an internal note that only teammates see.
	ReplyMessageTypeNote ReplyMessageType = "note"
)

var (
	paragraphBreakPattern = regexp.MustCompile(`\n[ \t]*\n`)
	htmlTagPattern        = regexp.MustCompile(`<[^>]*>`)
)

// ReplyToConversationInput is one admin reply or internal note to add to a conversation.
type ReplyToConversationInput struct {
	// ConversationID is the Intercom conversation ID, a string of digits.
	ConversationID string `json:"conversationId"`
	// AdminID is the Intercom admin the reply is authored by, chosen with the adminPicker unit.
	AdminID string `json:"adminId"`
	// MessageType is comment for a reply the customer sees or note for an internal note. It is required,
	// so a customer-visible message is never sent by default.
	MessageType ReplyMessageType `json:"messageType"`
	// Body is the plain-text message, at most MaxTextBytes. The connector escapes it into Intercom's HTML
	// body, so markup is shown literally, blank lines separate paragraphs, and line breaks are kept.
	Body string `json:"body"`
}

// ReplyToConversationOutput is the reply or note in the conversation.
type ReplyToConversationOutput struct {
	// ConversationID is the conversation the reply belongs to.
	ConversationID string `json:"conversationId"`
	// Part is the reply part. Its ID is empty when Intercom's response did not show the new part.
	Part ConversationPart `json:"part"`
	// ConversationState is the conversation's state after the reply, as Intercom reported it.
	ConversationState ConversationState `json:"conversationState,omitempty"`
	// WasAlreadyApplied reports that an earlier attempt of this Step sent the reply without a confirmed
	// outcome and this attempt found it in the conversation, so nothing was sent again.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
}

// ReplyToConversationOperation is the replyToConversation Mutation.
type ReplyToConversationOperation struct {
	client *Client
}

type replyRequestWire struct {
	MessageType string `json:"message_type"`
	Type        string `json:"type"`
	AdminID     string `json:"admin_id"`
	Body        string `json:"body"`
}

// Definition returns the immutable connector operation definition.
func (ReplyToConversationOperation) Definition() sdkgo.MutationDefinition {
	return ReplyToConversationDefinition
}

// IdempotencyKey uses the stable connector Call ID. Intercom documents no idempotency key, so the key
// only correlates the Receipt; the single-dispatch fence counts sends instead.
func (ReplyToConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /conversations/{id}/reply through the single-dispatch fence under the manifest send
// cap. Whenever an earlier attempt may have sent, it first reads the conversation and reports the
// earlier reply when a part by the same admin with the same type and text exists since the Step's
// first attempt. Only a read-back that shows no such part permits a resend while the cap allows; any
// other unconfirmed read-back, and an absent one at the cap, selects uncertain. A 2xx is never resent.
// Only a definite refusal of a first send selects notFound, providerRejected, or defect.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ReplyToConversationOutput] {
	output := ReplyToConversationOutput{ConversationID: input.ConversationID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, intercomFailurePointer(sdkgo.FailureValidation, replyToConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, ReplyToConversationDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, intercomFailurePointer(sdkgo.FailureLocalDefect, replyToConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](intercomFailure(sdkgo.FailureAvailability, replyToConversationOperation,
			"dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
	}
	hasPossibleEarlierSend := hasPossibleEarlierReplySend(decision)
	credentials, credentialProblem := operation.client.resolveWriteCredentials(call, replyToConversationOperation)
	if credentialProblem != nil {
		return replyCredentialFailureAttempt(fence, hasPossibleEarlierSend, *credentialProblem, output)
	}
	if hasPossibleEarlierSend {
		readBack := operation.readBackReply(call, credentials, input)
		switch {
		case readBack.outcome == replyReadBackFound:
			readBack.output.WasAlreadyApplied = true
			return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, readBack.output, nil, readBack.receipt)
		case readBack.outcome == replyReadBackRetryable:
			return sdkgo.NewMutationRetry[ReplyToConversationOutput](readBack.failure, readBack.retryAfter)
		case readBack.outcome != replyReadBackAbsent || !canResendAfterAbsentReadBack(decision):
			return sdkgo.NewMutationUncertain(readBack.output, intercomFailure(readBack.failure.Kind, replyToConversationOperation,
				"an earlier attempt of this Step may have sent the reply, and "+readBack.failure.Message), readBack.receipt)
		}
		// The read-back found no matching part, so this attempt resends below the cap.
	}
	permit := replySendPermit{fence: fence, isFirstSend: decision.MaximumPriorSends == 0}
	dispatchedAt := operation.client.now()
	send := &replySend{client: operation.client, call: call, credentials: credentials, request: intercomRequest{
		method: http.MethodPost, path: conversationPath(input.ConversationID) + "/reply",
		payload: replyRequestWire{
			MessageType: string(input.MessageType), Type: "admin", AdminID: input.AdminID, Body: plainTextToIntercomHTML(input.Body),
		},
	}}
	if err := fence.Dispatch(send.dispatch); err != nil {
		// Dispatch refused before sending, so nothing reached Intercom in this attempt.
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](intercomFailure(sdkgo.FailureAvailability, replyToConversationOperation,
			"the reply was not sent before the attempt deadline or checkpoint"), 0)
	}
	result := send.result.withOversizedRefusalAsUnknown(replyToConversationOperation)
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	if result.outcome != exchangeSucceeded && result.hasSuccessStatus() {
		// An oversized, credential-reflecting, or unreadable 2xx still means Intercom accepted the reply.
		return operation.confirmAcceptedReply(call, credentials, input, result.failure)
	}
	if attempt, isTerminal := classifySentReply(permit, result, output, receipt); isTerminal {
		return attempt
	}
	wire, err := decodeConversationEnvelope(result.response.body, input.ConversationID)
	if err != nil {
		return operation.confirmAcceptedReply(call, credentials, input, intercomFailure(sdkgo.FailureProtocol, replyToConversationOperation,
			"Intercom accepted the reply but returned an invalid conversation: "+err.Error()))
	}
	output.ConversationState = ConversationState(wire.State)
	part, isPartFound := findReplyPart(wire.ConversationParts.Parts, input, dispatchedAt.Add(-replyReconciliationSkew), htmlComparisonText)
	if !isPartFound {
		part, isPartFound = findNewestAdminPart(wire.ConversationParts.Parts, input, dispatchedAt.Add(-replyReconciliationSkew))
	}
	if isPartFound {
		decoded, err := decodePartWire(part)
		if err != nil {
			return operation.confirmAcceptedReply(call, credentials, input, intercomFailure(sdkgo.FailureProtocol, replyToConversationOperation,
				"Intercom accepted the reply but returned an invalid part: "+err.Error()))
		}
		output.Part = decoded
		receipt.Metadata = map[string]string{"partId": decoded.ID}
	}
	return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
}

// confirmAcceptedReply reads back once after a 2xx with an unusable answer. No provider ID guards a
// later attempt against resending, so anything but a confirmed part selects uncertain, never Retry.
func (operation ReplyToConversationOperation) confirmAcceptedReply(
	call sdkgo.Call, credentials Credentials, input ReplyToConversationInput, answerFailure sdkgo.Failure,
) sdkgo.MutationAttempt[ReplyToConversationOutput] {
	readBack := operation.readBackReply(call, credentials, input)
	if readBack.outcome == replyReadBackFound {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, readBack.output, nil, readBack.receipt)
	}
	return sdkgo.NewMutationUncertain(readBack.output, intercomFailure(answerFailure.Kind, replyToConversationOperation,
		answerFailure.Message+"; the reply was accepted and is never resent, and "+readBack.failure.Message), readBack.receipt)
}

// replyReadBackOutcome is what reading the conversation back proves about the reply.
type replyReadBackOutcome uint8

const (
	// replyReadBackFound means the conversation shows the reply with a valid part.
	replyReadBackFound replyReadBackOutcome = iota + 1
	// replyReadBackAbsent means the conversation was read and shows no matching part.
	replyReadBackAbsent
	// replyReadBackRetryable means the read may succeed when repeated, such as after a 429.
	replyReadBackRetryable
	// replyReadBackUnconfirmed means the read proves neither presence nor absence, such as a 404 or an invalid part.
	replyReadBackUnconfirmed
)

// replyReadBack is one read-back result; failure explains every outcome except replyReadBackFound.
type replyReadBack struct {
	outcome    replyReadBackOutcome
	output     ReplyToConversationOutput
	failure    sdkgo.Failure
	retryAfter time.Duration
	receipt    sdkgo.Receipt
}

// readBackReply never sends: it looks for a part by the input's admin, with its type and words, since the Step's first attempt.
func (operation ReplyToConversationOperation) readBackReply(
	call sdkgo.Call, credentials Credentials, input ReplyToConversationInput,
) replyReadBack {
	readBack := replyReadBack{output: ReplyToConversationOutput{ConversationID: input.ConversationID}}
	result := operation.client.readConversation(call, credentials, replyToConversationOperation, input.ConversationID)
	readBack.receipt = operation.client.receipt(call, result.response, input.ConversationID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		readBack.outcome, readBack.failure, readBack.retryAfter = replyReadBackRetryable, result.failure, result.retryAfter
		return readBack
	default:
		readBack.outcome = replyReadBackUnconfirmed
		readBack.failure = intercomFailure(result.failure.Kind, replyToConversationOperation, "the conversation could not be read back: "+result.failure.Message)
		return readBack
	}
	wire, err := decodeConversationEnvelope(result.response.body, input.ConversationID)
	if err != nil {
		readBack.outcome = replyReadBackUnconfirmed
		readBack.failure = intercomFailure(sdkgo.FailureProtocol, replyToConversationOperation, "the conversation read back is invalid: "+err.Error())
		return readBack
	}
	readBack.output.ConversationState = ConversationState(wire.State)
	var since time.Time
	if firstAttemptAt := call.Context.FirstAttemptAt(); !firstAttemptAt.IsZero() {
		// Every send of this Step happened after its first attempt began.
		since = firstAttemptAt.Add(-replyReconciliationSkew)
	}
	part, isPartFound := findReplyPart(wire.ConversationParts.Parts, input, since, plainComparisonText)
	if !isPartFound {
		readBack.outcome = replyReadBackAbsent
		readBack.failure = intercomFailure(sdkgo.FailureTransport, replyToConversationOperation, "the conversation shows no matching part")
		return readBack
	}
	decoded, err := decodePartWire(part)
	if err != nil {
		readBack.outcome = replyReadBackUnconfirmed
		readBack.failure = intercomFailure(sdkgo.FailureProtocol, replyToConversationOperation, "the conversation shows the reply, but its part is invalid: "+err.Error())
		return readBack
	}
	readBack.outcome, readBack.output.Part = replyReadBackFound, decoded
	readBack.receipt.Metadata = map[string]string{"partId": decoded.ID}
	return readBack
}

// hasPossibleEarlierReplySend reports that an earlier attempt may have sent, so the conversation is read back first.
func hasPossibleEarlierReplySend(decision singledispatch.Decision) bool {
	return decision.Kind != singledispatch.SendAllowed || decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != ""
}

// canResendAfterAbsentReadBack reports room under the cap and no saved ID proving Intercom accepted a send.
func canResendAfterAbsentReadBack(decision singledispatch.Decision) bool {
	return decision.Kind != singledispatch.ReadBackRequired && decision.Marker.ProviderID == ""
}

// replyCredentialFailureAttempt maps unusable credentials before this attempt's send. After a possible
// earlier send it records nothing, since notSent would hide that send, and never selects defect.
func replyCredentialFailureAttempt(fence *singledispatch.Fence, hasPossibleEarlierSend bool, problem credentialFailure,
	output ReplyToConversationOutput) sdkgo.MutationAttempt[ReplyToConversationOutput] {
	if hasPossibleEarlierSend {
		if problem.isTransient {
			return sdkgo.NewMutationRetry[ReplyToConversationOutput](problem.failure, 0)
		}
		return sdkgo.NewMutationUncertain(output, intercomFailure(problem.failure.Kind, replyToConversationOperation,
			"an earlier attempt of this Step may have sent the reply, and the connection credentials cannot read it back"), sdkgo.Receipt{})
	}
	if err := fence.RecordNotSent(); err != nil {
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](intercomFailure(sdkgo.FailureAvailability, replyToConversationOperation,
			"credential checkpoint could not be recorded; nothing was sent"), 0)
	}
	if problem.isTransient {
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](problem.failure, 0)
	}
	return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, &problem.failure, sdkgo.Receipt{})
}

// replySend is the fenced reply request and its classified outcome.
type replySend struct {
	client      *Client
	call        sdkgo.Call
	credentials Credentials
	request     intercomRequest
	result      intercomExchange
}

func (send *replySend) dispatch(requestContext context.Context) error {
	send.request.requestContext = requestContext
	send.result = send.client.exchange(send.call, send.credentials, replyToConversationOperation, send.request)
	return nil
}

// replySendPermit is a fence that allowed this attempt's send, with its send-cap facts.
type replySendPermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
}

// classifySentReply maps a sent reply. Only a definite refusal selects a refusal branch; any other
// outcome returns Retry, and the next attempt reads back before it resends or selects uncertain at the cap.
func classifySentReply(permit replySendPermit, result intercomExchange, output ReplyToConversationOutput, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[ReplyToConversationOutput], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[ReplyToConversationOutput]{}, false
	case exchangeNotSent, exchangeRateLimited:
		// A lost checkpoint leaves the claim, so the next attempt counts this send and reads back.
		_ = permit.fence.RecordCanResend()
	case exchangeUnconfirmed:
		if result.response.statusCode == http.StatusServiceUnavailable {
			// Intercom answers 503 before applying the request; a lost checkpoint only overcounts.
			_ = permit.fence.RecordCanResend()
		}
	case exchangeNotFound:
		if result.response.statusCode == http.StatusNotFound {
			return rejectSentReply(permit, result, output, receipt, ReplyToConversationBranchNotFound), true
		}
	case exchangeDefect:
		if permit.isFirstSend {
			return rejectSentReply(permit, result, output, receipt, ReplyToConversationBranchDefect), true
		}
	case exchangeRejected:
		if isDefiniteRefusalStatus(result.response.statusCode) {
			return rejectSentReply(permit, result, output, receipt, ReplyToConversationBranchProviderRejected), true
		}
	}
	// A 3xx, 408, 409, 410, 5xx, unlisted status, or lost answer may follow an applied reply.
	return sdkgo.NewMutationRetry[ReplyToConversationOutput](result.failure, result.retryAfter), true
}

// rejectSentReply selects a refusal branch only when no earlier send of this call may have applied.
func rejectSentReply(permit replySendPermit, result intercomExchange, output ReplyToConversationOutput, receipt sdkgo.Receipt, branch sdkgo.BranchID) sdkgo.MutationAttempt[ReplyToConversationOutput] {
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(output, result.failure, receipt)
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](intercomFailure(sdkgo.FailureAvailability, replyToConversationOperation,
			"refusal checkpoint could not be recorded"), 0)
	}
	if branch == ReplyToConversationBranchDefect {
		receipt = sdkgo.Receipt{}
	}
	return sdkgo.NewMutationBranch(branch, output, &result.failure, receipt)
}

// isDefiniteRefusalStatus reports a status that proves Intercom applied nothing.
func isDefiniteRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

func validateReplyToConversationInput(input ReplyToConversationInput) error {
	if err := validateConversationID("conversationId", input.ConversationID); err != nil {
		return err
	}
	if err := validateAdminID(input.AdminID); err != nil {
		return err
	}
	switch input.MessageType {
	case ReplyMessageTypeComment, ReplyMessageTypeNote:
	default:
		return fmt.Errorf("messageType %q must be comment, for a reply the customer sees, or note, for an internal note", input.MessageType)
	}
	if strings.TrimSpace(input.Body) == "" {
		return errors.New("body is required")
	}
	if len(input.Body) > MaxTextBytes || !utf8.ValidString(input.Body) {
		return fmt.Errorf("body must be valid UTF-8 of at most %d bytes", MaxTextBytes)
	}
	return nil
}

// plainTextToIntercomHTML escapes plain text into paragraphs, so Intercom shows it as written.
func plainTextToIntercomHTML(text string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	paragraphs := paragraphBreakPattern.Split(normalized, -1)
	var builder strings.Builder
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		builder.WriteString("<p>")
		builder.WriteString(strings.ReplaceAll(html.EscapeString(paragraph), "\n", "<br>"))
		builder.WriteString("</p>")
	}
	return builder.String()
}

// findReplyPart returns the newest part by the input's admin, with its type and text, created at or after since.
func findReplyPart(parts []intercomPartWire, input ReplyToConversationInput, since time.Time,
	comparisonText func(string) string) (intercomPartWire, bool) {
	wanted := plainComparisonText(input.Body)
	var found intercomPartWire
	isFound := false
	for _, part := range parts {
		if !isAdminPartOfInput(part, input, since) || comparisonText(part.Body) != wanted {
			continue
		}
		if !isFound || part.CreatedAt >= found.CreatedAt {
			found, isFound = part, true
		}
	}
	return found, isFound
}

// findNewestAdminPart identifies a confirmed reply whose text Intercom changed, by admin and type alone.
func findNewestAdminPart(parts []intercomPartWire, input ReplyToConversationInput, since time.Time) (intercomPartWire, bool) {
	var found intercomPartWire
	isFound := false
	for _, part := range parts {
		if isAdminPartOfInput(part, input, since) && (!isFound || part.CreatedAt >= found.CreatedAt) {
			found, isFound = part, true
		}
	}
	return found, isFound
}

func isAdminPartOfInput(part intercomPartWire, input ReplyToConversationInput, since time.Time) bool {
	return part.PartType == string(input.MessageType) && part.Author.Type == "admin" && string(part.Author.ID) == input.AdminID &&
		(since.IsZero() || !part.CreatedAt.time().Before(since))
}

// plainComparisonText reduces plain text to its words, so line-break rendering differences do not matter.
func plainComparisonText(text string) string {
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}

// htmlComparisonText reduces an HTML body to the words plainComparisonText produces for its text.
func htmlComparisonText(body string) string {
	return plainComparisonText(htmlTagPattern.ReplaceAllString(body, " "))
}
