// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

// openAIAPIBaseURL is the public OpenAI API; the wire format appends /responses.
const openAIAPIBaseURL = "https://api.openai.com/v1"

const (
	responseStatusCompleted  = "completed"
	responseStatusIncomplete = "incomplete"
)

// openAIRateLimitHeaders are the OpenAI rate-limit headers copied into Receipt metadata.
var openAIRateLimitHeaders = []string{
	"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests",
	"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens",
}

// responsesErrorTokenPointers read an OpenAI error envelope and the error of a failed Response object.
var responsesErrorTokenPointers = []string{"/error/type", "/error/code"}

// newOpenAIResponsesWireFormat returns the Responses API dialect of the shared
// generateText pipeline, as documented at
// https://developers.openai.com/api/reference/resources/responses/methods/create.
func newOpenAIResponsesWireFormat(*Config) (textgen.WireFormat, error) {
	return textgen.WireFormat{
		ProviderName: string(ProviderOpenai),
		ModelIDRule:  textgen.ModelIDRuleBody,
		Features: textgen.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true,
			SupportsTemperature: true, SupportsReasoningEffort: true,
		},
		CredentialHeader: textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "},
		RulesForModel:    rulesForResponsesModel,
		EncodeRequest:    encodeResponsesRequest,
		DecodeResponse:   decodeResponsesBody,
		DecodeStream:     decodeResponsesStream,
		FinishReasons: map[string]textgen.FinishReason{
			responseStatusCompleted: textgen.FinishReasonStop,
			"max_output_tokens":     textgen.FinishReasonLength,
			"content_filter":        textgen.FinishReasonContentPolicy,
		},
		ErrorRules: []textgen.ErrorRule{
			// Billing errors are 429s that a retry cannot fix; error.type may stay insufficient_quota.
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "insufficient_quota", Outcome: textgen.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "credit_balance_exhausted", Outcome: textgen.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "organization_spend_limit_exceeded", Outcome: textgen.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "project_spend_limit_exceeded", Outcome: textgen.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "organization_usage_limit_exceeded", Outcome: textgen.QuotaExhaustedOutcome()},
			// A safety block arrives as a 403 before streaming or as an error during it.
			{ErrorToken: "misalignment_policy_violation", Outcome: textgen.BlockedOutcome()},
			{ErrorToken: "bio_policy", Outcome: textgen.BlockedOutcome()},
			// The cybersecurity check of GPT-5.3-Codex and newer; OpenAI documents no HTTP status for it.
			{ErrorToken: "cyber_policy", Outcome: textgen.BlockedOutcome()},
			// Keeps the shared 501 default, because the server_error rule below also matches every status.
			{StatusCode: http.StatusNotImplemented, Outcome: textgen.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
			// A failed Response carries these retryable error codes.
			{ErrorToken: "server_error", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
			{ErrorToken: "rate_limit_exceeded", Outcome: textgen.RetryOutcome(sdkgo.FailureRateLimit)},
		},
		ErrorTokenPointers: responsesErrorTokenPointers,
		RequestIDHeaders:   []string{"x-request-id"},
		RateLimitHeaders:   openAIRateLimitHeaders,
	}, nil
}

func rulesForResponsesModel(model string) textgen.ModelRequestRules {
	return responsesModelFamilyFor(model).requestRules()
}

// responsesRequestBody is the documented Create a model response body for one generateText request.
type responsesRequestBody struct {
	Model           string                    `json:"model"`
	Instructions    string                    `json:"instructions,omitempty"`
	Input           []responsesInputMessage   `json:"input"`
	MaxOutputTokens int                       `json:"max_output_tokens,omitempty"`
	Temperature     *float64                  `json:"temperature,omitempty"`
	Reasoning       *responsesReasoningConfig `json:"reasoning,omitempty"`
	Text            *responsesTextConfig      `json:"text,omitempty"`
	// Store is always sent as false, because OpenAI stores a Response by default and generateText is a Query.
	Store  bool `json:"store"`
	Stream bool `json:"stream,omitempty"`
}

type responsesInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesReasoningConfig struct {
	Effort string `json:"effort"`
}

type responsesTextConfig struct {
	Format responsesJSONSchemaFormat `json:"format"`
}

type responsesJSONSchemaFormat struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
	Strict      bool           `json:"strict"`
}

