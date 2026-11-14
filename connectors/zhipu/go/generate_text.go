// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zhipu

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const chatCompletionsURL = "https://open.bigmodel.cn/api/paas/v4/chat/completions"

// GenerateTextInput requests one text answer. MaxOutputTokens defaults to 1024 when zero.
type GenerateTextInput struct {
	// Prompt is a nonempty user message of at most 64 KiB.
	Prompt string `json:"prompt"`
	// MaxOutputTokens limits generated tokens to 1 through 4096; zero uses 1024.
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

// TokenUsage reports provider token accounting.
type TokenUsage struct {
	// PromptTokens counts tokens in the request context.
	PromptTokens int `json:"promptTokens"`
	// CompletionTokens counts generated tokens.
	CompletionTokens int `json:"completionTokens"`
	// TotalTokens is the provider-reported sum.
	TotalTokens int `json:"totalTokens"`
}

// GenerateTextOutput holds a bounded text answer and its provider metadata.
type GenerateTextOutput struct {
	// Text is the answer content, possibly partial on the truncated branch.
	Text string `json:"text"`
	// Model is the model reported by Zhipu.
	Model string `json:"model"`
	// ResponseID is the non-secret provider completion ID.
	ResponseID string `json:"responseId"`
	// FinishReason is the provider's stop reason.
	FinishReason string `json:"finishReason"`
	// Usage is the provider's reported token count.
	Usage TokenUsage `json:"usage"`
}

// GenerateTextOperation invokes Zhipu's nonstreaming chat completions endpoint.
type GenerateTextOperation struct{ client *Client }

// Definition returns the generated text generation contract.
func (GenerateTextOperation) Definition() sdkgo.QueryDefinition { return GenerateTextDefinition }

// Invoke makes one bounded provider request and classifies its result for Dex.
func (operation GenerateTextOperation) Invoke(call sdkgo.Call, input GenerateTextInput) sdkgo.QueryAttempt[GenerateTextOutput] {
	if strings.TrimSpace(input.Prompt) == "" || len(input.Prompt) > 64<<10 || input.MaxOutputTokens < 0 || input.MaxOutputTokens > 4096 {
		return generationBranch(GenerateTextBranchDefect, sdkgo.FailureValidation, "prompt or maxOutputTokens is invalid", GenerateTextOutput{})
	}
	maxTokens := input.MaxOutputTokens
	if maxTokens == 0 {
		maxTokens = 1024
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return generationBranch(GenerateTextBranchDefect, sdkgo.FailureAuthentication, "connection API key is unavailable or invalid", GenerateTextOutput{})
	}
	payload := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream         bool `json:"stream"`
		MaxTokens      int  `json:"max_tokens"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
	}{Model: operation.client.model, MaxTokens: maxTokens}
	payload.Messages = []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{{Role: "user", Content: input.Prompt}}
	payload.ResponseFormat.Type = "text"
	body, err := json.Marshal(payload)
	if err != nil {
		return generationBranch(GenerateTextBranchDefect, sdkgo.FailureValidation, "generation request is invalid", GenerateTextOutput{})
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, chatCompletionsURL, bytes.NewReader(body))
	if err != nil {
		return generationBranch(GenerateTextBranchDefect, sdkgo.FailureValidation, "generation request is invalid", GenerateTextOutput{})
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return generationRetry(sdkgo.FailureAvailability, "Zhipu request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return generationRetry(sdkgo.FailureRateLimit, "Zhipu rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return generationRetry(sdkgo.FailureAvailability, "Zhipu is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return generationBranch(GenerateTextBranchProviderRejected, kind, "Zhipu rejected the request with HTTP "+strconv.Itoa(response.StatusCode), GenerateTextOutput{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return generationBranch(GenerateTextBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Zhipu response exceeded the configured limit", GenerateTextOutput{})
	}
	if err != nil {
		return generationRetry(sdkgo.FailureAvailability, "Zhipu response was interrupted", 0)
	}
	output, branch, err := decodeGeneration(responseBody)
	if err != nil {
		return generationBranch(GenerateTextBranchInvalidResponse, sdkgo.FailureProtocol, "Zhipu returned an invalid text response", GenerateTextOutput{})
	}
	if branch == GenerateTextBranchBlocked {
		return generationBranch(branch, sdkgo.FailureProviderRejection, "Zhipu blocked the text response", output)
	}
	return sdkgo.NewQueryBranch(branch, output, nil, sdkgo.Receipt{})
}
func decodeGeneration(body []byte) (GenerateTextOutput, sdkgo.BranchID, error) {
	var wire struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role      string          `json:"role"`
				Content   *string         `json:"content"`
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID == "" || wire.Model == "" || len(wire.Choices) != 1 {
		return GenerateTextOutput{}, "", errors.New("invalid completion envelope")
	}
	choice := wire.Choices[0]
	if choice.Message.Role != "assistant" || len(choice.Message.ToolCalls) > 0 && string(choice.Message.ToolCalls) != "null" || wire.Usage.PromptTokens < 0 || wire.Usage.CompletionTokens < 0 || wire.Usage.TotalTokens < 0 {
		return GenerateTextOutput{}, "", errors.New("unsupported completion")
	}
	output := GenerateTextOutput{Model: wire.Model, ResponseID: wire.ID, FinishReason: choice.FinishReason, Usage: TokenUsage{PromptTokens: wire.Usage.PromptTokens, CompletionTokens: wire.Usage.CompletionTokens, TotalTokens: wire.Usage.TotalTokens}}
	if choice.Message.Content != nil {
		output.Text = *choice.Message.Content
	}
	switch choice.FinishReason {
	case "stop":
		if output.Text == "" {
			return GenerateTextOutput{}, "", errors.New("empty completion")
		}
		return output, GenerateTextBranchGenerated, nil
	case "length":
		return output, GenerateTextBranchTruncated, nil
	case "sensitive", "content_filter":
		return output, GenerateTextBranchBlocked, nil
	default:
		return GenerateTextOutput{}, "", errors.New("unsupported finish reason")
	}
}
func generationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, output GenerateTextOutput) sdkgo.QueryAttempt[GenerateTextOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zhipu", Operation: "generateText", Message: message}
	return sdkgo.NewQueryBranch(branch, output, &failure, sdkgo.Receipt{})
}
func generationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GenerateTextOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zhipu", Operation: "generateText", Message: message}
	return sdkgo.NewQueryRetry[GenerateTextOutput](failure, delay)
}
