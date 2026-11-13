// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iflytek

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CreateChatCompletionInput contains one bounded user prompt.
type CreateChatCompletionInput struct {
	// Prompt is nonempty UTF-8 text of at most 2000 characters.
	Prompt string `json:"prompt"`
}

// ChatCompletion contains one assistant answer and token usage.
type ChatCompletion struct {
	// Content is the assistant's nonempty text answer.
	Content string `json:"content"`
	// Model is the model ID selected by the connection.
	Model string `json:"model"`
	// PromptTokens is the number of billed input tokens reported by Spark.
	PromptTokens int `json:"promptTokens"`
	// CompletionTokens is the number of billed output tokens reported by Spark.
	CompletionTokens int `json:"completionTokens"`
	// TotalTokens is the total token count reported by Spark.
	TotalTokens int `json:"totalTokens"`
}

// CreateChatCompletionOperation generates one text answer with Spark.
type CreateChatCompletionOperation struct{ client *Client }

// CreateChatCompletion binds the Query to this client.
func (client *Client) CreateChatCompletion() CreateChatCompletionOperation {
	return CreateChatCompletionOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (CreateChatCompletionOperation) Definition() sdkgo.QueryDefinition {
	return CreateChatCompletionDefinition
}

// Invoke sends one nonstreaming request and classifies the provider outcome.
func (operation CreateChatCompletionOperation) Invoke(call sdkgo.Call, input CreateChatCompletionInput) sdkgo.QueryAttempt[ChatCompletion] {
	if !utf8.ValidString(input.Prompt) || strings.TrimSpace(input.Prompt) == "" || utf8.RuneCountInString(input.Prompt) > 2000 {
		return completionBranch(CreateChatCompletionBranchDefect, sdkgo.FailureValidation, "Spark prompt must contain 1 to 2000 UTF-8 characters")
	}
	response := operation.client.complete(call, input)
	if errors.Is(response.err, errCredential) {
		return completionBranch(CreateChatCompletionBranchDefect, sdkgo.FailureAuthentication, "Spark APIPassword is unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[ChatCompletion](completionFailure(sdkgo.FailureTransport, "Spark request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[ChatCompletion](completionFailure(sdkgo.FailureRateLimit, "Spark rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[ChatCompletion](completionFailure(sdkgo.FailureAvailability, "Spark is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return completionBranch(CreateChatCompletionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Spark response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[ChatCompletion](completionFailure(sdkgo.FailureTransport, "Spark response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return completionBranch(CreateChatCompletionBranchProviderRejected, sdkgo.FailureProviderRejection, "Spark rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return completionBranch(CreateChatCompletionBranchInvalidResponse, sdkgo.FailureProtocol, "Spark returned invalid UTF-8")
	}
	var wire struct {
		Code    *int `json:"code"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return completionBranch(CreateChatCompletionBranchInvalidResponse, sdkgo.FailureProtocol, "Spark returned invalid JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return completionBranch(CreateChatCompletionBranchInvalidResponse, sdkgo.FailureProtocol, "Spark returned trailing JSON")
	}
	if wire.Code == nil {
		return completionBranch(CreateChatCompletionBranchInvalidResponse, sdkgo.FailureProtocol, "Spark omitted code")
	}
	if *wire.Code != 0 {
		switch *wire.Code {
		case 10007, 11202, 11203:
			return sdkgo.NewQueryRetry[ChatCompletion](completionFailure(sdkgo.FailureRateLimit, "Spark rate limited generation ("+strconv.Itoa(*wire.Code)+")"), response.retryAfter)
		default:
			return completionBranch(CreateChatCompletionBranchProviderRejected, sdkgo.FailureProviderRejection, "Spark rejected generation ("+strconv.Itoa(*wire.Code)+")")
		}
	}
	if len(wire.Choices) != 1 || wire.Choices[0].Message.Role != "assistant" || wire.Choices[0].Message.Content == "" || len(wire.Choices[0].Message.Content) > 65536 {
		return completionBranch(CreateChatCompletionBranchInvalidResponse, sdkgo.FailureProtocol, "Spark returned invalid assistant content")
	}
	if wire.Usage == nil || wire.Usage.PromptTokens < 0 || wire.Usage.CompletionTokens < 0 || wire.Usage.TotalTokens < 0 {
		return completionBranch(CreateChatCompletionBranchInvalidResponse, sdkgo.FailureProtocol, "Spark returned invalid token usage")
	}
	return sdkgo.NewQueryBranch(CreateChatCompletionBranchCompleted, ChatCompletion{Content: wire.Choices[0].Message.Content, Model: operation.client.model, PromptTokens: wire.Usage.PromptTokens, CompletionTokens: wire.Usage.CompletionTokens, TotalTokens: wire.Usage.TotalTokens}, nil, sdkgo.Receipt{})
}
func completionFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "iflytek", Operation: "createChatCompletion", Message: message}
}
func completionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ChatCompletion] {
	failure := completionFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, ChatCompletion{}, &failure, sdkgo.Receipt{})
}
