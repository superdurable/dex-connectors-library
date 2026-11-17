// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	// MaxReplyTextBytes bounds the text of one reply or comment; it is the connector's limit, not Front's.
	MaxReplyTextBytes = 64 << 10
)

var messageUIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

// ReplyToConversationInput is one reply to the customer or one internal comment.
type ReplyToConversationInput struct {
	// ConversationID is the Front conversation ID, such as cnv_55c8c149.
	ConversationID string `json:"conversationId"`
	// Text is the plain text to send, required and at most MaxReplyTextBytes. A reply sends it as text and
	// as an HTML body with one paragraph per blank-line-separated block; a comment sends it unchanged, and
	// Front renders Markdown in comments.
	Text string `json:"text"`
	// IsInternalNote adds a comment that only teammates see, through POST /conversations/{id}/comments.
	// False sends a reply that Front delivers to the conversation's recipients, through
	// POST /conversations/{id}/messages.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// AuthorID is the teammate the reply is sent on behalf of, or who writes the comment, such as tea_2thf.
	// Blank lets Front choose: a comment is then written as the API token.
	AuthorID string `json:"authorId,omitempty"`
	// ShouldArchive archives the conversation when the reply is sent. False keeps it open, unlike Front's
	// own default; a comment must leave it false.
	ShouldArchive bool `json:"shouldArchive,omitempty"`
}

// ConversationReply is the reply or comment replyToConversation added.
type ConversationReply struct {
	// ConversationID is the conversation the reply or comment belongs to.
	ConversationID string `json:"conversationId"`
	// IsInternalNote reports that a comment was added rather than a reply sent.
	IsInternalNote bool `json:"isInternalNote,omitempty"`
	// MessageUID is Front's message_uid for an accepted reply; GET /messages/alt:uid:{uid} reads the message
	// once Front has created it. Empty for a comment.
	MessageUID string `json:"messageUid,omitempty"`
	// CommentID is the new comment's ID, such as com_1ywg3f2; empty for a reply.
	CommentID string `json:"commentId,omitempty"`
	// AuthorID is the requested author, or empty.
	AuthorID string `json:"authorId,omitempty"`
}

// ReplyToConversationOperation implements the replyToConversation Mutation. Build it with
// Client.ReplyToConversation.
type ReplyToConversationOperation struct{ client *Client }

type replySendAttempt struct {
	client      *Client
	credentials Credentials
	operationID string
	request     frontRequest
	result      frontExchange
}

type replyMessageWire struct {
	Body     string `json:"body"`
	Text     string `json:"text"`
	AuthorID string `json:"author_id,omitempty"`
	Options  struct {
		Archive bool `json:"archive"`
	} `json:"options"`
}

type commentWire struct {
	Body     string `json:"body"`
	AuthorID string `json:"author_id,omitempty"`
}

// Definition returns the immutable replyToConversation operation definition.
func (ReplyToConversationOperation) Definition() sdkgo.MutationDefinition {
	return ReplyToConversationDefinition
}

