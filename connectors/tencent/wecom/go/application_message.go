// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ApplicationMessageType selects how WeCom renders an application message.
type ApplicationMessageType string

const (
	// ApplicationMessageText sends plain text of at most 2048 UTF-8 bytes.
	ApplicationMessageText ApplicationMessageType = "text"
	// ApplicationMessageMarkdown sends WeCom's markdown subset of at most 2048 UTF-8 bytes.
	ApplicationMessageMarkdown ApplicationMessageType = "markdown"
)

// Limits from https://developer.work.weixin.qq.com/document/path/90236.
const (
	maxApplicationContentBytes = 2048
	maxApplicationUserIDs      = 1000
	maxApplicationGroupIDs     = 100
	maxUserIDBytes             = 64
	// DuplicateCheckInterval is the window in which WeCom suppresses an identical application
	// message request. It is the documented four-hour maximum, which covers the operation's
	// one-hour Execute retry window.
	DuplicateCheckInterval = 4 * time.Hour
	// DuplicateCheckResendCutoff is how long after the Step's first attempt a retry may still send.
	// It keeps ten minutes of DuplicateCheckInterval for request time and clock skew. A later
	// attempt, possible only under a longer ExecuteRetry override, selects uncertain instead,
	// because WeCom would no longer suppress a repeat of an accepted send.
	DuplicateCheckResendCutoff = DuplicateCheckInterval - 10*time.Minute
)

// SendApplicationMessageInput is one message from the connection's self-built application.
// Choose ToAllMembers, or at least one of ToUserIDs, ToDepartmentIDs, and ToTagIDs.
type SendApplicationMessageInput struct {
	// ToAllMembers sends to every member in the application's visible range and excludes the ID lists.
	ToAllMembers bool `json:"toAllMembers,omitempty"`
	// ToUserIDs lists at most 1000 member user IDs, as shown in the admin console Contacts > member > Account.
	ToUserIDs []string `json:"toUserIds,omitempty"`
	// ToDepartmentIDs lists at most 100 decimal department IDs.
	ToDepartmentIDs []string `json:"toDepartmentIds,omitempty"`
	// ToTagIDs lists at most 100 decimal tag IDs.
	ToTagIDs []string `json:"toTagIds,omitempty"`
	// MessageType is text or markdown.
	MessageType ApplicationMessageType `json:"messageType"`
	// Content is the UTF-8 message body of at most 2048 bytes.
	Content string `json:"content"`
	// IsConfidential marks a text message as confidential: WeCom blocks forwarding and adds a watermark. Markdown messages cannot set it.
	IsConfidential bool `json:"isConfidential,omitempty"`
}

// SentApplicationMessage identifies an accepted application message and the recipients WeCom skipped.
type SentApplicationMessage struct {
	// MessageID is WeCom's msgid, usable to recall the message; empty only if WeCom omits it.
	MessageID string `json:"messageId"`
	// InvalidUserIDs lists requested user IDs that do not exist or are outside the visible range, in lowercase.
	InvalidUserIDs []string `json:"invalidUserIds"`
	// InvalidDepartmentIDs lists requested department IDs that do not exist or are outside the visible range.
	InvalidDepartmentIDs []string `json:"invalidDepartmentIds"`
	// InvalidTagIDs lists requested tag IDs that do not exist or are outside the visible range.
	InvalidTagIDs []string `json:"invalidTagIds"`
	// UnlicensedUserIDs lists recipients in the visible range without a WeCom basic interface license.
	UnlicensedUserIDs []string `json:"unlicensedUserIds"`
}

// SendApplicationMessageOperation sends one application message with WeCom duplicate checking enabled.
type SendApplicationMessageOperation struct{ client *Client }

// Definition returns the manifest-generated application message contract.
func (SendApplicationMessageOperation) Definition() sdkgo.MutationDefinition {
	return SendApplicationMessageDefinition
}