// responsesWireResponse is the part of a Response object that generateText reads.
type responsesWireResponse struct {
	ID                string          `json:"id"`
	Model             string          `json:"model"`
	Status            string          `json:"status"`
	Error             json.RawMessage `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Refusal string `json:"refusal"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
		InputDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

// responsesStreamEvent is one semantic Responses streaming event.
type responsesStreamEvent struct {
	Type     string                `json:"type"`
	Delta    string                `json:"delta,omitempty"`
	Response responsesWireResponse `json:"response"`
}

func encodeResponsesRequest(input textgen.EncodeRequestInput) (textgen.EncodedRequest, error) {
	request := input.Request
	family := responsesModelFamilyFor(request.Model)
	if request.Temperature != nil {
		if err := family.validateTemperatureWithReasoningEffort(request.ReasoningEffort); err != nil {
			return textgen.EncodedRequest{}, err
		}
	}
	body := responsesRequestBody{
		Model: request.Model, Instructions: request.Instructions, Input: make([]responsesInputMessage, 0, len(request.Messages)),
		MaxOutputTokens: request.MaxOutputTokens, Temperature: request.Temperature, Stream: !family.isStreamingUnsupported,
	}
	for _, message := range request.Messages {
		body.Input = append(body.Input, responsesInputMessage{Role: string(message.Role), Content: message.Text})
	}
	if request.ReasoningEffort != "" {
		body.Reasoning = &responsesReasoningConfig{Effort: input.Rules.ReasoningEfforts[request.ReasoningEffort]}
	}
	if output := request.StructuredOutput; output != nil {
		body.Text = &responsesTextConfig{Format: responsesJSONSchemaFormat{
			Type: "json_schema", Name: output.Name, Description: output.Description, Schema: output.Schema, Strict: true,
		}}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return textgen.EncodedRequest{}, fmt.Errorf("the request body is not JSON serializable")
	}
	return textgen.EncodedRequest{Path: "/responses", Body: encoded, IsStreaming: body.Stream}, nil
}

// decodeResponsesBody decodes a complete Response object, or an error object a gateway returned with 2xx.
func decodeResponsesBody(body []byte) (textgen.DecodedResponse, error) {
	var response responsesWireResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return textgen.DecodedResponse{}, err
	}
	if hasResponseErrorObject(response.Error) {
		return textgen.DecodedResponse{}, &textgen.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, responsesErrorTokenPointers)}
	}
	return decodeResponseOutput(response, ""), nil
}

// decodeResponsesStream reads semantic Responses events until response.completed,
// response.incomplete, response.failed, or error.
func decodeResponsesStream(events *providerhttp.ServerSentEventReader, writeTextDelta func(delta string) error) (textgen.DecodedResponse, error) {
	var streamedText strings.Builder
	for {
		event, err := events.ReadEvent()
		if errors.Is(err, io.EOF) {
			return textgen.DecodedResponse{}, io.ErrUnexpectedEOF
		}
		if err != nil {
			return textgen.DecodedResponse{}, fmt.Errorf("read Responses event stream: %w", err)
		}
		var streamed responsesStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &streamed); err != nil {
			return textgen.DecodedResponse{}, err
		}
		switch streamed.Type {
		case "response.output_text.delta":
			streamedText.WriteString(streamed.Delta)
			if err := writeTextDelta(streamed.Delta); err != nil {
				return textgen.DecodedResponse{}, err
			}
		case "response.completed", "response.incomplete":
			return decodeResponseOutput(streamed.Response, streamedText.String()), nil
		case "response.failed":
			return textgen.DecodedResponse{}, &textgen.ProviderReportedError{
				ErrorTokens: providerhttp.ReadErrorTokens([]byte(event.Data), []string{"/response/error/code"}),
			}
		case "error":
			return textgen.DecodedResponse{}, &textgen.ProviderReportedError{
				ErrorTokens: providerhttp.ReadErrorTokens([]byte(event.Data), []string{"/code", "/error/type", "/error/code"}),
			}
		}
	}
}

// decodeResponseOutput joins output_text parts; streamedText stands in when a terminal event omits the message output.
func decodeResponseOutput(response responsesWireResponse, streamedText string) textgen.DecodedResponse {
	decoded := textgen.DecodedResponse{
		ServedModel: response.Model, ResponseID: response.ID, ProviderFinishReason: responseFinishToken(response),
		Usage: textgen.Usage{
			InputTokens: int64(response.Usage.InputTokens), CachedInputTokens: int64(response.Usage.InputDetails.CachedTokens),
			OutputTokens: int64(response.Usage.OutputTokens), ReasoningTokens: int64(response.Usage.OutputDetails.ReasoningTokens),
			TotalTokens: int64(response.Usage.TotalTokens),
		},
	}
	var text strings.Builder
	hasMessageContent := false
	for _, output := range response.Output {
		if output.Type == "reasoning" {
			continue
		}
		for _, content := range output.Content {
			switch content.Type {
			case "output_text":
				hasMessageContent = true
				text.WriteString(content.Text)
			case "refusal":
				hasMessageContent = true
				decoded.IsRefusal = true
			}
		}
	}
	if !hasMessageContent {
		text.WriteString(streamedText)
	}
	if text.Len() > 0 {
		decoded.Parts = []textgen.ResponsePart{{Text: text.String()}}
	}
	return decoded
}

// responseFinishToken is "completed", the incomplete_details reason, or the unrecognized status.
func responseFinishToken(response responsesWireResponse) string {
	switch response.Status {
	case responseStatusCompleted:
		return responseStatusCompleted
	case responseStatusIncomplete:
		if response.IncompleteDetails != nil && response.IncompleteDetails.Reason != "" {
			return response.IncompleteDetails.Reason
		}
		return responseStatusIncomplete
	default:
		return response.Status
	}
}

func hasResponseErrorObject(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && string(trimmed) != "null"
}
