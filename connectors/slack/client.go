// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package slack connects Dex applications to Slack channel threads.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const maximumThreadPageSize = 15

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient   *http.Client
	now          func() time.Time
	socketDialer socketDialer
	logger       *slog.Logger
}

// WithHTTPClient replaces the HTTP client used for Slack Web API calls.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for provider receipts.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the Trigger runner's records to logger: Socket Mode connections, ignored and skipped
// events, delivery retries, and the durable inboxes that NewLocalMessageTriggerRunner creates. Without
// this option, or with a nil logger, records go to slog.Default() as of each record. Records carry IDs
// and error messages, never message text or tokens. The generated per-Trigger factories, such as
// NewLocalThreadReplyCreatedTrigger, pass logger to their Socket Mode source only; their durable inbox
// and runner records go to slog.Default(), so call slog.SetDefault when you use them.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated Slack requests for connector operations.
type Client struct {
	endpoint             *url.URL
	httpClient           *http.Client
	credentials          sdkgo.CredentialProvider[Credentials]
	refreshDriver        sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes     int64
	maxMessageCharacters int
	now                  func() time.Time
	socketDialer         socketDialer
	logger               *slog.Logger
}

// Message represents the connector's message data.
type Message struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// Timestamp is the Slack message timestamp identifier.
	Timestamp string `json:"timestamp"`
	// ThreadTimestamp is the Slack parent message timestamp.
	ThreadTimestamp string `json:"threadTimestamp,omitempty"`
	// UserID is the Slack user identifier.
	UserID string `json:"userId,omitempty"`
	// Text is the text for message.
	Text string `json:"text"`
}

// ListThreadMessagesInput contains the provider request fields for list thread messages.
type ListThreadMessagesInput struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// ThreadTimestamp is the Slack parent message timestamp.
	ThreadTimestamp string `json:"threadTimestamp"`
	// Cursor specifies cursor for list thread messages input.
	Cursor string `json:"cursor,omitempty"`
	// PageSize specifies page size for list thread messages input.
	PageSize int `json:"pageSize"`
}

// ListThreadMessagesOutput contains the provider response fields for list thread messages.
type ListThreadMessagesOutput struct {
	// Messages is the messages returned by Slack.
	Messages []Message `json:"messages"`
	// NextCursor is the next cursor returned by Slack.
	NextCursor string `json:"nextCursor,omitempty"`
}

// GetThreadReplyInput contains the provider request fields for get thread reply.
type GetThreadReplyInput struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// ThreadTimestamp is the Slack parent message timestamp.
	ThreadTimestamp string `json:"threadTimestamp"`
	// ReplyTimestamp is the Slack reply message timestamp.
	ReplyTimestamp string `json:"replyTimestamp"`
}

// GetThreadReplyOutput contains the provider response fields for get thread reply.
type GetThreadReplyOutput struct {
	// Message is the message returned by Slack.
	Message Message `json:"message"`
}

// PostChannelMessageInput contains the provider request fields for post channel message.
type PostChannelMessageInput struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// Text specifies text for post channel message input.
	Text string `json:"text"`
}

// PostThreadReplyInput contains the provider request fields for post thread reply.
type PostThreadReplyInput struct {
	// ChannelID is the Slack channel identifier.
	ChannelID string `json:"channelId"`
	// ThreadTimestamp is the Slack parent message timestamp.
	ThreadTimestamp string `json:"threadTimestamp"`
	// Text specifies text for post thread reply input.
	Text string `json:"text"`
}

// PostMessageOutput contains the provider response fields for post message.
type PostMessageOutput struct {
	// Message is the message returned by Slack.
	Message Message `json:"message"`
}

// ListThreadMessagesOperation implements the list thread messages connector operation.
type ListThreadMessagesOperation struct{ client *Client }

// GetThreadReplyOperation implements the get thread reply connector operation.
type GetThreadReplyOperation struct{ client *Client }

// PostChannelMessageOperation implements the post channel message connector operation.
type PostChannelMessageOperation struct{ client *Client }

// PostThreadReplyOperation implements the post thread reply connector operation.
type PostThreadReplyOperation struct{ client *Client }