// IdempotencyKey records stable call identity; WeCom deduplicates by identical request content instead.
func (SendApplicationMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ SendApplicationMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var (
	corpIDPattern  = regexp.MustCompile(`^[A-Za-z0-9]{4,64}$`)
	agentIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
	groupIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
)

// Invoke sends one message under a single-dispatch fence. Every attempt sends byte-identical JSON
// with enable_duplicate_check, so WeCom should suppress a repeat of an accepted send. Because that
// suppression is unconfirmed, an unknown outcome resends only while possible sends stay below
// call.MaxSends() and before DuplicateCheckResendCutoff, and otherwise selects uncertain.
func (operation SendApplicationMessageOperation) Invoke(call sdkgo.Call, input SendApplicationMessageInput) sdkgo.MutationAttempt[SentApplicationMessage] {
	if message := validateApplicationInput(input); message != "" {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureValidation, message)
	}
	fence, err := singledispatch.NewFence(call, SendApplicationMessageDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureLocalDefect, "WeCom dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind != singledispatch.FailClosed && decision.Marker.ProviderID != "" {
		return newSentFromAcceptedMarker(decision.Marker.ProviderID)
	}
	switch decision.Kind {
	case singledispatch.FailClosed:
		return retryApplicationSend(sdkgo.FailureAvailability, "WeCom dispatch checkpoint is unreadable; nothing was sent in this attempt")
	case singledispatch.ReadBackRequired:
		return uncertainApplicationSend(sdkgo.FailureTransport, "an earlier attempt may have delivered the message and the send cap is reached; check with the recipients before sending again")
	}
	send := applicationSend{
		fence:     fence,
		operation: operation,
		// This attempt's send would be the last one the cap allows when no further send remains.
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		// An earlier send may have delivered, so a refusal of this resend cannot prove nothing changed.
		hasPossibleEarlierSend: decision.MaximumPriorSends > 0,
	}
	// The first attempt starts no later than any earlier send, so this elapsed time never understates the window used.
	if send.hasPossibleEarlierSend && !operation.client.now().Before(call.Context.FirstAttemptAt().Add(DuplicateCheckResendCutoff)) {
		return uncertainApplicationSend(sdkgo.FailureTransport, "an earlier attempt may have delivered the message and WeCom's four-hour duplicate check would no longer suppress a resend; check with the recipients before sending again")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil && !isTerminalCredentialError(err) {
		return send.retryBeforeSending(newFailure("sendApplicationMessage", sdkgo.FailureAuthentication, "WeCom application credentials are temporarily unavailable; nothing was sent in this attempt"))
	}
	if err != nil || credentials.Validate() != nil {
		return send.localFailure(sdkgo.FailureAuthentication, "WeCom application credentials are unavailable")
	}
	if credentials.AuthMethodID != applicationAuthMethodID {
		return send.localFailure(sdkgo.FailureValidation, "sendApplicationMessage needs a connection that uses the application method")
	}
	corpSecret := credentials.CorpSecret.Reveal()
	if !corpIDPattern.MatchString(credentials.CorpID) || !agentIDPattern.MatchString(credentials.AgentID) || !providerhttp.IsHeaderSafeCredential(corpSecret) {
		return send.localFailure(sdkgo.FailureAuthentication, "corp_id, agent_id, or corp_secret has an invalid format")
	}
	agentID, err := strconv.ParseInt(credentials.AgentID, 10, 64)
	if err != nil {
		return send.localFailure(sdkgo.FailureAuthentication, "agent_id must be a positive decimal integer")
	}
	body, err := json.Marshal(newApplicationRequest(input, agentID))
	if err != nil {
		return send.localFailure(sdkgo.FailureLocalDefect, "WeCom application message could not be encoded")
	}
	tokenKey := newAccessTokenCacheKey(credentials.CorpID, corpSecret)
	token := operation.client.accessTokens.resolve(call.Context, operation.client, "sendApplicationMessage", tokenKey, corpSecret)
	switch {
	case token.shouldRetry:
		return send.retryBeforeSending(token.failure)
	case token.isLocalDefect:
		return send.localFailure(token.failure.Kind, token.failure.Message)
	case token.isRejected:
		// Repeating a refused secret can block the caller IP, so it is never retried.
		return send.localFailureWithBranch(SendApplicationMessageBranchProviderRejected, token.failure.Kind, token.failure.Message)
	}
	endpoint := operation.client.baseURL + "/cgi-bin/message/send?" + url.Values{"access_token": {token.token}}.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return send.localFailure(sdkgo.FailureLocalDefect, "WeCom application message request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return send.localFailure(sdkgo.FailureLocalDefect, "WeCom application message request is invalid")
	}
	exchange, err := operation.client.dispatchFencedWrite(fence, request)
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retryApplicationSend(sdkgo.FailureTransport, "WeCom application message was not started")
		}
		// The error embeds the request URL, which carries the access token, so only fixed messages are kept.
		if exchange.isUnsentDialFailure {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return retryApplicationSend(sdkgo.FailureTransport, "WeCom could not be reached before sending")
		}
		return send.resendOrSelectUncertain(sdkgo.FailureTransport, "WeCom application message outcome is unknown")
	}
	return send.classifyResponse(call.Context, exchange, tokenKey, token.token)
}

