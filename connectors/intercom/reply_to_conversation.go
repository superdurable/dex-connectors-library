// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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

// replyDispatchMarker is the Dex heartbeat checkpoint recorded before the reply is sent.
type replyDispatchMarker struct {
	DispatchedCallID sdkgo.CallID `json:"intercomReplyDispatchedCallId"`
	// DispatchedAt is the Worker's Unix time just before the send.
	DispatchedAt int64 `json:"intercomReplyDispatchedAt"`
}

// Definition returns the immutable connector operation definition.
func (ReplyToConversationOperation) Definition() sdkgo.MutationDefinition {
	return ReplyToConversationDefinition
}

// IdempotencyKey uses the stable connector Call ID. Intercom documents no idempotency key, so the key
// only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (ReplyToConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /conversations/{id}/reply at most once per Step execution. It records a dispatch
// checkpoint first. An attempt that finds the checkpoint never sends: it reads the conversation and
// reports the earlier reply when a part by the same admin with the same type and text exists, or
// selects uncertain. Only a 429 or a connection that never opened clears the checkpoint for a resend.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ReplyToConversationOutput] {
	output := ReplyToConversationOutput{ConversationID: input.ConversationID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, intercomFailurePointer(sdkgo.FailureValidation, replyToConversationOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, replyToConversationOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, failure, sdkgo.Receipt{})
	}
	var marker replyDispatchMarker
	isMarkerFound, err := call.Context.GetLastHeartbeatValue(&marker)
	if err != nil || isMarkerFound {
		return operation.reconcileEarlierDispatch(call, credentials, input, marker)
	}
	dispatchedAt := operation.client.now()
	if err := call.Context.RecordHeartbeat(replyDispatchMarker{DispatchedCallID: call.ID, DispatchedAt: dispatchedAt.Unix()}); err != nil {
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](intercomFailure(sdkgo.FailureAvailability, replyToConversationOperation,
			"Dex did not record the dispatch checkpoint; nothing was sent"), 0)
	}
	result := operation.client.exchange(call, credentials, replyToConversationOperation, intercomRequest{
		method: http.MethodPost, path: conversationPath(input.ConversationID) + "/reply",
		payload: replyRequestWire{
			MessageType: string(input.MessageType), Type: "admin", AdminID: input.AdminID, Body: plainTextToIntercomHTML(input.Body),
		},
	})
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeNotSent, exchangeRateLimited:
		releaseReplyDispatch(call)
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](result.failure, result.retryAfter)
	case exchangeUnconfirmed:
		// The checkpoint stays, so the next attempt reads the conversation instead of resending.
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchInvalidResponse, output, &result.failure, receipt)
	case exchangeDefect:
		releaseReplyDispatch(call)
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchProviderRejected, output, &result.failure, receipt)
	}
	wire, err := decodeConversationEnvelope(result.response.body, input.ConversationID)
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchInvalidResponse, output, intercomFailurePointer(sdkgo.FailureProtocol, replyToConversationOperation,
			"Intercom accepted the reply but returned an invalid conversation: "+err.Error()), receipt)
	}
	output.ConversationState = ConversationState(wire.State)
	part, isPartFound := findReplyPart(wire.ConversationParts.Parts, input, dispatchedAt.Add(-replyReconciliationSkew), htmlComparisonText)
	if !isPartFound {
		part, isPartFound = findNewestAdminPart(wire.ConversationParts.Parts, input, dispatchedAt.Add(-replyReconciliationSkew))
	}
	if isPartFound {
		decoded, err := decodePartWire(part)
		if err != nil {
			return sdkgo.NewMutationBranch(ReplyToConversationBranchInvalidResponse, output, intercomFailurePointer(sdkgo.FailureProtocol, replyToConversationOperation,
				"Intercom accepted the reply but returned an invalid part: "+err.Error()), receipt)
		}
		output.Part = decoded
		receipt.Metadata = map[string]string{"partId": decoded.ID}
	}
	return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
}

// reconcileEarlierDispatch never sends: it reports an earlier attempt's reply found in the conversation, or uncertain.
func (operation ReplyToConversationOperation) reconcileEarlierDispatch(
	call sdkgo.Call, credentials Credentials, input ReplyToConversationInput, marker replyDispatchMarker,
) sdkgo.MutationAttempt[ReplyToConversationOutput] {
	output := ReplyToConversationOutput{ConversationID: input.ConversationID}
	result := operation.client.readConversation(call, credentials, replyToConversationOperation, input.ConversationID)
	receipt := operation.client.receipt(call, result.response, input.ConversationID)
	switch {
	case result.outcome == exchangeSucceeded:
	case result.isRetryableRead():
		return sdkgo.NewMutationRetry[ReplyToConversationOutput](result.failure, result.retryAfter)
	case result.outcome == exchangeNotFound:
		return sdkgo.NewMutationBranch(ReplyToConversationBranchNotFound, output, &result.failure, receipt)
	default:
		return sdkgo.NewMutationUncertain(output, intercomFailure(result.failure.Kind, replyToConversationOperation,
			"an earlier attempt of this Step sent the reply, and the conversation could not be read back: "+result.failure.Message), receipt)
	}
	wire, err := decodeConversationEnvelope(result.response.body, input.ConversationID)
	if err != nil {
		return sdkgo.NewMutationUncertain(output, intercomFailure(sdkgo.FailureProtocol, replyToConversationOperation,
			"an earlier attempt of this Step sent the reply, and the conversation read back is invalid: "+err.Error()), receipt)
	}
	output.ConversationState = ConversationState(wire.State)
	var since time.Time
	if marker.DispatchedAt > 0 {
		since = time.Unix(marker.DispatchedAt, 0).Add(-replyReconciliationSkew)
	}
	part, isPartFound := findReplyPart(wire.ConversationParts.Parts, input, since, plainComparisonText)
	if !isPartFound {
		return sdkgo.NewMutationUncertain(output, intercomFailure(sdkgo.FailureTransport, replyToConversationOperation,
			"an earlier attempt of this Step sent the reply without a confirmed outcome, and the conversation shows no matching part, so it is not sent again"), receipt)
	}
	decoded, err := decodePartWire(part)
	if err != nil {
		return sdkgo.NewMutationUncertain(output, intercomFailure(sdkgo.FailureProtocol, replyToConversationOperation,
			"an earlier attempt of this Step sent the reply, and its part read back is invalid: "+err.Error()), receipt)
	}
	output.Part, output.WasAlreadyApplied = decoded, true
	receipt.Metadata = map[string]string{"partId": decoded.ID}
	return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
}

// releaseReplyDispatch clears the checkpoint after Intercom provably did not receive or apply the reply.
func releaseReplyDispatch(call sdkgo.Call) {
	// A failed clear leaves the checkpoint, so the next attempt reconciles instead of resending.
	_ = call.Context.RecordHeartbeat(nil)
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
