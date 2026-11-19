// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetChatMemberInput identifies a bot-visible chat member.
type GetChatMemberInput struct {
	// ChatID is the numeric chat ID or public @username.
	ChatID string `json:"chatId"`
	// UserID is the positive numeric Telegram user ID.
	UserID int64 `json:"userId"`
}

// ChatMemberSummary excludes a member's name, username, and profile data.
type ChatMemberSummary struct {
	// UserID is the Telegram user ID.
	UserID int64 `json:"userId"`
	// Status is creator, administrator, member, restricted, left, or kicked.
	Status string `json:"status"`
}

// ChatAdministrators contains only administrator IDs and statuses.
type ChatAdministrators struct {
	// Administrators are the chat's administrators.
	Administrators []ChatMemberSummary `json:"administrators"`
}

// SetChatDescriptionInput replaces a chat description.
type SetChatDescriptionInput struct {
	// ChatID is the numeric chat ID or public @username.
	ChatID string `json:"chatId"`
	// Description is a Telegram description of at most 255 UTF-8 characters; blank clears it.
	Description string `json:"description"`
}

// SetChatTitleInput replaces a chat title.
type SetChatTitleInput struct {
	// ChatID is the numeric chat ID or public @username.
	ChatID string `json:"chatId"`
	// Title is 1 through 128 UTF-8 characters.
	Title string `json:"title"`
}

// ChatActionResult identifies the chat whose action Telegram confirmed.
type ChatActionResult struct {
	// ChatID is the requested chat ID or @username.
	ChatID string `json:"chatId"`
}

// GetChatAdministratorsOperation reads administrator identities.
type GetChatAdministratorsOperation struct{ client *Client }

// GetChatAdministrators binds the query to this client.
func (client *Client) GetChatAdministrators() GetChatAdministratorsOperation {
	return GetChatAdministratorsOperation{client}
}

// Definition returns the generated query contract.
func (GetChatAdministratorsOperation) Definition() sdkgo.QueryDefinition {
	return GetChatAdministratorsDefinition
}

// Invoke reads administrator IDs and statuses without profile fields.
func (operation GetChatAdministratorsOperation) Invoke(call sdkgo.Call, input GetChatInput) sdkgo.QueryAttempt[ChatAdministrators] {
	branches := chatQueryBranches{found: GetChatAdministratorsBranchFound, rejected: GetChatAdministratorsBranchProviderRejected, invalid: GetChatAdministratorsBranchInvalidResponse, defect: GetChatAdministratorsBranchDefect}
	if !validChatID(input.ChatID) {
		return chatQueryBranch(branches.defect, ChatAdministrators{}, "getChatAdministrators", sdkgo.FailureValidation, "chat ID invalid")
	}
	return invokeChatQuery(call, operation.client, "getChatAdministrators", url.Values{"chat_id": {input.ChatID}}, branches, decodeChatAdministrators)
}

// GetChatMemberOperation reads one member's identity and status.
type GetChatMemberOperation struct{ client *Client }

// GetChatMember binds the query to this client.
func (client *Client) GetChatMember() GetChatMemberOperation { return GetChatMemberOperation{client} }

// Definition returns the generated query contract.
func (GetChatMemberOperation) Definition() sdkgo.QueryDefinition { return GetChatMemberDefinition }

// Invoke reads a member without profile information.
func (operation GetChatMemberOperation) Invoke(call sdkgo.Call, input GetChatMemberInput) sdkgo.QueryAttempt[ChatMemberSummary] {
	branches := chatQueryBranches{found: GetChatMemberBranchFound, notFound: GetChatMemberBranchNotFound, rejected: GetChatMemberBranchProviderRejected, invalid: GetChatMemberBranchInvalidResponse, defect: GetChatMemberBranchDefect}
	if !validChatID(input.ChatID) || input.UserID <= 0 {
		return chatQueryBranch(branches.defect, ChatMemberSummary{}, "getChatMember", sdkgo.FailureValidation, "chat or user ID invalid")
	}
	return invokeChatQuery(call, operation.client, "getChatMember", url.Values{"chat_id": {input.ChatID}, "user_id": {strconv.FormatInt(input.UserID, 10)}}, branches, decodeChatMember)
}

