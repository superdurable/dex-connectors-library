// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// AnswerCallbackQueryInput acknowledges one Telegram callback.
type AnswerCallbackQueryInput struct {
	// QueryID is the opaque callback query ID from a Telegram update.
	QueryID string `json:"queryId"`
	// Text is an optional notification of at most 200 UTF-8 characters.
	Text string `json:"text,omitempty"`
}

// AnswerInlineQueryInput supplies JSON results for one inline query.
type AnswerInlineQueryInput struct {
	// QueryID is the opaque inline query ID from a Telegram update.
	QueryID string `json:"queryId"`
	// ResultsJSON is a JSON array of at most 50 Bot API InlineQueryResult objects.
	ResultsJSON string `json:"resultsJson"`
}

// AnsweredQuery confirms a callback or inline query response without echoing its content.
type AnsweredQuery struct {
	// Answered is true when Telegram confirmed the answer.
	Answered bool `json:"answered"`
}

// GetFileInput identifies a Telegram file.
type GetFileInput struct {
	// FileID is the opaque file ID returned by Telegram.
	FileID string `json:"fileId"`
}

// TelegramFile contains metadata needed to locate a Telegram file.
type TelegramFile struct {
	// FileID is reusable for Bot API calls.
	FileID string `json:"fileId"`
	// FileUniqueID is stable across bots but cannot be used to download the file.
	FileUniqueID string `json:"fileUniqueId"`
	// FileSize is the optional size in bytes.
	FileSize int64 `json:"fileSize"`
	// FilePath is the provider path, without the bot token or download URL.
	FilePath string `json:"filePath"`
}

// AnswerCallbackQueryOperation acknowledges a callback query.
type AnswerCallbackQueryOperation struct{ client *Client }

// AnswerCallbackQuery binds the mutation to this client.
func (client *Client) AnswerCallbackQuery() AnswerCallbackQueryOperation {
	return AnswerCallbackQueryOperation{client}
}

// Definition returns the generated mutation contract.
func (AnswerCallbackQueryOperation) Definition() sdkgo.MutationDefinition {
	return AnswerCallbackQueryDefinition
}

