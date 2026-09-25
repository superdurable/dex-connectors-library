// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package slack connects Dex applications to Slack channel threads.
package slack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

const maximumThreadPageSize = 15

type Option func(*clientOptions)

type clientOptions struct {
	httpClient   *http.Client
	now          func() time.Time
	socketDialer socketDialer
}

// WithHTTPClient replaces the HTTP client used for Slack Web API calls.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for provider receipts.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

type Client struct {
	endpoint             *url.URL
	httpClient           *http.Client
	credentials          connector.CredentialProvider[Credentials]
	maxResponseBytes     int64
	maxMessageCharacters int
	now                  func() time.Time
	socketDialer         socketDialer
}

type Message struct {
	ChannelID       string `json:"channelId"`
	Timestamp       string `json:"timestamp"`
	ThreadTimestamp string `json:"threadTimestamp,omitempty"`
	UserID          string `json:"userId,omitempty"`
	Text            string `json:"text"`
}

type ListThreadMessagesInput struct {
	ChannelID       string `json:"channelId"`
	ThreadTimestamp string `json:"threadTimestamp"`
	Cursor          string `json:"cursor,omitempty"`
	PageSize        int    `json:"pageSize"`
}

type ListThreadMessagesOutput struct {
	Messages   []Message `json:"messages"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

type GetThreadReplyInput struct {
	ChannelID       string `json:"channelId"`
	ThreadTimestamp string `json:"threadTimestamp"`
	ReplyTimestamp  string `json:"replyTimestamp"`
}

type GetThreadReplyOutput struct {
	Message Message `json:"message"`
}

type PostChannelMessageInput struct {
	ChannelID string `json:"channelId"`
	Text      string `json:"text"`
}

type PostThreadReplyInput struct {
	ChannelID       string `json:"channelId"`
	ThreadTimestamp string `json:"threadTimestamp"`
	Text            string `json:"text"`
}

type PostMessageOutput struct {
	Message Message `json:"message"`
}

type ListThreadMessagesOperation struct{ client *Client }
type GetThreadReplyOperation struct{ client *Client }
type PostChannelMessageOperation struct{ client *Client }
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

func New(config Config, credentials connector.CredentialProvider[Credentials], options ...Option) (*Client, error) {
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
		maxResponseBytes: config.MaxResponseBytes, maxMessageCharacters: int(config.MaxMessageCharacters),
		now: dependencies.now, socketDialer: dependencies.socketDialer,
	}, nil
}

func (client *Client) ListThreadMessages() ListThreadMessagesOperation {
	return ListThreadMessagesOperation{client: client}
}

func (client *Client) GetThreadReply() GetThreadReplyOperation {
	return GetThreadReplyOperation{client: client}
}

func (client *Client) PostChannelMessage() PostChannelMessageOperation {
	return PostChannelMessageOperation{client: client}
}

func (client *Client) PostThreadReply() PostThreadReplyOperation {
	return PostThreadReplyOperation{client: client}
}

func (ListThreadMessagesOperation) Definition() connector.QueryDefinition {
	return ListThreadMessagesDefinition
}

func (operation ListThreadMessagesOperation) Invoke(call connector.Call, input ListThreadMessagesInput) connector.QueryAttempt[ListThreadMessagesOutput] {
	if strings.TrimSpace(input.ChannelID) == "" || strings.TrimSpace(input.ThreadTimestamp) == "" || input.PageSize < 1 || input.PageSize > maximumThreadPageSize {
		return connector.NewQueryBranch(ListThreadMessagesBranchDefect, ListThreadMessagesOutput{}, slackFailurePointer("listThreadMessages", connector.FailureValidation, "channel, thread timestamp, and page size from 1 through 15 are required"), connector.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "listThreadMessages")
	if failure != nil {
		return connector.NewQueryBranch(ListThreadMessagesBranchRejected, ListThreadMessagesOutput{}, failure, connector.Receipt{})
	}
	values := url.Values{"channel": {input.ChannelID}, "ts": {input.ThreadTimestamp}, "limit": {strconv.Itoa(input.PageSize)}}
	if input.Cursor != "" {
		values.Set("cursor", input.Cursor)
	}
	response, err := operation.client.get(call, credentials.UserToken.Reveal(), "conversations.replies", values)
	if err != nil {
		return connector.NewQueryRetry[ListThreadMessagesOutput](slackFailure("listThreadMessages", connector.FailureAvailability, "Slack thread messages are temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusTooManyRequests {
		return connector.NewQueryRetry[ListThreadMessagesOutput](slackFailure("listThreadMessages", connector.FailureRateLimit, "Slack rate limited the thread query"), retryAfter(response.header))
	}
	if failure := classifyResponse("listThreadMessages", response); failure != nil {
		return connector.NewQueryBranch(ListThreadMessagesBranchRejected, ListThreadMessagesOutput{}, failure, operation.client.receipt(call, response, ""))
	}
	messages := make([]Message, len(response.decoded.Messages))
	for index, message := range response.decoded.Messages {
		messages[index] = convertMessage(input.ChannelID, message)
	}
	return connector.NewQueryBranch(ListThreadMessagesBranchRead, ListThreadMessagesOutput{Messages: messages, NextCursor: response.decoded.ResponseMetadata.NextCursor}, nil, operation.client.receipt(call, response, ""))
}

func (GetThreadReplyOperation) Definition() connector.QueryDefinition {
	return GetThreadReplyDefinition
}

func (operation GetThreadReplyOperation) Invoke(call connector.Call, input GetThreadReplyInput) connector.QueryAttempt[GetThreadReplyOutput] {
	if strings.TrimSpace(input.ChannelID) == "" || strings.TrimSpace(input.ThreadTimestamp) == "" || strings.TrimSpace(input.ReplyTimestamp) == "" || input.ReplyTimestamp == input.ThreadTimestamp {
		return connector.NewQueryBranch(GetThreadReplyBranchDefect, GetThreadReplyOutput{}, slackFailurePointer("getThreadReply", connector.FailureValidation, "channel, thread timestamp, and a distinct reply timestamp are required"), connector.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "getThreadReply")
	if failure != nil {
		return connector.NewQueryBranch(GetThreadReplyBranchRejected, GetThreadReplyOutput{}, failure, connector.Receipt{})
	}
	values := url.Values{
		"channel": {input.ChannelID}, "ts": {input.ThreadTimestamp}, "oldest": {input.ReplyTimestamp},
		"latest": {input.ReplyTimestamp}, "inclusive": {"true"}, "limit": {"1"},
	}
	response, err := operation.client.get(call, credentials.UserToken.Reveal(), "conversations.replies", values)
	if err != nil {
		return connector.NewQueryRetry[GetThreadReplyOutput](slackFailure("getThreadReply", connector.FailureAvailability, "Slack thread reply is temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusTooManyRequests {
		return connector.NewQueryRetry[GetThreadReplyOutput](slackFailure("getThreadReply", connector.FailureRateLimit, "Slack rate limited the reply query"), retryAfter(response.header))
	}
	if failure := classifyResponse("getThreadReply", response); failure != nil {
		return connector.NewQueryBranch(GetThreadReplyBranchRejected, GetThreadReplyOutput{}, failure, operation.client.receipt(call, response, ""))
	}
	for _, message := range response.decoded.Messages {
		if message.Timestamp == input.ReplyTimestamp && message.ThreadTS == input.ThreadTimestamp {
			return connector.NewQueryBranch(GetThreadReplyBranchFound, GetThreadReplyOutput{Message: convertMessage(input.ChannelID, message)}, nil, operation.client.receipt(call, response, message.Timestamp))
		}
	}
	return connector.NewQueryBranch(GetThreadReplyBranchNotFound, GetThreadReplyOutput{}, slackFailurePointer("getThreadReply", connector.FailureNotFound, "Slack thread reply was not found"), operation.client.receipt(call, response, ""))
}

func (PostChannelMessageOperation) Definition() connector.MutationDefinition {
	return PostChannelMessageDefinition
}

func (PostChannelMessageOperation) IdempotencyKey(callID connector.CallID, _ PostChannelMessageInput) connector.IdempotencyKey {
	return connector.IdempotencyKey(callID)
}

func (operation PostChannelMessageOperation) Invoke(call connector.Call, input PostChannelMessageInput) connector.MutationAttempt[PostMessageOutput] {
	return operation.client.postMessage(call, "postChannelMessage", input.ChannelID, "", input.Text, PostChannelMessageBranchSent, PostChannelMessageBranchRejected, PostChannelMessageBranchUncertain, PostChannelMessageBranchDefect)
}

func (PostThreadReplyOperation) Definition() connector.MutationDefinition {
	return PostThreadReplyDefinition
}

func (PostThreadReplyOperation) IdempotencyKey(callID connector.CallID, _ PostThreadReplyInput) connector.IdempotencyKey {
	return connector.IdempotencyKey(callID)
}

func (operation PostThreadReplyOperation) Invoke(call connector.Call, input PostThreadReplyInput) connector.MutationAttempt[PostMessageOutput] {
	return operation.client.postMessage(call, "postThreadReply", input.ChannelID, input.ThreadTimestamp, input.Text, PostThreadReplyBranchSent, PostThreadReplyBranchRejected, PostThreadReplyBranchUncertain, PostThreadReplyBranchDefect)
}

func (client *Client) postMessage(call connector.Call, operationName string, channelID string, threadTimestamp string, text string, sent connector.BranchID, rejected connector.BranchID, uncertain connector.BranchID, defect connector.BranchID) connector.MutationAttempt[PostMessageOutput] {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(text) == "" || len([]rune(text)) > client.maxMessageCharacters {
		return connector.NewMutationBranch(defect, PostMessageOutput{}, slackFailurePointer(operationName, connector.FailureValidation, "channel and bounded non-empty text are required"), connector.Receipt{})
	}
	credentials, failure := client.resolveCredentials(call, operationName)
	if failure != nil {
		return connector.NewMutationBranch(rejected, PostMessageOutput{}, failure, connector.Receipt{})
	}
	payload := map[string]string{"channel": channelID, "text": text, "client_msg_id": string(call.IdempotencyKey)}
	if threadTimestamp != "" {
		payload["thread_ts"] = threadTimestamp
	}
	response, err := client.post(call, credentials.BotToken.Reveal(), "chat.postMessage", payload)
	if err != nil {
		return connector.NewMutationUncertain(PostMessageOutput{}, slackFailure(operationName, connector.FailureTransport, "Slack message outcome is unknown"), client.receipt(call, response, ""))
	}
	if response.statusCode == http.StatusTooManyRequests {
		return connector.NewMutationRetry[PostMessageOutput](slackFailure(operationName, connector.FailureRateLimit, "Slack rate limited the message"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return connector.NewMutationUncertain(PostMessageOutput{}, slackFailure(operationName, connector.FailureAvailability, "Slack message outcome is unknown"), client.receipt(call, response, ""))
	}
	if failure := classifyResponse(operationName, response); failure != nil {
		return connector.NewMutationBranch(rejected, PostMessageOutput{}, failure, client.receipt(call, response, ""))
	}
	message := response.decoded.Message
	if message.Timestamp == "" {
		message.Timestamp = response.decoded.Timestamp
	}
	if message.Channel == "" {
		message.Channel = response.decoded.Channel
	}
	if message.Timestamp == "" || message.Channel == "" {
		return connector.NewMutationUncertain(PostMessageOutput{}, slackFailure(operationName, connector.FailureProtocol, "Slack returned an invalid message response"), client.receipt(call, response, ""))
	}
	return connector.NewMutationBranch(sent, PostMessageOutput{Message: convertMessage(message.Channel, message)}, nil, client.receipt(call, response, message.Timestamp))
}

func (client *Client) resolveCredentials(call connector.Call, operationName string) (Credentials, *connector.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return Credentials{}, slackFailurePointer(operationName, connector.FailureAuthentication, "Slack connection credentials are unavailable")
	}
	return credentials, nil
}

func (client *Client) get(call connector.Call, token string, method string, values url.Values) (providerResponse, error) {
	target := strings.TrimRight(client.endpoint.String(), "/") + "/" + method
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, target, nil)
	if err != nil {
		return providerResponse{}, err
	}
	return client.do(request, token)
}

func (client *Client) post(call connector.Call, token string, method string, payload any) (providerResponse, error) {
	contents, err := json.Marshal(payload)
	if err != nil {
		return providerResponse{}, err
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/" + method
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, target, bytes.NewReader(contents))
	if err != nil {
		return providerResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	return client.do(request, token)
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
		return result, errors.New("Slack response exceeds configured size limit")
	}
	if len(result.body) == 0 {
		return result, errors.New("Slack returned an empty response")
	}
	if err := json.Unmarshal(result.body, &result.decoded); err != nil {
		return result, err
	}
	return result, nil
}

func classifyResponse(operationName string, response providerResponse) *connector.Failure {
	if response.statusCode >= 200 && response.statusCode < 300 && response.decoded.OK {
		return nil
	}
	kind := connector.FailureProviderRejection
	switch response.statusCode {
	case http.StatusUnauthorized:
		kind = connector.FailureAuthentication
	case http.StatusForbidden:
		kind = connector.FailureAuthorization
	case http.StatusNotFound:
		kind = connector.FailureNotFound
	}
	switch response.decoded.Error {
	case "invalid_auth", "not_authed", "token_revoked", "account_inactive":
		kind = connector.FailureAuthentication
	case "missing_scope", "no_permission":
		kind = connector.FailureAuthorization
	case "channel_not_found", "thread_not_found", "message_not_found":
		kind = connector.FailureNotFound
	}
	return slackFailurePointer(operationName, kind, "Slack rejected the request")
}

func convertMessage(channelID string, message slackMessage) Message {
	return Message{ChannelID: channelID, Timestamp: message.Timestamp, ThreadTimestamp: message.ThreadTS, UserID: message.User, Text: message.Text}
}

func slackFailure(operationName string, kind connector.FailureKind, message string) connector.Failure {
	return connector.Failure{Kind: kind, Provider: "slack", Operation: operationName, Message: message}
}

func slackFailurePointer(operationName string, kind connector.FailureKind, message string) *connector.Failure {
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

func (client *Client) receipt(call connector.Call, response providerResponse, objectID string) connector.Receipt {
	return connector.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "slack", ProviderObjectID: objectID,
		ProviderRequestID: response.header.Get("X-Slack-Req-Id"), ObservedAt: client.now().UTC(),
	}
}