// IdempotencyKey returns the call ID, recorded in the Receipt only. Front documents no idempotency key,
// and messages and comments carry no client-supplied ID a later attempt could find. A single-dispatch
// fence prevents another send when the prior outcome may have reached Front.
func (ReplyToConversationOperation) IdempotencyKey(callID sdkgo.CallID, _ ReplyToConversationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fences the reply or comment under the manifest send cap. A later attempt reads back an accepted
// provider ID and never resends it; without one, an unknown outcome resends while the cap allows.
func (operation ReplyToConversationOperation) Invoke(call sdkgo.Call, input ReplyToConversationInput) sdkgo.MutationAttempt[ConversationReply] {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	client := operation.client
	output := ConversationReply{ConversationID: input.ConversationID, IsInternalNote: input.IsInternalNote, AuthorID: input.AuthorID}
	if err := validateReplyToConversationInput(input); err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, frontFailurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, ReplyToConversationDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output, frontFailurePointer(sdkgo.FailureLocalDefect, operationID, err.Error()), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		// FailClosed sends nothing, so a later attempt decides again from a readable checkpoint.
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
			"the dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
	case singledispatch.ReadBackRequired:
		return client.readBackReply(call, input, output, decision.Marker)
	case singledispatch.ReadBackThenResendAllowed:
		// Front returns an ID only for an accepted write, so a saved one is read back and never resent.
		if decision.Marker.ProviderID != "" {
			return client.readBackReply(call, input, output, decision.Marker)
		}
	}
	permit := replySendPermit{
		fence:                   fence,
		isFirstSend:             decision.MaximumPriorSends == 0,
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
	}
	credentials, failure, credentialError := client.resolveCredentialsWithCause(call, operationID)
	if failure != nil && !permit.isFirstSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		if isTerminalCredentialFailure(credentialError) {
			return client.uncertainReply(call, output, sdkgo.FailureAuthentication,
				"an earlier attempt may have sent the reply or comment, and the connection needs reconnection to resend it")
		}
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
			"Front credentials were temporarily unavailable; nothing was sent in this attempt"), 0)
	}
	if failure != nil {
		if err := fence.RecordNotSent(); err != nil {
			return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
				"Front credentials were temporarily unavailable and the not-sent checkpoint failed"), 0)
		}
		if isTerminalCredentialFailure(credentialError) {
			return sdkgo.NewMutationBranch(ReplyToConversationBranchDefect, output,
				frontFailurePointer(sdkgo.FailureAuthentication, operationID, "Reconnect the Front connection before sending the reply or comment"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
			"Front credentials were temporarily unavailable"), 0)
	}
	attempt := &replySendAttempt{client: client, credentials: credentials, operationID: operationID, request: buildReplyRequest(input)}
	if err := fence.Dispatch(attempt.send); err != nil {
		// Dispatch refused before sending, so nothing reached Front in this attempt.
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
			"the reply was not dispatched; retry after the checkpoint or attempt budget recovers"), 0)
	}
	return client.classifyReplyExchange(call, output, attempt.result, credentials, permit)
}

// replySendPermit is a fence that allowed this attempt's send, with its send-cap facts.
type replySendPermit struct {
	fence *singledispatch.Fence
	// isFirstSend means no earlier attempt of this call may have sent.
	isFirstSend bool
	// canResendUnknownOutcome means this attempt's send leaves room under the cap for one more.
	canResendUnknownOutcome bool
}

func (attempt *replySendAttempt) send(requestContext context.Context) error {
	attempt.result = attempt.client.exchangeFencedWrite(requestContext, attempt.credentials, attempt.operationID, attempt.request)
	return nil
}

