// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	responseStatusCompleted  = "completed"
	responseStatusIncomplete = "incomplete"
)

// responsesErrorTokenPointers read an OpenAI error envelope and the error of a failed Response object.
var responsesErrorTokenPointers = []string{"/error/type", "/error/code"}

// newResponsesWireFormat returns the Responses API dialect of the shared
// generateText pipeline, as documented at
// https://developers.openai.com/api/reference/resources/responses/methods/create.
func newResponsesWireFormat() llm.WireFormat {
	return llm.WireFormat{
		ProviderName: ConnectorID,
		ModelIDRule:  llm.ModelIDRuleBody,
		Features: llm.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true,
			SupportsTemperature: true, SupportsReasoningEffort: true,
		},
		CredentialHeader: llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "},
		RulesForModel:    rulesForResponsesModel,
		EncodeRequest:    encodeResponsesRequest,
		DecodeResponse:   decodeResponsesBody,
		DecodeStream:     decodeResponsesStream,
		FinishReasons: map[string]llm.FinishReason{
			responseStatusCompleted: llm.FinishReasonStop,
			"max_output_tokens":     llm.FinishReasonLength,
			"content_filter":        llm.FinishReasonContentPolicy,
		},
		ErrorRules: []llm.ErrorRule{
			// Billing errors are 429s that a retry cannot fix; error.type may stay insufficient_quota.
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "insufficient_quota", Outcome: llm.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "credit_balance_exhausted", Outcome: llm.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "organization_spend_limit_exceeded", Outcome: llm.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "project_spend_limit_exceeded", Outcome: llm.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "organization_usage_limit_exceeded", Outcome: llm.QuotaExhaustedOutcome()},
			// A safety block arrives as a 403 before streaming or as an error during it.
			{ErrorToken: "misalignment_policy_violation", Outcome: llm.BlockedOutcome()},
			{ErrorToken: "bio_policy", Outcome: llm.BlockedOutcome()},
			// The cybersecurity check of GPT-5.3-Codex and newer; OpenAI documents no HTTP status for it.
			{ErrorToken: "cyber_policy", Outcome: llm.BlockedOutcome()},
			// Keeps the shared 501 default, because the server_error rule below also matches every status.
			{StatusCode: http.StatusNotImplemented, Outcome: llm.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
			// A failed Response carries these retryable error codes.
			{ErrorToken: "server_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
			{ErrorToken: "rate_limit_exceeded", Outcome: llm.RetryOutcome(sdkgo.FailureRateLimit)},
		},
		ErrorTokenPointers: responsesErrorTokenPointers,
		RequestIDHeaders:   []string{"x-request-id"},
		RateLimitHeaders:   rateLimitHeaderNames,
	}
}

func rulesForResponsesModel(model string) llm.ModelRequestRules {
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

func encodeResponsesRequest(input llm.EncodeRequestInput) (llm.EncodedRequest, error) {
	request := input.Request
	family := responsesModelFamilyFor(request.Model)
	if request.Temperature != nil {
		if err := family.validateTemperatureWithReasoningEffort(request.ReasoningEffort); err != nil {
			return llm.EncodedRequest{}, err
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
		return llm.EncodedRequest{}, fmt.Errorf("the request body is not JSON serializable")
	}
	return llm.EncodedRequest{Path: "/responses", Body: encoded, IsStreaming: body.Stream}, nil
}

// decodeResponsesBody decodes a complete Response object, or an error object a gateway returned with 2xx.
func decodeResponsesBody(body []byte) (llm.DecodedResponse, error) {
	var response wireResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return llm.DecodedResponse{}, err
	}
	if hasResponseErrorObject(response.Error) {
		return llm.DecodedResponse{}, &llm.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, responsesErrorTokenPointers)}
	}
	return decodeResponseOutput(response, ""), nil
}

// decodeResponsesStream reads semantic Responses events until response.completed,
// response.incomplete, response.failed, or error.
func decodeResponsesStream(events *providerhttp.ServerSentEventReader, writeTextDelta func(delta string) error) (llm.DecodedResponse, error) {
	var streamedText strings.Builder
	for {
		event, err := events.ReadEvent()
		if errors.Is(err, io.EOF) {
			return llm.DecodedResponse{}, io.ErrUnexpectedEOF
		}
		if err != nil {
			return llm.DecodedResponse{}, fmt.Errorf("read Responses event stream: %w", err)
		}
		var streamed streamEvent
		if err := json.Unmarshal([]byte(event.Data), &streamed); err != nil {
			return llm.DecodedResponse{}, err
		}
		switch streamed.Type {
		case "response.output_text.delta":
			streamedText.WriteString(streamed.Delta)
			if err := writeTextDelta(streamed.Delta); err != nil {
				return llm.DecodedResponse{}, err
			}
		case "response.completed", "response.incomplete":
			return decodeResponseOutput(streamed.Response, streamedText.String()), nil
		case "response.failed":
			return llm.DecodedResponse{}, &llm.ProviderReportedError{
				ErrorTokens: providerhttp.ReadErrorTokens([]byte(event.Data), []string{"/response/error/code"}),
			}
		case "error":
			return llm.DecodedResponse{}, &llm.ProviderReportedError{
				ErrorTokens: providerhttp.ReadErrorTokens([]byte(event.Data), []string{"/code", "/error/type", "/error/code"}),
			}
		}
	}
}

// decodeResponseOutput joins output_text parts; streamedText stands in when a terminal event omits the message output.
func decodeResponseOutput(response wireResponse, streamedText string) llm.DecodedResponse {
	decoded := llm.DecodedResponse{
		ServedModel: response.Model, ResponseID: response.ID, ProviderFinishReason: responseFinishToken(response),
		Usage: llm.Usage{
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
		decoded.Parts = []llm.ResponsePart{{Text: text.String()}}
	}
	return decoded
}

// responseFinishToken is "completed", the incomplete_details reason, or the unrecognized status.
func responseFinishToken(response wireResponse) string {
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