// applicationSend holds one attempt's fence and its send-cap decision.
type applicationSend struct {
	fence                   *singledispatch.Fence
	operation               SendApplicationMessageOperation
	canResendUnknownOutcome bool
	hasPossibleEarlierSend  bool
}

func (send applicationSend) classifyResponse(ctx context.Context, exchange fencedExchange, tokenKey accessTokenCacheKey, accessToken string) sdkgo.MutationAttempt[SentApplicationMessage] {
	if exchange.statusCode == http.StatusTooManyRequests || exchange.statusCode == http.StatusServiceUnavailable {
		kind := sdkgo.FailureAvailability
		if exchange.statusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return send.retryUnprocessed(kind, "WeCom did not process the application message")
	}
	if isDefiniteRefusalStatus(exchange.statusCode) {
		if !exchange.isReadableRefusal() {
			// An unreadable or oversized refusal body, or one reporting a msgid, cannot prove nothing was sent.
			return send.resendOrSelectUncertain(sdkgo.FailureProtocol, "WeCom application message outcome is unknown after HTTP "+strconv.Itoa(exchange.statusCode))
		}
		return send.rejectOrSelectUncertain(sdkgo.FailureProviderRejection, "WeCom rejected the application message with HTTP "+strconv.Itoa(exchange.statusCode))
	}
	if !isSuccessStatus(exchange.statusCode) {
		return send.resendOrSelectUncertain(sdkgo.FailureAvailability, "WeCom application message outcome is unknown after HTTP "+strconv.Itoa(exchange.statusCode))
	}
	if exchange.isBrokenSuccessBody() {
		// WeCom answered 2xx before the body broke, so the message may be delivered and is never resent.
		return uncertainApplicationSend(sdkgo.FailureTransport, "WeCom answered the application message, but its response body broke; check with the recipients before sending again")
	}
	if exchange.bodyErr != nil {
		return send.resendOrSelectUncertain(sdkgo.FailureResponseTooLarge, "WeCom application message response exceeded maxResponseBytes")
	}
	errcode, ok := decodeProviderEnvelope(exchange.body, nil)
	if !ok {
		return send.resendOrSelectUncertain(sdkgo.FailureProtocol, "WeCom application message response is malformed")
	}
	switch {
	case errcode == errcodeSuccess:
		return send.acceptedResponse(exchange.body)
	case errcode == errcodeInvalidAccessToken || errcode == errcodeExpiredAccessToken || errcode == errcodeMissingAccessToken:
		// WeCom may revoke a token before its reported expiry; the next attempt fetches a new one.
		send.operation.client.accessTokens.invalidate(ctx, tokenKey, accessToken)
		return send.retryUnprocessed(failureKindForErrcode(errcode), rejectionMessage("WeCom rejected the cached access token", errcode))
	case isRetryableErrcode(errcode):
		return send.retryUnprocessed(failureKindForErrcode(errcode), rejectionMessage("WeCom deferred the application message", errcode))
	}
	return send.rejectOrSelectUncertain(failureKindForErrcode(errcode), rejectionMessage("WeCom rejected the application message", errcode))
}

