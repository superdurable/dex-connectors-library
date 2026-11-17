// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// GroupRobotMessageType selects how WeCom renders a group robot post.
type GroupRobotMessageType string

const (
	// GroupRobotMessageText posts plain text of at most 2048 UTF-8 bytes and may mention members.
	GroupRobotMessageText GroupRobotMessageType = "text"
	// GroupRobotMessageMarkdown posts WeCom's markdown subset of at most 4096 UTF-8 bytes.
	GroupRobotMessageMarkdown GroupRobotMessageType = "markdown"
)

// Content limits from https://developer.work.weixin.qq.com/document/path/91770.
const (
	maxGroupRobotTextBytes     = 2048
	maxGroupRobotMarkdownBytes = 4096
	maxMentionBytes            = 64
	// groupRobotRateLimitWindow is the documented 20-messages-per-minute robot window that 45009 reports.
	groupRobotRateLimitWindow = time.Minute
)

// SendGroupRobotMessageInput is one post to the group chat that owns the connection's webhook key.
type SendGroupRobotMessageInput struct {
	// MessageType is text or markdown.
	MessageType GroupRobotMessageType `json:"messageType"`
	// Content is the UTF-8 message body: at most 2048 bytes for text and 4096 bytes for markdown.
	Content string `json:"content"`
	// MentionedUserIDs lists WeCom member user IDs to mention in a text post; "@all" mentions everyone. Markdown posts cannot mention.
	MentionedUserIDs []string `json:"mentionedUserIds,omitempty"`
	// MentionedMobileNumbers lists member mobile numbers to mention in a text post; "@all" mentions everyone. Markdown posts cannot mention.
	MentionedMobileNumbers []string `json:"mentionedMobileNumbers,omitempty"`
}

// SentGroupRobotMessage confirms a post WeCom accepted. It never repeats the posted content.
type SentGroupRobotMessage struct {
	// MessageType is the posted message type.
	MessageType GroupRobotMessageType `json:"messageType"`
	// ContentBytes is the UTF-8 byte length of the posted content.
	ContentBytes int `json:"contentBytes"`
}

// SendGroupRobotMessageOperation posts one group robot message behind a durable single-dispatch fence.
type SendGroupRobotMessageOperation struct{ client *Client }

// Definition returns the manifest-generated group robot post contract.
func (SendGroupRobotMessageOperation) Definition() sdkgo.MutationDefinition {
	return SendGroupRobotMessageDefinition
}