// LeaveChatOperation makes the bot leave a group, supergroup, or channel.
type LeaveChatOperation struct{ client *Client }

// LeaveChat binds the mutation to this client.
func (client *Client) LeaveChat() LeaveChatOperation { return LeaveChatOperation{client} }

// Definition returns the generated mutation contract.
func (LeaveChatOperation) Definition() sdkgo.MutationDefinition { return LeaveChatDefinition }

// IdempotencyKey derives stable Dex identity for a repeatable leave action.
func (LeaveChatOperation) IdempotencyKey(callID sdkgo.CallID, _ GetChatInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke leaves the chat and confirms Telegram's boolean response.
func (operation LeaveChatOperation) Invoke(call sdkgo.Call, input GetChatInput) sdkgo.MutationAttempt[ChatActionResult] {
	if !validChatID(input.ChatID) {
		return chatMutationBranch(LeaveChatBranchDefect, ChatActionResult{}, "leaveChat", sdkgo.FailureValidation, "chat ID invalid")
	}
	return invokeChatAction(call, operation.client, "leaveChat", url.Values{"chat_id": {input.ChatID}}, input.ChatID, LeaveChatBranchLeft, LeaveChatBranchProviderRejected, LeaveChatBranchInvalidResponse, LeaveChatBranchDefect)
}

// SetChatDescriptionOperation replaces a chat description.
type SetChatDescriptionOperation struct{ client *Client }

// SetChatDescription binds the mutation to this client.
func (client *Client) SetChatDescription() SetChatDescriptionOperation {
	return SetChatDescriptionOperation{client}
}

// Definition returns the generated mutation contract.
func (SetChatDescriptionOperation) Definition() sdkgo.MutationDefinition {
	return SetChatDescriptionDefinition
}

// IdempotencyKey derives stable Dex identity for absolute description replacement.
func (SetChatDescriptionOperation) IdempotencyKey(callID sdkgo.CallID, _ SetChatDescriptionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a description or clears it with a blank value.
func (operation SetChatDescriptionOperation) Invoke(call sdkgo.Call, input SetChatDescriptionInput) sdkgo.MutationAttempt[ChatActionResult] {
	if !validChatID(input.ChatID) || !utf8.ValidString(input.Description) || utf8.RuneCountInString(input.Description) > 255 {
		return chatMutationBranch(SetChatDescriptionBranchDefect, ChatActionResult{}, "setChatDescription", sdkgo.FailureValidation, "chat ID or description invalid")
	}
	return invokeChatAction(call, operation.client, "setChatDescription", url.Values{"chat_id": {input.ChatID}, "description": {input.Description}}, input.ChatID, SetChatDescriptionBranchUpdated, SetChatDescriptionBranchProviderRejected, SetChatDescriptionBranchInvalidResponse, SetChatDescriptionBranchDefect)
}

// SetChatTitleOperation replaces a chat title.
type SetChatTitleOperation struct{ client *Client }

// SetChatTitle binds the mutation to this client.
func (client *Client) SetChatTitle() SetChatTitleOperation { return SetChatTitleOperation{client} }

// Definition returns the generated mutation contract.
func (SetChatTitleOperation) Definition() sdkgo.MutationDefinition { return SetChatTitleDefinition }

// IdempotencyKey derives stable Dex identity for absolute title replacement.
func (SetChatTitleOperation) IdempotencyKey(callID sdkgo.CallID, _ SetChatTitleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces a title and confirms Telegram's boolean response.
func (operation SetChatTitleOperation) Invoke(call sdkgo.Call, input SetChatTitleInput) sdkgo.MutationAttempt[ChatActionResult] {
	if !validChatID(input.ChatID) || !utf8.ValidString(input.Title) || strings.TrimSpace(input.Title) == "" || utf8.RuneCountInString(input.Title) > 128 {
		return chatMutationBranch(SetChatTitleBranchDefect, ChatActionResult{}, "setChatTitle", sdkgo.FailureValidation, "chat ID or title invalid")
	}
	return invokeChatAction(call, operation.client, "setChatTitle", url.Values{"chat_id": {input.ChatID}, "title": {input.Title}}, input.ChatID, SetChatTitleBranchUpdated, SetChatTitleBranchProviderRejected, SetChatTitleBranchInvalidResponse, SetChatTitleBranchDefect)
}

type chatQueryBranches struct {
	found    sdkgo.BranchID
	notFound sdkgo.BranchID
	rejected sdkgo.BranchID
	invalid  sdkgo.BranchID
	defect   sdkgo.BranchID
}

type botAPIResponse struct {
	status     int
	body       []byte
	retryAfter time.Duration
	err        error
}

var errBotCredential = errors.New("bot credentials unavailable")
var errBotRequest = errors.New("bot request failed")

func (client *Client) callBot(call sdkgo.Call, method, operation string, parameters url.Values) botAPIResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return botAPIResponse{err: errBotCredential}
	}
	token := credentials.BotToken.Reveal()
	if !botTokenPattern.MatchString(token) {
		return botAPIResponse{err: errBotCredential}
	}
	endpoint := client.baseURL + "/bot" + token + "/" + operation
	var request *http.Request
	if method == http.MethodGet {
		request, err = http.NewRequestWithContext(call.Context, method, endpoint+"?"+parameters.Encode(), nil)
	} else {
		request, err = http.NewRequestWithContext(call.Context, method, endpoint, strings.NewReader(parameters.Encode()))
	}
	if err != nil {
		return botAPIResponse{err: errBotRequest}
	}
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return botAPIResponse{err: errBotRequest}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return botAPIResponse{status: response.StatusCode, retryAfter: retryAfterFromResponse(response, client.maxResponseBytes)}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return botAPIResponse{status: response.StatusCode, body: body, err: err}
}

func invokeChatQuery[Output any](call sdkgo.Call, client *Client, operation string, parameters url.Values, branches chatQueryBranches, decode func(json.RawMessage) (Output, error)) sdkgo.QueryAttempt[Output] {
	response := client.callBot(call, http.MethodGet, operation, parameters)
	if retry, failure := botRetry(operation, response); retry {
		return sdkgo.NewQueryRetry[Output](failure, response.retryAfter)
	}
	var zero Output
	if response.err == errBotCredential {
		return chatQueryBranch(branches.defect, zero, operation, sdkgo.FailureAuthentication, "bot token unavailable")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return chatQueryBranch(branches.invalid, zero, operation, sdkgo.FailureResponseTooLarge, "Bot API response exceeded limit")
	}
	envelope, err := decodeBotEnvelope(response.body)
	if err != nil {
		return chatQueryBranch(branches.invalid, zero, operation, sdkgo.FailureProtocol, "Bot API response invalid")
	}
	if !envelope.OK {
		if branches.notFound != "" && response.status == http.StatusBadRequest && (strings.Contains(strings.ToLower(envelope.Description), "not found") || strings.Contains(strings.ToLower(envelope.Description), "participant") || strings.Contains(strings.ToLower(envelope.Description), "wrong file identifier")) {
			return chatQueryBranch(branches.notFound, zero, operation, sdkgo.FailureNotFound, "member not found")
		}
		return chatQueryBranch(branches.rejected, zero, operation, botRejectionKind(response.status), "Telegram rejected lookup")
	}
	if response.status != http.StatusOK {
		return chatQueryBranch(branches.invalid, zero, operation, sdkgo.FailureProtocol, "Bot API status inconsistent")
	}
	output, err := decode(envelope.Result)
	if err != nil {
		return chatQueryBranch(branches.invalid, zero, operation, sdkgo.FailureProtocol, "Bot API result invalid")
	}
	return sdkgo.NewQueryBranch(branches.found, output, nil, sdkgo.Receipt{})
}

func invokeChatAction(call sdkgo.Call, client *Client, operation string, parameters url.Values, chatID string, success, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[ChatActionResult] {
	response := client.callBot(call, http.MethodPost, operation, parameters)
	if retry, failure := botRetry(operation, response); retry {
		return sdkgo.NewMutationRetry[ChatActionResult](failure, response.retryAfter)
	}
	if response.err == errBotCredential {
		return chatMutationBranch(defect, ChatActionResult{}, operation, sdkgo.FailureAuthentication, "bot token unavailable")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return chatMutationBranch(invalid, ChatActionResult{}, operation, sdkgo.FailureResponseTooLarge, "Bot API response exceeded limit")
	}
	envelope, err := decodeBotEnvelope(response.body)
	if err != nil {
		return chatMutationBranch(invalid, ChatActionResult{}, operation, sdkgo.FailureProtocol, "Bot API response invalid")
	}
	if !envelope.OK {
		return chatMutationBranch(rejected, ChatActionResult{}, operation, botRejectionKind(response.status), "Telegram rejected chat action")
	}
	var confirmed bool
	if response.status != http.StatusOK || json.Unmarshal(envelope.Result, &confirmed) != nil || !confirmed {
		return chatMutationBranch(invalid, ChatActionResult{}, operation, sdkgo.FailureProtocol, "Bot API confirmation invalid")
	}
	return sdkgo.NewMutationBranch(success, ChatActionResult{ChatID: chatID}, nil, sdkgo.Receipt{})
}

type botEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

func decodeBotEnvelope(body []byte) (botEnvelope, error) {
	var envelope botEnvelope
	err := json.Unmarshal(body, &envelope)
	if err != nil || len(envelope.Result) == 0 && envelope.OK {
		return botEnvelope{}, errors.New("incomplete Bot API response")
	}
	return envelope, nil
}

func decodeChatMember(result json.RawMessage) (ChatMemberSummary, error) {
	var wire struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &wire); err != nil {
		return ChatMemberSummary{}, err
	}
	if wire.User.ID <= 0 || !validMemberStatus(wire.Status) {
		return ChatMemberSummary{}, errors.New("member identity missing")
	}
	return ChatMemberSummary{UserID: wire.User.ID, Status: wire.Status}, nil
}

func decodeChatAdministrators(result json.RawMessage) (ChatAdministrators, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(result, &entries); err != nil || entries == nil {
		return ChatAdministrators{}, errors.New("administrator list invalid")
	}
	administrators := ChatAdministrators{Administrators: make([]ChatMemberSummary, 0, len(entries))}
	for _, entry := range entries {
		member, err := decodeChatMember(entry)
		if err != nil || member.Status != "creator" && member.Status != "administrator" {
			return ChatAdministrators{}, errors.New("administrator invalid")
		}
		administrators.Administrators = append(administrators.Administrators, member)
	}
	return administrators, nil
}

func validMemberStatus(status string) bool {
	switch status {
	case "creator", "administrator", "member", "restricted", "left", "kicked":
		return true
	}
	return false
}

func botRetry(operation string, response botAPIResponse) (bool, sdkgo.Failure) {
	if response.err == errBotRequest || response.err != nil && response.err != errBotCredential && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return true, botFailure(operation, sdkgo.FailureTransport, "Bot API request failed")
	}
	if response.status == http.StatusTooManyRequests {
		return true, botFailure(operation, sdkgo.FailureRateLimit, "Telegram rate limited the request")
	}
	if response.status == http.StatusServiceUnavailable || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return true, botFailure(operation, sdkgo.FailureAvailability, "Telegram is unavailable")
	}
	return false, sdkgo.Failure{}
}

func botRejectionKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

func botFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "telegram", Operation: operation, Message: message}
}

func chatQueryBranch[Output any](branch sdkgo.BranchID, output Output, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := botFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, output, &failure, sdkgo.Receipt{})
}

func chatMutationBranch[Output any](branch sdkgo.BranchID, output Output, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Output] {
	failure := botFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, output, &failure, sdkgo.Receipt{})
}