// acceptedResponse builds the sent result of errcode 0 and checkpoints the acceptance first.
func (send applicationSend) acceptedResponse(body []byte) sdkgo.MutationAttempt[SentApplicationMessage] {
	var sendResponse struct {
		InvalidUsers    string `json:"invaliduser"`
		InvalidParties  string `json:"invalidparty"`
		InvalidTags     string `json:"invalidtag"`
		UnlicensedUsers string `json:"unlicenseduser"`
		MessageID       string `json:"msgid"`
	}
	if json.Unmarshal(body, &sendResponse) != nil {
		// WeCom accepted the message, so a resend could deliver it twice.
		return uncertainApplicationSend(sdkgo.FailureProtocol, "WeCom accepted the application message, but its response fields are malformed; check with the recipients before sending again")
	}
	acceptedMarker := applicationAcceptedMarker
	if messageIDPattern.MatchString(sendResponse.MessageID) {
		acceptedMarker = sendResponse.MessageID
	}
	// A lost acceptance checkpoint leaves the claim, which a later attempt counts as a send.
	_ = send.fence.RecordProviderID(acceptedMarker)
	return sdkgo.NewMutationBranch(SendApplicationMessageBranchSent, SentApplicationMessage{
		MessageID:            sendResponse.MessageID,
		InvalidUserIDs:       splitWeComList(sendResponse.InvalidUsers),
		InvalidDepartmentIDs: splitWeComList(sendResponse.InvalidParties),
		InvalidTagIDs:        splitWeComList(sendResponse.InvalidTags),
		UnlicensedUserIDs:    splitWeComList(sendResponse.UnlicensedUsers),
	}, nil, sdkgo.Receipt{})
}

// applicationAcceptedMarker is the checkpoint provider ID of an accepted send whose msgid is absent or unusual.
const applicationAcceptedMarker = "errcode-0"

