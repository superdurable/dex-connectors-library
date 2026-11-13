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

// Invoke sends one message. Every attempt sends byte-identical JSON with enable_duplicate_check,
// so WeCom suppresses a repeat of an accepted send and an unconfirmed attempt can safely retry
// until DuplicateCheckResendCutoff after the first attempt; a later attempt selects uncertain.
func (operation SendApplicationMessageOperation) Invoke(call sdkgo.Call, input SendApplicationMessageInput) sdkgo.MutationAttempt[SentApplicationMessage] {
	if message := validateApplicationInput(input); message != "" {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureValidation, message)
	}
	// The first attempt starts no later than any earlier send, so this elapsed time never understates the window used.
	if call.Context.Attempt() > 1 && !operation.client.now().Before(call.Context.FirstAttemptAt().Add(DuplicateCheckResendCutoff)) {
		failure := newFailure("sendApplicationMessage", sdkgo.FailureTransport, "an earlier attempt may have delivered the message and WeCom's four-hour duplicate check would no longer suppress a resend; check with the recipients before sending again")
		return sdkgo.NewMutationUncertain(SentApplicationMessage{}, failure, sdkgo.Receipt{})
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureAuthentication, "WeCom application credentials are unavailable")
	}
	if credentials.AuthMethodID != applicationAuthMethodID {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureValidation, "sendApplicationMessage needs a connection that uses the application method")
	}
	corpSecret := credentials.CorpSecret.Reveal()
	if !corpIDPattern.MatchString(credentials.CorpID) || !agentIDPattern.MatchString(credentials.AgentID) || !providerhttp.IsHeaderSafeCredential(corpSecret) {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureAuthentication, "corp_id, agent_id, or corp_secret has an invalid format")
	}
	agentID, err := strconv.ParseInt(credentials.AgentID, 10, 64)
	if err != nil {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureAuthentication, "agent_id must be a positive decimal integer")
	}
	body, err := json.Marshal(newApplicationRequest(input, agentID))
	if err != nil {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureLocalDefect, "WeCom application message could not be encoded")
	}
	tokenKey := newAccessTokenCacheKey(credentials.CorpID, corpSecret)
	token := operation.client.accessTokens.resolve(call.Context, operation.client, "sendApplicationMessage", tokenKey, corpSecret)
	switch {
	case token.shouldRetry:
		return sdkgo.NewMutationRetry[SentApplicationMessage](token.failure, 0)
	case token.isLocalDefect:
		return sdkgo.NewMutationBranch(SendApplicationMessageBranchDefect, SentApplicationMessage{}, &token.failure, sdkgo.Receipt{})
	case token.isRejected:
		return sdkgo.NewMutationBranch(SendApplicationMessageBranchProviderRejected, SentApplicationMessage{}, &token.failure, sdkgo.Receipt{})
	}
	endpoint := operation.client.baseURL + "/cgi-bin/message/send?" + url.Values{"access_token": {token.token}}.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return rejectedApplicationSend(SendApplicationMessageBranchDefect, sdkgo.FailureLocalDefect, "WeCom application message request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		// The error embeds the request URL, which carries the access token, so only a fixed message is kept.
		return retryApplicationSend(sdkgo.FailureTransport, "WeCom application message request failed")
	}
	// Closing the response cannot change the provider outcome already received.
	defer func() { _ = response.Body.Close() }()
	return operation.classifyResponse(call.Context, response, tokenKey, token.token)
}

func (operation SendApplicationMessageOperation) classifyResponse(ctx context.Context, response *http.Response, tokenKey accessTokenCacheKey, accessToken string) sdkgo.MutationAttempt[SentApplicationMessage] {
	if response.StatusCode == http.StatusTooManyRequests {
		return retryApplicationSend(sdkgo.FailureRateLimit, "WeCom rate limited the application message")
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryApplicationSend(sdkgo.FailureAvailability, "WeCom application message service is unavailable")
	}
	if response.StatusCode != http.StatusOK {
		return rejectedApplicationSend(SendApplicationMessageBranchProviderRejected, sdkgo.FailureProviderRejection, "WeCom rejected the application message with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return retryApplicationSend(sdkgo.FailureResponseTooLarge, "WeCom application message response exceeded maxResponseBytes")
	}
	if err != nil {
		return retryApplicationSend(sdkgo.FailureTransport, "WeCom application message response was interrupted")
	}
	var sendResponse struct {
		InvalidUsers    string `json:"invaliduser"`
		InvalidParties  string `json:"invalidparty"`
		InvalidTags     string `json:"invalidtag"`
		UnlicensedUsers string `json:"unlicenseduser"`
		MessageID       string `json:"msgid"`
	}
	errcode, ok := decodeProviderEnvelope(body, &sendResponse)
	if !ok {
		return retryApplicationSend(sdkgo.FailureProtocol, "WeCom application message response is malformed")
	}
	switch {
	case errcode == errcodeSuccess:
		return sdkgo.NewMutationBranch(SendApplicationMessageBranchSent, SentApplicationMessage{
			MessageID:            sendResponse.MessageID,
			InvalidUserIDs:       splitWeComList(sendResponse.InvalidUsers),
			InvalidDepartmentIDs: splitWeComList(sendResponse.InvalidParties),
			InvalidTagIDs:        splitWeComList(sendResponse.InvalidTags),
			UnlicensedUserIDs:    splitWeComList(sendResponse.UnlicensedUsers),
		}, nil, sdkgo.Receipt{})
	case errcode == errcodeInvalidAccessToken || errcode == errcodeExpiredAccessToken || errcode == errcodeMissingAccessToken:
		// WeCom may revoke a token before its reported expiry; the next attempt fetches a new one.
		operation.client.accessTokens.invalidate(ctx, tokenKey, accessToken)
		return retryApplicationSend(failureKindForErrcode(errcode), rejectionMessage("WeCom rejected the cached access token", errcode))
	case isRetryableErrcode(errcode):
		return retryApplicationSend(failureKindForErrcode(errcode), rejectionMessage("WeCom deferred the application message", errcode))
	}
	return rejectedApplicationSend(SendApplicationMessageBranchProviderRejected, failureKindForErrcode(errcode), rejectionMessage("WeCom rejected the application message", errcode))
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