func (client *Client) readBackReply(
	call sdkgo.Call, input ReplyToConversationInput, output ConversationReply, marker singledispatch.Marker,
) sdkgo.MutationAttempt[ConversationReply] {
	providerIDPattern := messageUIDPattern
	if input.IsInternalNote {
		providerIDPattern = commentIDPattern
	}
	if marker.CallID != call.ID {
		return client.uncertainReply(call, output, sdkgo.FailureAvailability, "an earlier attempt has no trusted dispatch checkpoint")
	}
	if marker.ProviderID == "" {
		if marker.State == singledispatch.StateClaimed {
			return client.uncertainReply(call, output, sdkgo.FailureTransport, "an earlier attempt may have sent the reply or comment without a provider ID")
		}
		return client.uncertainReply(call, output, sdkgo.FailureAvailability, "a stale checkpoint cannot establish whether the reply or comment was sent")
	}
	if !providerIDPattern.MatchString(marker.ProviderID) {
		return client.uncertainReply(call, output, sdkgo.FailureProtocol, "an earlier attempt stored an invalid provider ID")
	}
	operationID := ReplyToConversationDefinition.Operation.OperationID
	credentials, failure, credentialError := client.resolveCredentialsWithCause(call, operationID)
	if failure != nil {
		if isTerminalCredentialFailure(credentialError) {
			return sdkgo.NewMutationUncertain(output, frontFailure(sdkgo.FailureAuthentication, operationID,
				"Front accepted an earlier reply or comment, but its connection needs reconnection before read-back; verify the saved provider ID before retrying manually"),
				client.receipt(call, marker.ProviderID))
		}
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID,
			"Front credentials were temporarily unavailable during read-back"), 0)
	}
	path := "/messages/alt:uid:" + marker.ProviderID
	if input.IsInternalNote {
		path = "/comments/" + marker.ProviderID
	}
	result := client.exchange(call.Context, credentials, operationID, frontRequest{method: http.MethodGet, path: path})
	if result.outcome == exchangeSucceeded {
		if input.IsInternalNote {
			output.CommentID = marker.ProviderID
		} else {
			output.MessageUID = marker.ProviderID
		}
		receipt := client.receipt(call, marker.ProviderID)
		return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
	}
	if result.isRetryable() {
		return sdkgo.NewMutationRetry[ConversationReply](result.failure, result.retryAfter)
	}
	if result.outcome == exchangeNotFound && !input.IsInternalNote {
		// Front may return 404 before an accepted message_uid becomes readable; live timing is unverified.
		return sdkgo.NewMutationRetry[ConversationReply](result.failure, result.retryAfter)
	}
	message := "Front could not confirm an earlier accepted reply or comment"
	if result.failure.Kind == sdkgo.FailureAuthentication {
		message = "Front rejected credentials during accepted-write read-back; reconnect and verify the saved provider ID before retrying manually"
	}
	return sdkgo.NewMutationUncertain(output, frontFailure(result.failure.Kind, operationID, message), client.receipt(call, marker.ProviderID))
}

func isTerminalCredentialFailure(err error) bool {
	return err == nil || errors.Is(err, sdkgo.ErrReauthorizationRequired)
}

func (client *Client) uncertainReply(call sdkgo.Call, output ConversationReply, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ConversationReply] {
	return sdkgo.NewMutationUncertain(output, frontFailure(kind,
		ReplyToConversationDefinition.Operation.OperationID, message), client.receipt(call, output.ConversationID))
}