type slackMessage struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"ts"`
	ThreadTS  string `json:"thread_ts"`
	User      string `json:"user"`
	Text      string `json:"text"`
}

type slackResponse struct {
	OK               bool           `json:"ok"`
	Error            string         `json:"error"`
	Channel          string         `json:"channel"`
	Timestamp        string         `json:"ts"`
	Message          slackMessage   `json:"message"`
	Messages         []slackMessage `json:"messages"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

type providerResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	decoded    slackResponse
}

var (
	errSlackRequestInvalid   = errors.New("Slack request is invalid")
	errSlackResponseInvalid  = errors.New("Slack response is invalid")
	errSlackResponseTooLarge = errors.New("Slack response exceeds configured size limit")
)

// New validates configuration and constructs an authenticated Slack client.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("Slack endpoint must be absolute")
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("Slack endpoint must use HTTPS")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now, socketDialer: newWebSocketConnection}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Slack connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	if dependencies.now == nil || dependencies.socketDialer == nil {
		return nil, fmt.Errorf("Slack connector clock and Socket Mode dialer are required")
	}
	return &Client{
		endpoint: endpoint, httpClient: dependencies.httpClient, credentials: credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, maxMessageCharacters: int(config.MaxMessageCharacters),
		now: dependencies.now, socketDialer: dependencies.socketDialer, logger: dependencies.logger,
	}, nil
}

// ListThreadMessages returns the ListThreadMessages operation bound to this client.
func (client *Client) ListThreadMessages() ListThreadMessagesOperation {
	return ListThreadMessagesOperation{client: client}
}

// GetThreadReply returns the GetThreadReply operation bound to this client.
func (client *Client) GetThreadReply() GetThreadReplyOperation {
	return GetThreadReplyOperation{client: client}
}

// PostChannelMessage returns the PostChannelMessage operation bound to this client.
func (client *Client) PostChannelMessage() PostChannelMessageOperation {
	return PostChannelMessageOperation{client: client}
}

// PostThreadReply returns the PostThreadReply operation bound to this client.
func (client *Client) PostThreadReply() PostThreadReplyOperation {
	return PostThreadReplyOperation{client: client}
}

func (client *Client) channelThreadCreatedTriggerSource(connection sdkgo.ConnectionRef, configuration ChannelThreadCreatedTriggerConfiguration) *messageTriggerSource {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messageTriggerSource{
		client: client, connection: connection,
		channelID: configuration.ChannelID, matcher: configuration.ThreadTriggerMatcher, triggerName: "channelThreadCreated",
	}
}

func (client *Client) threadReplyCreatedTriggerSource(connection sdkgo.ConnectionRef, configuration ThreadReplyCreatedTriggerConfiguration) *messageTriggerSource {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	return &messageTriggerSource{
		client: client, connection: connection,
		channelID: configuration.ChannelID, matcher: configuration.ThreadReplyMatcher, requiresThread: true,
		triggerName: "threadReplyCreated",
	}
}

// triggerLogger returns the configured logger or, as of the call, slog.Default().
func (client *Client) triggerLogger() *slog.Logger {
	if client != nil && client.logger != nil {
		return client.logger
	}
	return slog.Default()
}