// IdempotencyKey derives stable Dex identity for a callback acknowledgement.
func (AnswerCallbackQueryOperation) IdempotencyKey(callID sdkgo.CallID, _ AnswerCallbackQueryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke acknowledges a callback and optionally displays text.
func (operation AnswerCallbackQueryOperation) Invoke(call sdkgo.Call, input AnswerCallbackQueryInput) sdkgo.MutationAttempt[AnsweredQuery] {
	if !validTelegramQueryID(input.QueryID) || !utf8.ValidString(input.Text) || utf8.RuneCountInString(input.Text) > 200 {
		return chatMutationBranch(AnswerCallbackQueryBranchDefect, AnsweredQuery{}, "answerCallbackQuery", sdkgo.FailureValidation, "callback ID or text invalid")
	}
	parameters := url.Values{"callback_query_id": {input.QueryID}}
	if input.Text != "" {
		parameters.Set("text", input.Text)
	}
	return invokeBotAnswer(call, operation.client, "answerCallbackQuery", parameters, AnswerCallbackQueryBranchAnswered, AnswerCallbackQueryBranchProviderRejected, AnswerCallbackQueryBranchInvalidResponse, AnswerCallbackQueryBranchDefect)
}

// AnswerInlineQueryOperation answers an inline query with JSON results.
type AnswerInlineQueryOperation struct{ client *Client }

// AnswerInlineQuery binds the mutation to this client.
func (client *Client) AnswerInlineQuery() AnswerInlineQueryOperation {
	return AnswerInlineQueryOperation{client}
}

// Definition returns the generated mutation contract.
func (AnswerInlineQueryOperation) Definition() sdkgo.MutationDefinition {
	return AnswerInlineQueryDefinition
}

// IdempotencyKey derives stable Dex identity for one inline query answer.
func (AnswerInlineQueryOperation) IdempotencyKey(callID sdkgo.CallID, _ AnswerInlineQueryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends up to 50 inline result objects without returning their content.
func (operation AnswerInlineQueryOperation) Invoke(call sdkgo.Call, input AnswerInlineQueryInput) sdkgo.MutationAttempt[AnsweredQuery] {
	var results []json.RawMessage
	if !validTelegramQueryID(input.QueryID) || len(input.ResultsJSON) > 1<<20 || json.Unmarshal([]byte(input.ResultsJSON), &results) != nil || results == nil || len(results) > 50 {
		return chatMutationBranch(AnswerInlineQueryBranchDefect, AnsweredQuery{}, "answerInlineQuery", sdkgo.FailureValidation, "inline query ID or results invalid")
	}
	return invokeBotAnswer(call, operation.client, "answerInlineQuery", url.Values{"inline_query_id": {input.QueryID}, "results": {input.ResultsJSON}}, AnswerInlineQueryBranchAnswered, AnswerInlineQueryBranchProviderRejected, AnswerInlineQueryBranchInvalidResponse, AnswerInlineQueryBranchDefect)
}

// GetFileOperation reads file metadata.
type GetFileOperation struct{ client *Client }

// GetFile binds the query to this client.
func (client *Client) GetFile() GetFileOperation { return GetFileOperation{client} }

// Definition returns the generated query contract.
func (GetFileOperation) Definition() sdkgo.QueryDefinition { return GetFileDefinition }

// Invoke reads file metadata without returning a token-bearing download URL.
func (operation GetFileOperation) Invoke(call sdkgo.Call, input GetFileInput) sdkgo.QueryAttempt[TelegramFile] {
	branches := chatQueryBranches{found: GetFileBranchFound, notFound: GetFileBranchNotFound, rejected: GetFileBranchProviderRejected, invalid: GetFileBranchInvalidResponse, defect: GetFileBranchDefect}
	if strings.TrimSpace(input.FileID) == "" || len(input.FileID) > 512 || !utf8.ValidString(input.FileID) {
		return chatQueryBranch(branches.defect, TelegramFile{}, "getFile", sdkgo.FailureValidation, "file ID invalid")
	}
	return invokeChatQuery(call, operation.client, "getFile", url.Values{"file_id": {input.FileID}}, branches, decodeTelegramFile)
}

func invokeBotAnswer(call sdkgo.Call, client *Client, operation string, parameters url.Values, success, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[AnsweredQuery] {
	response := client.callBot(call, http.MethodPost, operation, parameters)
	if retry, failure := botRetry(operation, response); retry {
		return sdkgo.NewMutationRetry[AnsweredQuery](failure, response.retryAfter)
	}
	if response.err == errBotCredential {
		return chatMutationBranch(defect, AnsweredQuery{}, operation, sdkgo.FailureAuthentication, "bot token unavailable")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return chatMutationBranch(invalid, AnsweredQuery{}, operation, sdkgo.FailureResponseTooLarge, "Bot API response exceeded limit")
	}
	envelope, err := decodeBotEnvelope(response.body)
	if err != nil {
		return chatMutationBranch(invalid, AnsweredQuery{}, operation, sdkgo.FailureProtocol, "Bot API response invalid")
	}
	if !envelope.OK {
		return chatMutationBranch(rejected, AnsweredQuery{}, operation, botRejectionKind(response.status), "Telegram rejected query answer")
	}
	var confirmed bool
	if response.status != http.StatusOK || json.Unmarshal(envelope.Result, &confirmed) != nil || !confirmed {
		return chatMutationBranch(invalid, AnsweredQuery{}, operation, sdkgo.FailureProtocol, "Bot API confirmation invalid")
	}
	return sdkgo.NewMutationBranch(success, AnsweredQuery{Answered: true}, nil, sdkgo.Receipt{})
}

func validTelegramQueryID(queryID string) bool {
	return strings.TrimSpace(queryID) != "" && len(queryID) <= 512 && utf8.ValidString(queryID)
}

func decodeTelegramFile(result json.RawMessage) (TelegramFile, error) {
	var wire struct {
		FileID       string `json:"file_id"`
		FileUniqueID string `json:"file_unique_id"`
		FileSize     int64  `json:"file_size"`
		FilePath     string `json:"file_path"`
	}
	if err := json.Unmarshal(result, &wire); err != nil {
		return TelegramFile{}, err
	}
	if wire.FileID == "" || wire.FileUniqueID == "" || wire.FileSize < 0 || strings.HasPrefix(wire.FilePath, "/") || strings.Contains(wire.FilePath, "..") || strings.Contains(wire.FilePath, "://") || strings.Contains(wire.FilePath, "?") {
		return TelegramFile{}, errors.New("file metadata invalid")
	}
	return TelegramFile{FileID: wire.FileID, FileUniqueID: wire.FileUniqueID, FileSize: wire.FileSize, FilePath: wire.FilePath}, nil
}