// IdempotencyKey records stable call identity; WeCom robot posts accept no deduplication key.
func (SendGroupRobotMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendGroupRobotMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var webhookKeyPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// Invoke posts under a single-dispatch fence. WeCom robot posts have no idempotency key or
// read-back, so an unknown outcome resends while possible sends stay below call.MaxSends() and
// selects uncertain at the cap. A definite refusal is never resent.
func (operation SendGroupRobotMessageOperation) Invoke(call sdkgo.Call, input SendGroupRobotMessageInput) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	if message := validateGroupRobotInput(input); message != "" {
		return rejectedGroupRobotPost(SendGroupRobotMessageBranchDefect, sdkgo.FailureValidation, message)
	}
	fence, err := singledispatch.NewFence(call, SendGroupRobotMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return rejectedGroupRobotPost(SendGroupRobotMessageBranchDefect, sdkgo.FailureLocalDefect, "WeCom dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind != singledispatch.FailClosed && decision.Marker.ProviderID == groupRobotAcceptedMarker {
		// An earlier attempt saw errcode 0, so the post is confirmed without posting again.
		return sdkgo.NewMutationBranch(SendGroupRobotMessageBranchSent, newSentGroupRobotMessage(input), nil, sdkgo.Receipt{})
	}
	switch decision.Kind {
	case singledispatch.FailClosed:
		return retryGroupRobotPost(sdkgo.FailureAvailability, "WeCom dispatch checkpoint is unreadable; nothing was posted in this attempt", 0)
	case singledispatch.ReadBackRequired:
		return uncertainGroupRobotPost(sdkgo.FailureTransport, "an earlier attempt may have posted to WeCom and the send cap is reached; check the group chat before posting again")
	}
	post := groupRobotPost{
		fence: fence,
		// This attempt's send would be the last one the cap allows when no further send remains.
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		// An earlier send may have posted, so a refusal of this resend cannot prove nothing changed.
		hasPossibleEarlierSend: decision.MaximumPriorSends > 0,
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return post.localFailure(sdkgo.FailureAuthentication, "WeCom group robot credentials are unavailable")
	}
	if credentials.AuthMethodID != groupRobotAuthMethodID {
		return post.localFailure(sdkgo.FailureValidation, "sendGroupRobotMessage needs a connection that uses the group-robot method")
	}
	webhookKey := credentials.WebhookKey.Reveal()
	if !webhookKeyPattern.MatchString(webhookKey) {
		return post.localFailure(sdkgo.FailureAuthentication, "webhook_key must be only the value after key= in the robot webhook address")
	}
	body, err := json.Marshal(newGroupRobotRequest(input))
	if err != nil {
		return post.localFailure(sdkgo.FailureLocalDefect, "WeCom group robot request could not be encoded")
	}
	endpoint := operation.client.baseURL + "/cgi-bin/webhook/send?" + url.Values{"key": {webhookKey}}.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return post.localFailure(sdkgo.FailureLocalDefect, "WeCom group robot request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return post.localFailure(sdkgo.FailureLocalDefect, "WeCom group robot request is invalid")
	}
	var didGetConnection atomic.Bool
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		request = request.WithContext(httptrace.WithClientTrace(requestContext, trace))
		var sendErr error
		response, sendErr = operation.client.httpClient.Do(request)
		return sendErr
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retryGroupRobotPost(sdkgo.FailureTransport, "WeCom group robot post was not started", 0)
		}
		// The error embeds the request URL, which carries the webhook key, so only fixed messages are kept.
		// Only net/http's Transport proves a dial-phase failure sent nothing; any other error may follow a sent post.
		if operation.client.hasStandardTransport && !didGetConnection.Load() && isUnsentDialError(err) {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return retryGroupRobotPost(sdkgo.FailureTransport, "WeCom could not be reached before posting", 0)
		}
		return post.resendOrSelectUncertain(sdkgo.FailureTransport, "WeCom group robot post outcome is unknown", 0)
	}
	// Closing the response cannot change the provider outcome already received.
	defer func() { _ = response.Body.Close() }()
	return post.classifyResponse(response, operation.client.maxResponseBytes, input)
}

// groupRobotPost holds one attempt's fence and its send-cap decision.
type groupRobotPost struct {
	fence                   *singledispatch.Fence
	canResendUnknownOutcome bool
	hasPossibleEarlierSend  bool
}