// messageIDPattern bounds a msgid kept in the dispatch checkpoint.
var messageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_=+/-]{1,128}$`)

// newSentFromAcceptedMarker confirms a send an earlier attempt checkpointed. The skipped-recipient
// lists were not checkpointed, so they are empty.
func newSentFromAcceptedMarker(acceptedMarker string) sdkgo.MutationAttempt[SentApplicationMessage] {
	sent := SentApplicationMessage{InvalidUserIDs: []string{}, InvalidDepartmentIDs: []string{}, InvalidTagIDs: []string{}, UnlicensedUserIDs: []string{}}
	if acceptedMarker != applicationAcceptedMarker {
		sent.MessageID = acceptedMarker
	}
	return sdkgo.NewMutationBranch(SendApplicationMessageBranchSent, sent, nil, sdkgo.Receipt{})
}

// retryUnprocessed retries a send WeCom reported it did not process, so it is not counted.
func (send applicationSend) retryUnprocessed(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
	_ = send.fence.RecordCanResend()
	return retryApplicationSend(kind, message)
}

// resendOrSelectUncertain retries a possibly delivered send while the send cap allows another send.
func (send applicationSend) resendOrSelectUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	if send.canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return retryApplicationSend(kind, message)
	}
	return uncertainApplicationSend(kind, message+"; the send cap is reached, so check with the recipients before sending again")
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send of this call may have delivered.
func (send applicationSend) rejectOrSelectUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	if send.hasPossibleEarlierSend {
		// A refusal of this attempt cannot prove an earlier attempt delivered nothing.
		return uncertainApplicationSend(kind, message+"; an earlier attempt may have delivered the message")
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = send.fence.RecordCanResend()
	return rejectedApplicationSend(SendApplicationMessageBranchProviderRejected, kind, message)
}

// retryBeforeSending retries a transient failure that stopped this attempt before it sent.
func (send applicationSend) retryBeforeSending(failure sdkgo.Failure) sdkgo.MutationAttempt[SentApplicationMessage] {
	if !send.hasPossibleEarlierSend {
		// A lost notSent checkpoint leaves the previous marker, which permits the same send.
		_ = send.fence.RecordNotSent()
	}
	// After a possible earlier send, notSent would hide it, so nothing is recorded.
	return sdkgo.NewMutationRetry[SentApplicationMessage](failure, 0)
}

// localFailure selects defect before any possible send and uncertain after one.
func (send applicationSend) localFailure(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	return send.localFailureWithBranch(SendApplicationMessageBranchDefect, kind, message)
}

// localFailureWithBranch selects branch for a terminal failure before this attempt sent anything.
func (send applicationSend) localFailureWithBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	if send.hasPossibleEarlierSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		return uncertainApplicationSend(kind, message+"; an earlier attempt may have delivered the message")
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = send.fence.RecordNotSent()
	return rejectedApplicationSend(branch, kind, message)
}

func uncertainApplicationSend(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	return sdkgo.NewMutationUncertain(SentApplicationMessage{}, newFailure("sendApplicationMessage", kind, message), sdkgo.Receipt{})
}

// validateApplicationInput returns a safe message describing the first invalid field, or "".
func validateApplicationInput(input SendApplicationMessageInput) string {
	if input.Content == "" || !utf8.ValidString(input.Content) || len(input.Content) > maxApplicationContentBytes {
		return "content must be nonempty UTF-8 of at most 2048 bytes"
	}
	switch input.MessageType {
	case ApplicationMessageText:
	case ApplicationMessageMarkdown:
		if input.IsConfidential {
			return "isConfidential applies only to text messages"
		}
	default:
		return "messageType must be text or markdown"
	}
	hasRecipientList := len(input.ToUserIDs) > 0 || len(input.ToDepartmentIDs) > 0 || len(input.ToTagIDs) > 0
	if input.ToAllMembers == hasRecipientList {
		return "choose toAllMembers or at least one of toUserIds, toDepartmentIds, and toTagIds"
	}
	if len(input.ToUserIDs) > maxApplicationUserIDs || len(input.ToDepartmentIDs) > maxApplicationGroupIDs || len(input.ToTagIDs) > maxApplicationGroupIDs {
		return "send to at most 1000 user IDs, 100 department IDs, and 100 tag IDs"
	}
	for _, userID := range input.ToUserIDs {
		if !isPrintableIdentifier(userID, maxUserIDBytes) || userID == "@all" {
			return "each toUserIds entry must be a member user ID of at most 64 bytes; use toAllMembers for everyone"
		}
	}
	for _, groupID := range append(append([]string(nil), input.ToDepartmentIDs...), input.ToTagIDs...) {
		if !groupIDPattern.MatchString(groupID) {
			return "each toDepartmentIds and toTagIds entry must be a positive decimal ID"
		}
	}
	return ""
}

// applicationRequest field order is fixed, so every attempt for one input marshals the same bytes.
type applicationRequest struct {
	ToUser                 string              `json:"touser,omitempty"`
	ToParty                string              `json:"toparty,omitempty"`
	ToTag                  string              `json:"totag,omitempty"`
	MessageType            string              `json:"msgtype"`
	AgentID                int64               `json:"agentid"`
	Text                   *applicationContent `json:"text,omitempty"`
	Markdown               *applicationContent `json:"markdown,omitempty"`
	Safe                   int                 `json:"safe,omitempty"`
	EnableDuplicateCheck   int                 `json:"enable_duplicate_check"`
	DuplicateCheckInterval int64               `json:"duplicate_check_interval"`
}

type applicationContent struct {
	Content string `json:"content"`
}

func newApplicationRequest(input SendApplicationMessageInput, agentID int64) applicationRequest {
	request := applicationRequest{
		ToUser:                 strings.Join(input.ToUserIDs, "|"),
		ToParty:                strings.Join(input.ToDepartmentIDs, "|"),
		ToTag:                  strings.Join(input.ToTagIDs, "|"),
		MessageType:            string(input.MessageType),
		AgentID:                agentID,
		EnableDuplicateCheck:   1,
		DuplicateCheckInterval: int64(DuplicateCheckInterval / time.Second),
	}
	if input.ToAllMembers {
		request.ToUser = "@all"
	}
	content := &applicationContent{Content: input.Content}
	if input.MessageType == ApplicationMessageMarkdown {
		request.Markdown = content
		return request
	}
	request.Text = content
	if input.IsConfidential {
		request.Safe = 1
	}
	return request
}

// splitWeComList splits WeCom's "|"-separated ID lists, always returning a non-nil slice.
func splitWeComList(value string) []string {
	identifiers := []string{}
	for _, identifier := range strings.Split(value, "|") {
		if identifier != "" {
			identifiers = append(identifiers, identifier)
		}
	}
	return identifiers
}

func rejectedApplicationSend(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	failure := newFailure("sendApplicationMessage", kind, message)
	return sdkgo.NewMutationBranch(branch, SentApplicationMessage{}, &failure, sdkgo.Receipt{})
}

func retryApplicationSend(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SentApplicationMessage] {
	return sdkgo.NewMutationRetry[SentApplicationMessage](newFailure("sendApplicationMessage", kind, message), 0)
}