// classifyReplyExchange maps the sent request. Only a definite refusal selects a refusal branch; any
// other outcome may have been applied, so it resends while the send cap allows.
func (client *Client) classifyReplyExchange(call sdkgo.Call, output ConversationReply, result frontExchange, credentials Credentials, permit replySendPermit) sdkgo.MutationAttempt[ConversationReply] {
	receipt := client.receipt(call, output.ConversationID)
	if result.response.statusCode >= 200 && result.response.statusCode < 300 {
		output.MessageUID, output.CommentID = decodeReplyIdentity(result.response.body, output.IsInternalNote)
		providerID := cmp.Or(output.CommentID, output.MessageUID)
		if providerID == "" {
			kind := result.failure.Kind
			if kind == "" {
				kind = sdkgo.FailureProtocol
			}
			// Without a provider ID the answer cannot be read back, so it counts as an unknown outcome.
			return client.resendOrSelectUncertain(call, output, permit, frontFailure(kind,
				ReplyToConversationDefinition.Operation.OperationID, "Front answered the reply or comment without a usable provider ID"), 0)
		}
		_ = permit.fence.RecordProviderID(providerID) // Acceptance is already known if this extra checkpoint fails.
		receipt.ProviderObjectID = providerID
		return sdkgo.NewMutationBranch(ReplyToConversationBranchReplied, output, nil, receipt)
	}
	switch result.outcome {
	case exchangeDefect:
		if !permit.isFirstSend {
			// defect would hide an earlier possible send.
			return client.resendOrSelectUncertain(call, output, permit, result.failure, 0)
		}
		return client.rejectReply(call, output, permit, ReplyToConversationBranchDefect, result.failure)
	case exchangeNotSent, exchangeRateLimited:
		// A lost checkpoint leaves the claim, so the next attempt counts this send toward the cap.
		_ = permit.fence.RecordCanResend()
		return sdkgo.NewMutationRetry[ConversationReply](result.failure, result.retryAfter)
	case exchangeUnconfirmed:
		if result.response.statusCode == http.StatusServiceUnavailable {
			_ = permit.fence.RecordCanResend()
			return sdkgo.NewMutationRetry[ConversationReply](result.failure, result.retryAfter)
		}
		failure := result.failure
		failure.Message += "; the reply or comment may exist"
		return client.resendOrSelectUncertain(call, output, permit, failure, result.retryAfter)
	case exchangeNotFound:
		if result.response.statusCode != http.StatusNotFound {
			return client.resendOrSelectUncertain(call, output, permit, result.failure, 0)
		}
		return client.rejectReply(call, output, permit, ReplyToConversationBranchNotFound, result.failure)
	case exchangeMerged:
		// Front documents this 301 as a merged conversation, a definite refusal of the write.
		failure := result.failure
		if mergedInto := mergedConversationID(result.response.header.Get("Location")); mergedInto != "" {
			failure.Message = fmt.Sprintf("the conversation was merged into %s; reply to that conversation", mergedInto)
		}
		return client.rejectReply(call, output, permit, ReplyToConversationBranchNotFound, failure)
	case exchangeRejected:
		if result.response.statusCode == http.StatusUnauthorized {
			return client.classifyRejectedReplyCredential(call, output, credentials, permit)
		}
		if isDefiniteRefusalStatus(result.response.statusCode) {
			return client.rejectReply(call, output, permit, ReplyToConversationBranchProviderRejected, result.failure)
		}
		return client.resendOrSelectUncertain(call, output, permit, result.failure, 0)
	default:
		failure := result.failure
		failure.Message += "; the reply or comment may exist"
		return client.resendOrSelectUncertain(call, output, permit, failure, 0)
	}
}

// isDefiniteRefusalStatus reports a status that proves Front applied nothing.
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

// resendOrSelectUncertain retries an unknown outcome while the send cap allows another send.
func (client *Client) resendOrSelectUncertain(call sdkgo.Call, output ConversationReply, permit replySendPermit, failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[ConversationReply] {
	if permit.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[ConversationReply](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(output, failure, client.receipt(call, output.ConversationID))
}

// rejectReply selects a refusal branch only when no earlier send of this call may have applied.
func (client *Client) rejectReply(call sdkgo.Call, output ConversationReply, permit replySendPermit, branch sdkgo.BranchID, failure sdkgo.Failure) sdkgo.MutationAttempt[ConversationReply] {
	if !permit.isFirstSend {
		return sdkgo.NewMutationUncertain(output, failure, client.receipt(call, output.ConversationID))
	}
	if err := permit.fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability,
			ReplyToConversationDefinition.Operation.OperationID, "the refusal checkpoint failed"), 0)
	}
	receipt := client.receipt(call, output.ConversationID)
	if branch == ReplyToConversationBranchDefect {
		receipt = sdkgo.Receipt{}
	}
	return sdkgo.NewMutationBranch(branch, output, &failure, receipt)
}

