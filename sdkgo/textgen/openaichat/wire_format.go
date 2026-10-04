// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openaichat

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

var (
	errNoChoice                 = errors.New("chat completion has no choice")
	errStreamEndedWithoutFinish = errors.New("chat completion stream ended without a finish reason")
)

// chatWireFormat holds a validated Profile's resolved values.
type chatWireFormat struct {
	chatCompletionsPath        string
	fixedHeaders               http.Header
	instructionsRole           InstructionsRole
	baseSettings               modelSettings
	modelRules                 []compiledModelRule
	shouldSendStrictJSONSchema bool
	shouldRequestStreamUsage   bool
	allowedRequestFields       map[string]bool
	errorTokenPointers         []string
}

// NewWireFormat validates profile and returns its Chat Completions wire
// format for textgen.TextGenerationQueryConfig. It returns an error for a nil
// profile, a missing provider name, an invalid path, header, role, token
// field, streaming policy, reasoning map, structured-output rule, request
// field, allowlist that omits a field every request sends, or model rule.
// Error rules, finish tokens, and header names are validated again by
// textgen.NewTextGenerationQuery. The model travels in the JSON body, so the wire
// format uses textgen.ModelIDRuleBody.
//
// The wire format declares exactly the textgen.RequestFeatures fields of SDK
// v0.10: instructions, structured output, max output tokens, temperature, and
// reasoning effort. A Profile limits what models accept through its
// temperature policy, reasoning map, and structured-output rules. A
// RequestFeatures field added in a later release is declared only when a new
// Profile field, whose zero value leaves it off, opts in, so a connector
// released against an older SDK keeps rejecting that request field under
// minimal version selection.
func NewWireFormat(profile *Profile) (textgen.WireFormat, error) {
	if profile == nil {
		return textgen.WireFormat{}, fmt.Errorf("openaichat profile is required")
	}
	modelRules, err := validateProfile(profile)
	if err != nil {
		return textgen.WireFormat{}, fmt.Errorf("openaichat %w", err)
	}
	format := &chatWireFormat{
		chatCompletionsPath: cmp.Or(profile.ChatCompletionsPath, "/chat/completions"),
		fixedHeaders:        http.Header{},
		instructionsRole:    InstructionsRole(cmp.Or(string(profile.InstructionsRole), string(InstructionsRoleSystem))),
		baseSettings: modelSettings{
			rules: textgen.ModelRequestRules{
				Temperature: profile.Temperature, ReasoningEfforts: profile.ReasoningEfforts,
				StructuredOutput: profile.StructuredOutput,
			},
			maxTokensField: MaxTokensField(cmp.Or(string(profile.MaxTokensField), string(MaxTokensFieldMaxTokens))),
			streaming:      StreamingPolicy(cmp.Or(string(profile.Streaming), string(StreamingPolicyNever))),
		},
		modelRules: modelRules, shouldSendStrictJSONSchema: profile.ShouldSendStrictJSONSchema, shouldRequestStreamUsage: profile.ShouldRequestStreamUsage,
	}
	for name, value := range profile.FixedHeaders {
		format.fixedHeaders.Set(name, value)
	}
	if len(profile.AllowedRequestFields) > 0 {
		format.allowedRequestFields = make(map[string]bool, len(profile.AllowedRequestFields))
		for _, field := range profile.AllowedRequestFields {
			format.allowedRequestFields[field] = true
		}
	}
	credentialHeader := profile.CredentialHeader
	if credentialHeader.Name == "" {
		credentialHeader = textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}
	}
	finishReasons := map[string]textgen.FinishReason{
		"stop": textgen.FinishReasonStop, "length": textgen.FinishReasonLength, "content_filter": textgen.FinishReasonContentPolicy,
	}
	for token, reason := range profile.FinishReasons {
		finishReasons[token] = reason
	}
	errorTokenPointers := profile.ErrorTokenPointers
	if errorTokenPointers == nil {
		errorTokenPointers = []string{"/error/type", "/error/code"}
	}
	format.errorTokenPointers = append([]string(nil), errorTokenPointers...)
	requestIDHeaders := profile.RequestIDHeaders
	if requestIDHeaders == nil {
		requestIDHeaders = []string{"x-request-id"}
	}
	return textgen.WireFormat{
		ProviderName: profile.ProviderName,
		ModelIDRule:  textgen.ModelIDRuleBody,
		Features: textgen.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true, SupportsTemperature: true,
			SupportsReasoningEffort: true,
		},
		CredentialHeader:   credentialHeader,
		RulesForModel:      format.rulesForModel,
		EncodeRequest:      format.encodeRequest,
		DecodeResponse:     format.decodeResponse,
		DecodeStream:       format.decodeStream,
		FinishReasons:      finishReasons,
		ErrorRules:         append([]textgen.ErrorRule(nil), profile.ErrorRules...),
		ErrorTokenPointers: append([]string(nil), errorTokenPointers...),
		RequestIDHeaders:   append([]string(nil), requestIDHeaders...),
		RateLimitHeaders:   append([]string(nil), profile.RateLimitHeaders...),
		StallTimeout:       profile.StallTimeout,
	}, nil
}