func (post groupRobotPost) classifyResponse(response *http.Response, maxResponseBytes int64, input SendGroupRobotMessageInput) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		// A lost checkpoint leaves the claim, which the next attempt counts as a send.
		_ = post.fence.RecordCanResend()
		return retryGroupRobotPost(kind, "WeCom did not process the group robot post", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if isDefiniteRefusalStatus(response.StatusCode) {
		return post.rejectOrSelectUncertain(sdkgo.FailureProviderRejection, "WeCom rejected the group robot post with HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusOK {
		return post.resendOrSelectUncertain(sdkgo.FailureAvailability, "WeCom group robot post outcome is unknown after HTTP "+strconv.Itoa(response.StatusCode), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, maxResponseBytes)
	if err != nil {
		return post.resendOrSelectUncertain(sdkgo.FailureTransport, "WeCom group robot response could not be read", 0)
	}
	errcode, ok := decodeProviderEnvelope(body, nil)
	if !ok {
		return post.resendOrSelectUncertain(sdkgo.FailureProtocol, "WeCom group robot response is malformed", 0)
	}
	if isRetryableErrcode(errcode) {
		_ = post.fence.RecordCanResend()
		retryAfter := time.Duration(0)
		if errcode == errcodeFrequencyLimited {
			retryAfter = groupRobotRateLimitWindow
		}
		return retryGroupRobotPost(failureKindForErrcode(errcode), rejectionMessage("WeCom deferred the group robot post", errcode), retryAfter)
	}
	if errcode != errcodeSuccess {
		return post.rejectOrSelectUncertain(failureKindForErrcode(errcode), rejectionMessage("WeCom rejected the group robot post", errcode))
	}
	// A lost acceptance checkpoint leaves the claim, which a later attempt counts as a send.
	_ = post.fence.RecordProviderID(groupRobotAcceptedMarker)
	return sdkgo.NewMutationBranch(SendGroupRobotMessageBranchSent, newSentGroupRobotMessage(input), nil, sdkgo.Receipt{})
}

// groupRobotAcceptedMarker is the checkpoint provider ID of a post WeCom accepted; robot posts return no message ID.
const groupRobotAcceptedMarker = "errcode-0"

func newSentGroupRobotMessage(input SendGroupRobotMessageInput) SentGroupRobotMessage {
	return SentGroupRobotMessage{MessageType: input.MessageType, ContentBytes: len(input.Content)}
}

// resendOrSelectUncertain retries a possibly sent post while the send cap allows another send.
func (post groupRobotPost) resendOrSelectUncertain(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	if post.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return retryGroupRobotPost(kind, message, retryAfter)
	}
	return uncertainGroupRobotPost(kind, message)
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send of this call may have posted.
func (post groupRobotPost) rejectOrSelectUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	if post.hasPossibleEarlierSend {
		return uncertainGroupRobotPost(kind, message)
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = post.fence.RecordCanResend()
	return rejectedGroupRobotPost(SendGroupRobotMessageBranchProviderRejected, kind, message)
}

// localFailure selects defect only when no earlier send of this call may have posted.
func (post groupRobotPost) localFailure(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	if post.hasPossibleEarlierSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		return retryGroupRobotPost(kind, message+"; nothing was posted in this attempt", 0)
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = post.fence.RecordNotSent()
	return rejectedGroupRobotPost(SendGroupRobotMessageBranchDefect, kind, message)
}

// validateGroupRobotInput returns a safe message describing the first invalid field, or "".
func validateGroupRobotInput(input SendGroupRobotMessageInput) string {
	if input.Content == "" || !utf8.ValidString(input.Content) {
		return "content must be nonempty UTF-8"
	}
	switch input.MessageType {
	case GroupRobotMessageText:
		if len(input.Content) > maxGroupRobotTextBytes {
			return "text content must be at most 2048 UTF-8 bytes"
		}
	case GroupRobotMessageMarkdown:
		if len(input.Content) > maxGroupRobotMarkdownBytes {
			return "markdown content must be at most 4096 UTF-8 bytes"
		}
		if len(input.MentionedUserIDs) > 0 || len(input.MentionedMobileNumbers) > 0 {
			return "markdown posts cannot carry mention lists; use a text post to mention members"
		}
	default:
		return "messageType must be text or markdown"
	}
	for _, mention := range append(append([]string(nil), input.MentionedUserIDs...), input.MentionedMobileNumbers...) {
		if !isPrintableIdentifier(mention, maxMentionBytes) {
			return "each mention must be a nonblank user ID, mobile number, or @all of at most 64 bytes"
		}
	}
	return ""
}

type groupRobotRequest struct {
	MessageType GroupRobotMessageType `json:"msgtype"`
	Text        *groupRobotText       `json:"text,omitempty"`
	Markdown    *groupRobotMarkdown   `json:"markdown,omitempty"`
}

type groupRobotText struct {
	Content                string   `json:"content"`
	MentionedUserIDs       []string `json:"mentioned_list,omitempty"`
	MentionedMobileNumbers []string `json:"mentioned_mobile_list,omitempty"`
}

type groupRobotMarkdown struct {
	Content string `json:"content"`
}

func newGroupRobotRequest(input SendGroupRobotMessageInput) groupRobotRequest {
	if input.MessageType == GroupRobotMessageMarkdown {
		return groupRobotRequest{MessageType: input.MessageType, Markdown: &groupRobotMarkdown{Content: input.Content}}
	}
	return groupRobotRequest{MessageType: input.MessageType, Text: &groupRobotText{
		Content: input.Content, MentionedUserIDs: input.MentionedUserIDs, MentionedMobileNumbers: input.MentionedMobileNumbers,
	}}
}

// isPrintableIdentifier reports a nonblank value of printable ASCII without "|", WeCom's list separator.
func isPrintableIdentifier(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7E || value[index] == '|' {
			return false
		}
	}
	return true
}

func rejectedGroupRobotPost(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	failure := newFailure("sendGroupRobotMessage", kind, message)
	return sdkgo.NewMutationBranch(branch, SentGroupRobotMessage{}, &failure, sdkgo.Receipt{})
}

func retryGroupRobotPost(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	return sdkgo.NewMutationRetry[SentGroupRobotMessage](newFailure("sendGroupRobotMessage", kind, message), retryAfter)
}

func uncertainGroupRobotPost(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentGroupRobotMessage] {
	return sdkgo.NewMutationUncertain(SentGroupRobotMessage{}, newFailure("sendGroupRobotMessage", kind, message), sdkgo.Receipt{})
}