func (client *Client) classifyRejectedReplyCredential(call sdkgo.Call, output ConversationReply, rejected Credentials, permit replySendPermit) sdkgo.MutationAttempt[ConversationReply] {
	operationID := ReplyToConversationDefinition.Operation.OperationID
	if _, supportsRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRefresh {
		return client.rejectReply(call, output, permit, ReplyToConversationBranchProviderRejected,
			frontFailure(sdkgo.FailureAuthentication, operationID, "Front rejected the API token (HTTP 401); reconnect the Front connection"))
	}
	var refreshContext context.Context = call.Context
	if deadline, hasDeadline := refreshContext.Deadline(); hasDeadline {
		var cancel context.CancelFunc
		refreshContext, cancel = context.WithDeadline(refreshContext, deadline.Add(-100*time.Millisecond))
		defer cancel()
	}
	replacement, err := sdkgo.ResolveCredentialAfterRejection(refreshContext, client.credentials, call, frontCredentialRefreshDriver{})
	if err == nil && replacement.Validate() == nil && providerhttp.IsHeaderSafeCredential(replacement.APIToken.Reveal()) &&
		replacement.APIToken.Reveal() != rejected.APIToken.Reveal() {
		if err := permit.fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAvailability, operationID, "the credential resend checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[ConversationReply](frontFailure(sdkgo.FailureAuthentication, operationID, "Front rejected the prior API token; retry with its replacement"), 0)
	}
	message := "Front rejected the API token (HTTP 401); reconnect the Front connection"
	if err != nil && !errors.Is(err, sdkgo.ErrReauthorizationRequired) {
		message = "Front rejected the API token (HTTP 401); credential refresh is temporarily unavailable"
	}
	return client.rejectReply(call, output, permit, ReplyToConversationBranchProviderRejected, frontFailure(sdkgo.FailureAuthentication, operationID, message))
}

type frontCredentialRefreshDriver struct{}

// RefreshRequired reports that Front API tokens have no expiry-based refresh.
func (frontCredentialRefreshDriver) RefreshRequired(sdkgo.CredentialRefreshState[Credentials]) bool {
	return false
}

// Refresh rejects provider-side renewal because Front API tokens require reconnection.
func (frontCredentialRefreshDriver) Refresh(context.Context, sdkgo.CredentialRefreshState[Credentials]) (sdkgo.CredentialRefreshResult[Credentials], error) {
	return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.ErrReauthorizationRequired
}

func buildReplyRequest(input ReplyToConversationInput) frontRequest {
	conversationPath := "/conversations/" + input.ConversationID
	if input.IsInternalNote {
		return frontRequest{method: http.MethodPost, path: conversationPath + "/comments", payload: commentWire{Body: input.Text, AuthorID: input.AuthorID}}
	}
	payload := replyMessageWire{Body: BuildReplyHTML(input.Text), Text: input.Text, AuthorID: input.AuthorID}
	payload.Options.Archive = input.ShouldArchive
	return frontRequest{method: http.MethodPost, path: conversationPath + "/messages", payload: payload}
}

// BuildReplyHTML returns the HTML body a reply sends for text: each block separated by a blank line
// becomes one escaped paragraph, and each remaining line break becomes <br>.
func BuildReplyHTML(text string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var paragraphs []string
	for _, block := range strings.Split(normalized, "\n\n") {
		if block = strings.Trim(block, "\n"); strings.TrimSpace(block) != "" {
			paragraphs = append(paragraphs, "<p>"+strings.ReplaceAll(html.EscapeString(block), "\n", "<br>")+"</p>")
		}
	}
	return strings.Join(paragraphs, "")
}

// decodeReplyIdentity reads the accepted reply's message_uid or the new comment's ID, or empty values.
func decodeReplyIdentity(body []byte, isInternalNote bool) (string, string) {
	var answer struct {
		ID         string `json:"id"`
		MessageUID string `json:"message_uid"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return "", ""
	}
	if isInternalNote {
		if commentIDPattern.MatchString(answer.ID) {
			return "", answer.ID
		}
		return "", ""
	}
	if messageUIDPattern.MatchString(answer.MessageUID) {
		return answer.MessageUID, ""
	}
	return "", ""
}

func validateReplyToConversationInput(input ReplyToConversationInput) error {
	if err := validateResourceID("conversationId", input.ConversationID, conversationIDPattern, "cnv_55c8c149"); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(input.Text) == "":
		return errors.New("text is required")
	case len(input.Text) > MaxReplyTextBytes:
		return fmt.Errorf("text is longer than %d bytes", MaxReplyTextBytes)
	case input.IsInternalNote && input.ShouldArchive:
		return errors.New("shouldArchive applies only to a reply, not to an internal comment")
	case input.AuthorID != "":
		return validateResourceID("authorId", input.AuthorID, teammateIDPattern, "tea_2thf")
	}
	return nil
}