func (client *Client) openSocketModeConnection(ctx context.Context, appToken string) (string, error) {
	target := strings.TrimRight(client.endpoint.String(), "/") + "/apps.connections.open"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return "", fmt.Errorf("build Slack Socket Mode open request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+appToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("open Slack Socket Mode connection: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil || int64(len(contents)) > client.maxResponseBytes {
		return "", fmt.Errorf("read Slack Socket Mode open response")
	}
	var decoded socketOpenResponse
	decodeErr := json.Unmarshal(contents, &decoded)
	if response.StatusCode >= 200 && response.StatusCode < 300 && decodeErr != nil {
		return "", fmt.Errorf("decode Slack Socket Mode open response: %w", decodeErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !decoded.OK || decoded.URL == "" {
		// Slack's error code names the cause without revealing the token.
		return "", fmt.Errorf("Slack rejected the Socket Mode connection: %s (HTTP %d)", slackCode(decoded.Error), response.StatusCode)
	}
	return decoded.URL, nil
}

// Definition returns the immutable connector operation definition.
func (ListThreadMessagesOperation) Definition() sdkgo.QueryDefinition {
	return ListThreadMessagesDefinition
}

// Invoke executes one provider call and classifies its attempt.
func (operation ListThreadMessagesOperation) Invoke(call sdkgo.Call, input ListThreadMessagesInput) sdkgo.QueryAttempt[ListThreadMessagesOutput] {
	if strings.TrimSpace(input.ChannelID) == "" || strings.TrimSpace(input.ThreadTimestamp) == "" || input.PageSize < 1 || input.PageSize > maximumThreadPageSize {
		return sdkgo.NewQueryBranch(ListThreadMessagesBranchDefect, ListThreadMessagesOutput{}, slackFailurePointer("listThreadMessages", sdkgo.FailureValidation, "channel, thread timestamp, and page size from 1 through 15 are required"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "listThreadMessages", userCredentialToken)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListThreadMessagesBranchDefect, ListThreadMessagesOutput{}, failure, sdkgo.Receipt{})
	}
	values := url.Values{"channel": {input.ChannelID}, "ts": {input.ThreadTimestamp}, "limit": {strconv.Itoa(input.PageSize)}}
	if input.Cursor != "" {
		values.Set("cursor", input.Cursor)
	}
	response, err := operation.client.get(call, &credentials, userCredentialToken, "conversations.replies", values)
	if err != nil {
		if errors.Is(err, errSlackResponseTooLarge) {
			return sdkgo.NewQueryBranch(ListThreadMessagesBranchInvalidResponse, ListThreadMessagesOutput{}, slackFailurePointer("listThreadMessages", sdkgo.FailureResponseTooLarge, err.Error()), operation.client.receipt(call, response, ""))
		}
		if errors.Is(err, errSlackResponseInvalid) {
			return sdkgo.NewQueryBranch(ListThreadMessagesBranchInvalidResponse, ListThreadMessagesOutput{}, slackFailurePointer("listThreadMessages", sdkgo.FailureProtocol, err.Error()), operation.client.receipt(call, response, ""))
		}
		return sdkgo.NewQueryRetry[ListThreadMessagesOutput](slackFailure("listThreadMessages", sdkgo.FailureAvailability, "Slack thread messages are temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ListThreadMessagesOutput](slackFailure("listThreadMessages", sdkgo.FailureRateLimit, "Slack rate limited the thread query"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return sdkgo.NewQueryRetry[ListThreadMessagesOutput](slackFailure("listThreadMessages", sdkgo.FailureAvailability, "Slack thread messages are temporarily unavailable"), 0)
	}
	if failure := classifyResponse("listThreadMessages", response); failure != nil {
		return sdkgo.NewQueryBranch(ListThreadMessagesBranchProviderRejected, ListThreadMessagesOutput{}, failure, operation.client.receipt(call, response, ""))
	}
	messages := make([]Message, len(response.decoded.Messages))
	for index, message := range response.decoded.Messages {
		messages[index] = convertMessage(input.ChannelID, message)
	}
	return sdkgo.NewQueryBranch(ListThreadMessagesBranchRead, ListThreadMessagesOutput{Messages: messages, NextCursor: response.decoded.ResponseMetadata.NextCursor}, nil, operation.client.receipt(call, response, ""))
}

// Definition returns the immutable connector operation definition.
func (GetThreadReplyOperation) Definition() sdkgo.QueryDefinition {
	return GetThreadReplyDefinition
}

// Invoke executes one provider call and classifies its attempt.
func (operation GetThreadReplyOperation) Invoke(call sdkgo.Call, input GetThreadReplyInput) sdkgo.QueryAttempt[GetThreadReplyOutput] {
	if strings.TrimSpace(input.ChannelID) == "" || strings.TrimSpace(input.ThreadTimestamp) == "" || strings.TrimSpace(input.ReplyTimestamp) == "" || input.ReplyTimestamp == input.ThreadTimestamp {
		return sdkgo.NewQueryBranch(GetThreadReplyBranchDefect, GetThreadReplyOutput{}, slackFailurePointer("getThreadReply", sdkgo.FailureValidation, "channel, thread timestamp, and a distinct reply timestamp are required"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "getThreadReply", userCredentialToken)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetThreadReplyBranchDefect, GetThreadReplyOutput{}, failure, sdkgo.Receipt{})
	}
	values := url.Values{
		"channel": {input.ChannelID}, "ts": {input.ThreadTimestamp}, "oldest": {input.ReplyTimestamp},
		"latest": {input.ReplyTimestamp}, "inclusive": {"true"}, "limit": {"1"},
	}
	response, err := operation.client.get(call, &credentials, userCredentialToken, "conversations.replies", values)
	if err != nil {
		if errors.Is(err, errSlackResponseTooLarge) {
			return sdkgo.NewQueryBranch(GetThreadReplyBranchInvalidResponse, GetThreadReplyOutput{}, slackFailurePointer("getThreadReply", sdkgo.FailureResponseTooLarge, err.Error()), operation.client.receipt(call, response, ""))
		}
		if errors.Is(err, errSlackResponseInvalid) {
			return sdkgo.NewQueryBranch(GetThreadReplyBranchInvalidResponse, GetThreadReplyOutput{}, slackFailurePointer("getThreadReply", sdkgo.FailureProtocol, err.Error()), operation.client.receipt(call, response, ""))
		}
		return sdkgo.NewQueryRetry[GetThreadReplyOutput](slackFailure("getThreadReply", sdkgo.FailureAvailability, "Slack thread reply is temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[GetThreadReplyOutput](slackFailure("getThreadReply", sdkgo.FailureRateLimit, "Slack rate limited the reply query"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return sdkgo.NewQueryRetry[GetThreadReplyOutput](slackFailure("getThreadReply", sdkgo.FailureAvailability, "Slack thread reply is temporarily unavailable"), 0)
	}
	if failure := classifyResponse("getThreadReply", response); failure != nil {
		return sdkgo.NewQueryBranch(GetThreadReplyBranchProviderRejected, GetThreadReplyOutput{}, failure, operation.client.receipt(call, response, ""))
	}
	for _, message := range response.decoded.Messages {
		if message.Timestamp == input.ReplyTimestamp && message.ThreadTS == input.ThreadTimestamp {
			return sdkgo.NewQueryBranch(GetThreadReplyBranchFound, GetThreadReplyOutput{Message: convertMessage(input.ChannelID, message)}, nil, operation.client.receipt(call, response, message.Timestamp))
		}
	}
	return sdkgo.NewQueryBranch(GetThreadReplyBranchNotFound, GetThreadReplyOutput{}, slackFailurePointer("getThreadReply", sdkgo.FailureNotFound, "Slack thread reply was not found"), operation.client.receipt(call, response, ""))
}

// Definition returns the immutable connector operation definition.
func (PostChannelMessageOperation) Definition() sdkgo.MutationDefinition {
	return PostChannelMessageDefinition
}

// IdempotencyKey derives the provider key from the stable connector call ID.
func (PostChannelMessageOperation) IdempotencyKey(callID sdkgo.CallID, _ PostChannelMessageInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke executes one provider call and classifies its attempt.
func (operation PostChannelMessageOperation) Invoke(call sdkgo.Call, input PostChannelMessageInput) sdkgo.MutationAttempt[PostMessageOutput] {
	return operation.client.postMessage(call, "postChannelMessage", input.ChannelID, "", input.Text, PostChannelMessageBranchSent, PostChannelMessageBranchProviderRejected, PostChannelMessageBranchUncertain, PostChannelMessageBranchDefect)
}

// Definition returns the immutable connector operation definition.
func (PostThreadReplyOperation) Definition() sdkgo.MutationDefinition {
	return PostThreadReplyDefinition
}

// IdempotencyKey derives the provider key from the stable connector call ID.
func (PostThreadReplyOperation) IdempotencyKey(callID sdkgo.CallID, _ PostThreadReplyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke executes one provider call and classifies its attempt.
func (operation PostThreadReplyOperation) Invoke(call sdkgo.Call, input PostThreadReplyInput) sdkgo.MutationAttempt[PostMessageOutput] {
	return operation.client.postMessage(call, "postThreadReply", input.ChannelID, input.ThreadTimestamp, input.Text, PostThreadReplyBranchSent, PostThreadReplyBranchProviderRejected, PostThreadReplyBranchUncertain, PostThreadReplyBranchDefect)
}

func (client *Client) postMessage(call sdkgo.Call, operationName string, channelID string, threadTimestamp string, text string, sent sdkgo.BranchID, providerRejected sdkgo.BranchID, uncertain sdkgo.BranchID, defect sdkgo.BranchID) sdkgo.MutationAttempt[PostMessageOutput] {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(text) == "" || len([]rune(text)) > client.maxMessageCharacters {
		return sdkgo.NewMutationBranch(defect, PostMessageOutput{}, slackFailurePointer(operationName, sdkgo.FailureValidation, "channel and bounded non-empty text are required"), sdkgo.Receipt{})
	}
	credentials, failure := client.resolveCredentials(call, operationName, botCredentialToken)
	if failure != nil {
		return sdkgo.NewMutationBranch(defect, PostMessageOutput{}, failure, sdkgo.Receipt{})
	}
	payload := map[string]string{"channel": channelID, "text": text, "client_msg_id": string(call.IdempotencyKey)}
	if threadTimestamp != "" {
		payload["thread_ts"] = threadTimestamp
	}
	response, err := client.post(call, &credentials, botCredentialToken, "chat.postMessage", payload)
	if err != nil {
		if errors.Is(err, errSlackRequestInvalid) {
			return sdkgo.NewMutationBranch(defect, PostMessageOutput{}, slackFailurePointer(operationName, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationUncertain(PostMessageOutput{}, slackFailure(operationName, sdkgo.FailureTransport, "Slack message outcome is unknown"), client.receipt(call, response, ""))
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[PostMessageOutput](slackFailure(operationName, sdkgo.FailureRateLimit, "Slack rate limited the message"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return sdkgo.NewMutationUncertain(PostMessageOutput{}, slackFailure(operationName, sdkgo.FailureAvailability, "Slack message outcome is unknown"), client.receipt(call, response, ""))
	}
	if failure := classifyResponse(operationName, response); failure != nil {
		return sdkgo.NewMutationBranch(providerRejected, PostMessageOutput{}, failure, client.receipt(call, response, ""))
	}
	message := response.decoded.Message
	if message.Timestamp == "" {
		message.Timestamp = response.decoded.Timestamp
	}
	if message.Channel == "" {
		message.Channel = response.decoded.Channel
	}
	if message.Timestamp == "" || message.Channel == "" {
		return sdkgo.NewMutationUncertain(PostMessageOutput{}, slackFailure(operationName, sdkgo.FailureProtocol, "Slack returned an invalid message response"), client.receipt(call, response, ""))
	}
	return sdkgo.NewMutationBranch(sent, PostMessageOutput{Message: convertMessage(message.Channel, message)}, nil, client.receipt(call, response, message.Timestamp))
}

func (client *Client) resolveCredentials(call sdkgo.Call, operationName string, tokenKind credentialToken) (Credentials, *sdkgo.Failure) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentialToken(credentials, tokenKind) != nil {
		return Credentials{}, slackFailurePointer(operationName, sdkgo.FailureAuthentication, "Slack connection credentials are unavailable")
	}
	return credentials, nil
}

type credentialToken int

const (
	botCredentialToken credentialToken = iota
	userCredentialToken
)

func (client *Client) get(call sdkgo.Call, credentials *Credentials, tokenKind credentialToken, method string, values url.Values) (providerResponse, error) {
	target := strings.TrimRight(client.endpoint.String(), "/") + "/" + method
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return client.doAuthenticated(call, credentials, tokenKind, func() (*http.Request, error) {
		return http.NewRequestWithContext(call.Context, http.MethodGet, target, nil)
	})
}

func (client *Client) post(call sdkgo.Call, credentials *Credentials, tokenKind credentialToken, method string, payload any) (providerResponse, error) {
	contents, err := json.Marshal(payload)
	if err != nil {
		return providerResponse{}, fmt.Errorf("%w: request could not be encoded", errSlackRequestInvalid)
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/" + method
	return client.doAuthenticated(call, credentials, tokenKind, func() (*http.Request, error) {
		request, requestErr := http.NewRequestWithContext(call.Context, http.MethodPost, target, bytes.NewReader(contents))
		if requestErr != nil {
			return nil, fmt.Errorf("%w: request could not be built", errSlackRequestInvalid)
		}
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
		return request, nil
	})
}

func (client *Client) doAuthenticated(
	call sdkgo.Call,
	credentials *Credentials,
	tokenKind credentialToken,
	buildRequest func() (*http.Request, error),
) (providerResponse, error) {
	if err := validateResolvedCredentialToken(*credentials, tokenKind); err != nil {
		return providerResponse{}, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		request, err := buildRequest()
		if err != nil {
			return providerResponse{}, err
		}
		result, err := client.do(request, resolvedCredentialToken(*credentials, tokenKind))
		if err != nil {
			return result, err
		}
		if !slackCredentialRejected(result) || attempt != 0 {
			return result, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return result, nil
		}
		replacement, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentialToken(replacement, tokenKind) != nil {
			return result, nil
		}
		*credentials = replacement
	}
	return providerResponse{}, errors.New("Slack authenticated request retry was exhausted")
}

func (client *Client) do(request *http.Request, token string) (providerResponse, error) {
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{}, err
	}
	defer response.Body.Close()
	result := providerResponse{statusCode: response.StatusCode, header: response.Header.Clone()}
	result.body, err = io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil {
		return result, err
	}
	if int64(len(result.body)) > client.maxResponseBytes {
		return result, errSlackResponseTooLarge
	}
	if len(result.body) == 0 {
		return result, fmt.Errorf("%w: Slack returned an empty response", errSlackResponseInvalid)
	}
	if err := json.Unmarshal(result.body, &result.decoded); err != nil {
		return result, fmt.Errorf("%w: Slack returned malformed JSON", errSlackResponseInvalid)
	}
	return result, nil
}

func resolvedCredentialToken(credentials Credentials, tokenKind credentialToken) string {
	if tokenKind == userCredentialToken {
		return credentials.UserToken.Reveal()
	}
	return credentials.BotToken.Reveal()
}

func validateResolvedCredentialToken(credentials Credentials, tokenKind credentialToken) error {
	if resolvedCredentialToken(credentials, tokenKind) == "" {
		return errors.New("Slack operation credential is unavailable")
	}
	return nil
}

func slackCredentialRejected(response providerResponse) bool {
	if response.statusCode == http.StatusUnauthorized {
		return true
	}
	switch response.decoded.Error {
	case "invalid_auth", "token_expired", "token_revoked":
		return true
	default:
		return false
	}
}

func classifyResponse(operationName string, response providerResponse) *sdkgo.Failure {
	if response.statusCode >= 200 && response.statusCode < 300 && response.decoded.OK {
		return nil
	}
	kind := sdkgo.FailureProviderRejection
	switch response.statusCode {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	}
	switch response.decoded.Error {
	case "invalid_auth", "not_authed", "token_expired", "token_revoked", "account_inactive":
		kind = sdkgo.FailureAuthentication
	case "missing_scope", "no_permission":
		kind = sdkgo.FailureAuthorization
	case "channel_not_found", "thread_not_found", "message_not_found":
		kind = sdkgo.FailureNotFound
	}
	return slackFailurePointer(operationName, kind, "Slack rejected the request")
}

func convertMessage(channelID string, message slackMessage) Message {
	return Message{ChannelID: channelID, Timestamp: message.Timestamp, ThreadTimestamp: message.ThreadTS, UserID: message.User, Text: message.Text}
}

func slackFailure(operationName string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "slack", Operation: operationName, Message: message}
}

func slackFailurePointer(operationName string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := slackFailure(operationName, kind, message)
	return &failure
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds < 1 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (client *Client) receipt(call sdkgo.Call, response providerResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "slack", ProviderObjectID: objectID,
		ProviderRequestID: response.header.Get("X-Slack-Req-Id"), ObservedAt: client.now().UTC(),
	}
}