func (format *chatWireFormat) rulesForModel(model string) textgen.ModelRequestRules {
	return format.settingsForModel(model).rules
}

func (format *chatWireFormat) settingsForModel(model string) modelSettings {
	settings := format.baseSettings
	for _, compiled := range format.modelRules {
		if !compiled.matches(model) {
			continue
		}
		rule := compiled.rule
		if rule.Temperature != nil {
			settings.rules.Temperature = *rule.Temperature
		}
		if rule.ReasoningEfforts != nil {
			settings.rules.ReasoningEfforts = rule.ReasoningEfforts
		}
		if rule.StructuredOutput != nil {
			settings.rules.StructuredOutput = *rule.StructuredOutput
		}
		if rule.MaxTokensField != "" {
			settings.maxTokensField = rule.MaxTokensField
		}
		if rule.Streaming != "" {
			settings.streaming = rule.Streaming
		}
		break
	}
	return settings
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (format *chatWireFormat) encodeRequest(input textgen.EncodeRequestInput) (textgen.EncodedRequest, error) {
	request := input.Request
	settings := format.settingsForModel(request.Model)
	messages := make([]chatMessage, 0, len(request.Messages)+1)
	if request.Instructions != "" {
		messages = append(messages, chatMessage{Role: string(format.instructionsRole), Content: request.Instructions})
	}
	for _, message := range request.Messages {
		messages = append(messages, chatMessage{Role: string(message.Role), Content: message.Text})
	}
	body := map[string]any{"model": request.Model, "messages": messages}
	if request.MaxOutputTokens > 0 {
		body[string(settings.maxTokensField)] = request.MaxOutputTokens
	}
	if request.Temperature != nil {
		body["temperature"] = *request.Temperature
	}
	if request.ReasoningEffort != "" {
		body["reasoning_effort"] = input.Rules.ReasoningEfforts[request.ReasoningEffort]
	}
	if output := request.StructuredOutput; output != nil {
		switch input.Rules.StructuredOutput.Mode {
		case textgen.StructuredOutputModeJSONSchema:
			jsonSchema := map[string]any{"name": output.Name, "schema": output.Schema}
			if output.Description != "" {
				jsonSchema["description"] = output.Description
			}
			if format.shouldSendStrictJSONSchema {
				jsonSchema["strict"] = true
			}
			body["response_format"] = map[string]any{"type": "json_schema", "json_schema": jsonSchema}
		case textgen.StructuredOutputModeJSONObjectWithInstruction:
			body["response_format"] = map[string]any{"type": "json_object"}
		}
	}
	isStreaming := settings.streaming == StreamingPolicyAlways
	if isStreaming {
		body["stream"] = true
		if format.shouldRequestStreamUsage {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
	}
	if format.allowedRequestFields != nil {
		for _, field := range sortedFieldNames(body) {
			if !format.allowedRequestFields[field] {
				return textgen.EncodedRequest{}, fmt.Errorf("the provider does not accept the %q request field", field)
			}
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return textgen.EncodedRequest{}, fmt.Errorf("the request body is not JSON serializable")
	}
	return textgen.EncodedRequest{
		Path: format.chatCompletionsPath, Body: encoded, Header: format.fixedHeaders.Clone(), IsStreaming: isStreaming,
	}, nil
}

type chatCompletion struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Choices []chatChoice    `json:"choices"`
	Usage   *chatUsage      `json:"usage"`
	Error   json.RawMessage `json:"error"`
}

type chatChoice struct {
	Index        int       `json:"index"`
	Message      chatDelta `json:"message"`
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type chatDelta struct {
	Content          json.RawMessage `json:"content"`
	ReasoningContent *string         `json:"reasoning_content"`
	Refusal          *string         `json:"refusal"`
}

type chatContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type chatUsage struct {
	PromptTokens        tokenCount `json:"prompt_tokens"`
	CompletionTokens    tokenCount `json:"completion_tokens"`
	TotalTokens         tokenCount `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens tokenCount `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens tokenCount `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func (format *chatWireFormat) decodeResponse(body []byte) (textgen.DecodedResponse, error) {
	var completion chatCompletion
	if err := json.Unmarshal(body, &completion); err != nil {
		return textgen.DecodedResponse{}, err
	}
	if hasErrorObject(completion.Error) {
		return textgen.DecodedResponse{}, format.reportedError(body)
	}
	choice, found := firstChoice(completion.Choices)
	if !found {
		return textgen.DecodedResponse{}, errNoChoice
	}
	decoded := textgen.DecodedResponse{ServedModel: completion.Model, ResponseID: completion.ID, Usage: completion.Usage.usage()}
	if err := appendDeltaParts(&decoded, choice.Message, nil); err != nil {
		return textgen.DecodedResponse{}, err
	}
	if choice.FinishReason != nil {
		decoded.ProviderFinishReason = *choice.FinishReason
	}
	return decoded, nil
}

func (format *chatWireFormat) decodeStream(events *providerhttp.ServerSentEventReader, writeTextDelta func(string) error) (textgen.DecodedResponse, error) {
	var decoded textgen.DecodedResponse
	hasFinished := false
	for {
		event, err := events.ReadEvent()
		if errors.Is(err, io.EOF) {
			if hasFinished {
				return decoded, nil
			}
			return textgen.DecodedResponse{}, io.ErrUnexpectedEOF
		}
		if err != nil {
			return textgen.DecodedResponse{}, fmt.Errorf("read chat completion stream: %w", err)
		}
		if strings.TrimSpace(event.Data) == "[DONE]" {
			if !hasFinished {
				return textgen.DecodedResponse{}, errStreamEndedWithoutFinish
			}
			return decoded, nil
		}
		var chunk chatCompletion
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return textgen.DecodedResponse{}, err
		}
		if hasErrorObject(chunk.Error) {
			return textgen.DecodedResponse{}, format.reportedError([]byte(event.Data))
		}
		decoded.ResponseID = cmp.Or(decoded.ResponseID, chunk.ID)
		decoded.ServedModel = cmp.Or(decoded.ServedModel, chunk.Model)
		if chunk.Usage != nil {
			decoded.Usage = chunk.Usage.usage()
		}
		choice, found := firstChoice(chunk.Choices)
		if !found {
			continue
		}
		if err := appendDeltaParts(&decoded, choice.Delta, writeTextDelta); err != nil {
			return textgen.DecodedResponse{}, err
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			decoded.ProviderFinishReason = *choice.FinishReason
			hasFinished = true
		}
	}
}

// reportedError keeps only the error object's bounded tokens, never its message.
func (format *chatWireFormat) reportedError(body []byte) *textgen.ProviderReportedError {
	return &textgen.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, format.errorTokenPointers)}
}

func hasErrorObject(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && string(trimmed) != "null"
}

// appendDeltaParts adds one message or delta's text and reasoning, writing text deltas when a writer is given.
func appendDeltaParts(decoded *textgen.DecodedResponse, delta chatDelta, writeTextDelta func(string) error) error {
	if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
		appendPart(decoded, textgen.ResponsePart{Text: *delta.ReasoningContent, IsReasoning: true})
	}
	if delta.Refusal != nil && *delta.Refusal != "" {
		decoded.IsRefusal = true
	}
	texts, err := readContentTexts(delta.Content)
	if err != nil {
		return err
	}
	for _, text := range texts {
		appendPart(decoded, textgen.ResponsePart{Text: text})
		if writeTextDelta != nil {
			if err := writeTextDelta(text); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendPart merges consecutive parts of the same kind so a stream yields few parts.
func appendPart(decoded *textgen.DecodedResponse, part textgen.ResponsePart) {
	if count := len(decoded.Parts); count > 0 && decoded.Parts[count-1].IsReasoning == part.IsReasoning {
		decoded.Parts[count-1].Text += part.Text
		return
	}
	decoded.Parts = append(decoded.Parts, part)
}

// readContentTexts accepts null, a string, or typed parts, returning only "text" parts so thinking is skipped.
func readContentTexts(content json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		if text == "" {
			return nil, nil
		}
		return []string{text}, nil
	}
	var parts []chatContentPart
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return texts, nil
}

func firstChoice(choices []chatChoice) (chatChoice, bool) {
	for _, choice := range choices {
		if choice.Index == 0 {
			return choice, true
		}
	}
	return chatChoice{}, false
}

func (usage *chatUsage) usage() textgen.Usage {
	if usage == nil {
		return textgen.Usage{}
	}
	converted := textgen.Usage{
		InputTokens: int64(usage.PromptTokens), OutputTokens: int64(usage.CompletionTokens),
		TotalTokens: int64(usage.TotalTokens),
	}
	if usage.PromptTokensDetails != nil {
		converted.CachedInputTokens = int64(usage.PromptTokensDetails.CachedTokens)
	}
	if usage.CompletionTokensDetails != nil {
		converted.ReasoningTokens = int64(usage.CompletionTokensDetails.ReasoningTokens)
	}
	return converted
}

// tokenCount decodes an integer token count, also accepting an integral JSON
// number such as 12.0 within the int64 range.
type tokenCount int64

// UnmarshalJSON accepts null, an integer, or an integral number.
func (count *tokenCount) UnmarshalJSON(contents []byte) error {
	trimmed := bytes.TrimSpace(contents)
	if string(trimmed) == "null" {
		*count = 0
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return err
	}
	if integer, err := number.Int64(); err == nil {
		*count = tokenCount(integer)
		return nil
	}
	value, err := number.Float64()
	if err != nil || value != math.Trunc(value) || math.Abs(value) >= math.MaxInt64 {
		return fmt.Errorf("token count must be an integer")
	}
	*count = tokenCount(value)
	return nil
}

func sortedFieldNames(body map[string]any) []string {
	names := make([]string, 0, len(body))
	for name := range body {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
