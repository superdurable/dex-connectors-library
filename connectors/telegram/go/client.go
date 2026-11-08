// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package telegram reads Bot API chat metadata and sends text through typed Dex Steps.
package telegram

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP client dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable Bot API settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// GetChatInput selects a numeric chat ID or public @username.
type GetChatInput struct {
	// ChatID is a decimal chat ID or an @username visible to the bot.
	ChatID string `json:"chatId"`
}

// Chat contains basic non-secret metadata returned by Telegram getChat.
type Chat struct {
	// ID is Telegram's stable numeric chat identifier.
	ID int64 `json:"id"`
	// Type is private, group, supergroup, or channel.
	Type string `json:"type"`
	// Title is the current display title for a group or channel.
	Title string `json:"title"`
	// Username is the optional public @username without its prefix.
	Username string `json:"username"`
}

// GetChatOperation reads one accessible chat.
type GetChatOperation struct{ client *Client }

// New validates configuration without contacting Telegram. Credentials resolve at each call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Telegram credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Telegram Bot API base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Telegram client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetChat returns the typed chat lookup Query bound to this client.
func (client *Client) GetChat() GetChatOperation { return GetChatOperation{client} }

// Definition returns the manifest-generated chat lookup contract.
func (GetChatOperation) Definition() sdkgo.QueryDefinition { return GetChatDefinition }

var botTokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
var chatUsernamePattern = regexp.MustCompile(`^@[A-Za-z0-9_]{5,32}$`)

// Invoke reads chat metadata; provider errors never include the token-bearing URL.
func (operation GetChatOperation) Invoke(call sdkgo.Call, input GetChatInput) sdkgo.QueryAttempt[Chat] {
	if !validChatID(input.ChatID) {
		return failedQuery(GetChatBranchDefect, sdkgo.FailureValidation, "chatId must be a decimal ID or public @username")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return failedQuery(GetChatBranchDefect, sdkgo.FailureAuthentication, "bot token is unavailable or invalid")
	}
	token := credentials.BotToken.Reveal()
	if !botTokenPattern.MatchString(token) {
		return failedQuery(GetChatBranchDefect, sdkgo.FailureAuthentication, "bot token is invalid")
	}
	endpoint := operation.client.baseURL + "/bot" + token + "/getChat?" + url.Values{"chat_id": {input.ChatID}}.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return failedQuery(GetChatBranchDefect, sdkgo.FailureLocalDefect, "Telegram request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return retryQuery(sdkgo.FailureAvailability, "Telegram request failed", 0)
	}
	// Closing a read-only response cannot alter the query outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return retryQuery(sdkgo.FailureRateLimit, "Telegram rate limited the request", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return retryQuery(sdkgo.FailureAvailability, "Telegram is temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedQuery(GetChatBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Telegram response exceeded the configured limit")
	}
	if err != nil {
		return retryQuery(sdkgo.FailureAvailability, "Telegram response was interrupted", 0)
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return failedQuery(GetChatBranchInvalidResponse, sdkgo.FailureProtocol, "Telegram returned invalid JSON")
	}
	if response.StatusCode == http.StatusOK && !envelope.OK && envelope.ErrorCode == 0 {
		return failedQuery(GetChatBranchInvalidResponse, sdkgo.FailureProtocol, "Telegram returned an incomplete response")
	}
	if !envelope.OK {
		status := response.StatusCode
		if envelope.ErrorCode != 0 {
			status = envelope.ErrorCode
		}
		if status == http.StatusTooManyRequests {
			return retryQuery(sdkgo.FailureRateLimit, "Telegram rate limited the request", time.Duration(max(envelope.Parameters.RetryAfter, 0))*time.Second)
		}
		if status >= 500 {
			return retryQuery(sdkgo.FailureAvailability, "Telegram is temporarily unavailable", 0)
		}
		if status == http.StatusBadRequest && strings.Contains(strings.ToLower(envelope.Description), "chat not found") {
			return failedQuery(GetChatBranchNotFound, sdkgo.FailureNotFound, "Telegram did not find the chat")
		}
		if status == http.StatusUnauthorized {
			return failedQuery(GetChatBranchProviderRejected, sdkgo.FailureAuthentication, "Telegram rejected the bot token")
		}
		if status == http.StatusForbidden {
			return failedQuery(GetChatBranchProviderRejected, sdkgo.FailureAuthorization, "Telegram denied access to the chat")
		}
		return failedQuery(GetChatBranchProviderRejected, sdkgo.FailureProviderRejection, "Telegram rejected HTTP "+strconv.Itoa(status))
	}
	if response.StatusCode != http.StatusOK || len(envelope.Result) == 0 {
		return failedQuery(GetChatBranchInvalidResponse, sdkgo.FailureProtocol, "Telegram returned an inconsistent response")
	}
	var chat Chat
	if err := json.Unmarshal(envelope.Result, &chat); err != nil || chat.ID == 0 || !validChatType(chat.Type) {
		return failedQuery(GetChatBranchInvalidResponse, sdkgo.FailureProtocol, "Telegram returned unusable chat data")
	}
	return sdkgo.NewQueryBranch(GetChatBranchFound, chat, nil, sdkgo.Receipt{})
}

func validChatID(chatID string) bool {
	if chatUsernamePattern.MatchString(chatID) {
		return true
	}
	if chatID == "" || strings.Trim(chatID, "-0123456789") != "" {
		return false
	}
	numericID, err := strconv.ParseInt(chatID, 10, 64)
	return err == nil && numericID != 0
}

func validChatType(chatType string) bool {
	switch chatType {
	case "private", "group", "supergroup", "channel":
		return true
	}
	return false
}

func failedQuery(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Chat] {
	failure := sdkgo.Failure{Kind: kind, Provider: "telegram", Operation: "getChat", Message: message}
	return sdkgo.NewQueryBranch(branch, Chat{}, &failure, sdkgo.Receipt{})
}

func retryQuery(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[Chat] {
	failure := sdkgo.Failure{Kind: kind, Provider: "telegram", Operation: "getChat", Message: message}
	return sdkgo.NewQueryRetry[Chat](failure, retryAfter)
}
