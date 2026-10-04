// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

const (
	// claudeAPIBaseURL is the only Claude API host; the wire format appends claudeMessagesPath.
	claudeAPIBaseURL   = "https://api.anthropic.com"
	claudeMessagesPath = "/v1/messages"
	// anthropicVersion is the API version header every Claude API request must send.
	anthropicVersion = "2023-06-01"
	// claudeDefaultMaxOutputTokens is the required max_tokens of a request without maxOutputTokens; it counts thinking.
	claudeDefaultMaxOutputTokens = 16000
)

var claudeWorkspaceIDPattern = regexp.MustCompile(`^wrkspc_[A-Za-z0-9]{1,128}$`)

var (
	errUnexpectedMessagesResponseType = errors.New("messages response is not a message")
	errMessagesStreamNotStarted       = errors.New("messages stream sent an event before message_start")
	errInvalidMessageStart            = errors.New("messages stream sent a missing or repeated message_start")
	errUnknownContentBlock            = errors.New("messages stream sent a delta for an unknown content block")
	errInvalidMessagesTokenCount      = errors.New("messages usage holds a negative or overflowing token count")
)

// claudeFinishReasons map Claude stop reasons; tool_use and pause_turn need tools this connector never sends.
var claudeFinishReasons = map[string]textgen.FinishReason{
	"end_turn": textgen.FinishReasonStop, "stop_sequence": textgen.FinishReasonStop,
	"max_tokens": textgen.FinishReasonLength, "model_context_window_exceeded": textgen.FinishReasonLength,
	"refusal": textgen.FinishReasonRefusal,
}

// claudeErrorTokenPointers read the error type and the spend-cap error code of Claude's error envelope.
var claudeErrorTokenPointers = []string{"/error/type", "/error/details/error_code"}

// claudeErrorRules follow https://platform.claude.com/docs/en/api/errors and the spend cap in api/rate-limits.
var claudeErrorRules = []textgen.ErrorRule{
	// The tier spend-cap 429 has no retry-after and keeps failing until the next month.
	{ErrorToken: "enforced_spend_limit_reached", Outcome: textgen.QuotaExhaustedOutcome()},
	{ErrorToken: "billing_error", Outcome: textgen.QuotaExhaustedOutcome()},
	// Keeps the shared 501 default, because the api_error rule below also matches every status.
	{StatusCode: http.StatusNotImplemented, Outcome: textgen.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
	// Transient types; a stream error event carries them after a 200 status.
	{ErrorToken: "overloaded_error", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
	{ErrorToken: "api_error", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
	{ErrorToken: "timeout_error", Outcome: textgen.RetryOutcome(sdkgo.FailureAvailability)},
	{ErrorToken: "rate_limit_error", Outcome: textgen.RetryOutcome(sdkgo.FailureRateLimit)},
}

var claudeRateLimitHeaders = []string{
	"anthropic-ratelimit-requests-limit", "anthropic-ratelimit-requests-remaining",
	"anthropic-ratelimit-tokens-limit", "anthropic-ratelimit-tokens-remaining",
	"anthropic-ratelimit-input-tokens-limit", "anthropic-ratelimit-input-tokens-remaining",
	"anthropic-ratelimit-output-tokens-limit", "anthropic-ratelimit-output-tokens-remaining",
}

// messagesWireFormat holds one connection's fixed headers and max_tokens default.
type messagesWireFormat struct {
	fixedHeaders           http.Header
	defaultMaxOutputTokens int64
}

// newClaudeMessagesWireFormat returns the streaming wire format for https://platform.claude.com/docs/en/api/messages/create.
// Its errors never repeat configuration values.
func newClaudeMessagesWireFormat(config *Config) (textgen.WireFormat, error) {
	format := &messagesWireFormat{fixedHeaders: http.Header{}, defaultMaxOutputTokens: claudeDefaultMaxOutputTokens}
	format.fixedHeaders.Set("anthropic-version", anthropicVersion)
	if workspaceID := strings.TrimSpace(config.AnthropicWorkspaceID); workspaceID != "" {
		if !claudeWorkspaceIDPattern.MatchString(workspaceID) {
			return textgen.WireFormat{}, fmt.Errorf("configuration anthropicWorkspaceId must be a wrkspc_ workspace ID")
		}
		format.fixedHeaders.Set("anthropic-workspace-id", workspaceID)
	}
	return textgen.WireFormat{
		ProviderName: string(ProviderAnthropic),
		ModelIDRule:  textgen.ModelIDRuleBody,
		Features: textgen.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true,
			SupportsTemperature: true, SupportsReasoningEffort: true,
		},
		CredentialHeader:   textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "},
		RulesForModel:      rulesForClaudeModel,
		EncodeRequest:      format.encodeRequest,
		DecodeResponse:     decodeMessagesResponse,
		DecodeStream:       decodeMessagesStream,
		FinishReasons:      claudeFinishReasons,
		ErrorRules:         claudeErrorRules,
		ErrorTokenPointers: claudeErrorTokenPointers,
		RequestIDHeaders:   []string{"request-id"},
		RateLimitHeaders:   claudeRateLimitHeaders,
	}, nil
}

type messagesRequestBody struct {
	Model        string                `json:"model"`
	MaxTokens    int64                 `json:"max_tokens"`
	System       string                `json:"system,omitempty"`
	Messages     []messagesRequestTurn `json:"messages"`
	Temperature  *float64              `json:"temperature,omitempty"`
	OutputConfig *messagesOutputConfig `json:"output_config,omitempty"`
	Stream       bool                  `json:"stream"`
}

type messagesRequestTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesOutputConfig struct {
	Effort string                `json:"effort,omitempty"`
	Format *messagesOutputFormat `json:"format,omitempty"`
}

type messagesOutputFormat struct {
	Type   string         `json:"type"`
	Schema map[string]any `json:"schema"`
}

func (format *messagesWireFormat) encodeRequest(input textgen.EncodeRequestInput) (textgen.EncodedRequest, error) {
	request := input.Request
	body := messagesRequestBody{
		Model: request.Model, MaxTokens: format.defaultMaxOutputTokens, System: request.Instructions,
		Messages: make([]messagesRequestTurn, 0, len(request.Messages)), Temperature: request.Temperature, Stream: true,
	}
	if request.MaxOutputTokens > 0 {
		body.MaxTokens = int64(request.MaxOutputTokens)
	}
	for _, message := range request.Messages {
		body.Messages = append(body.Messages, messagesRequestTurn{Role: string(message.Role), Content: message.Text})
	}
	outputConfig := messagesOutputConfig{Effort: input.Rules.ReasoningEfforts[request.ReasoningEffort]}
	if output := request.StructuredOutput; output != nil {
		outputConfig.Format = &messagesOutputFormat{Type: "json_schema", Schema: schemaWithRootDescription(output.Schema, output.Description)}
	}
	if outputConfig != (messagesOutputConfig{}) {
		body.OutputConfig = &outputConfig
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return textgen.EncodedRequest{}, fmt.Errorf("the request body is not JSON serializable")
	}
	return textgen.EncodedRequest{Path: claudeMessagesPath, Body: encoded, Header: format.fixedHeaders.Clone(), IsStreaming: true}, nil
}

// messagesResponse is a Message object, or the error envelope that shares its "type" field.
type messagesResponse struct {
	Type       string                 `json:"type"`
	ID         string                 `json:"id"`
	Model      string                 `json:"model"`
	Content    []messagesContentBlock `json:"content"`
	StopReason *string                `json:"stop_reason"`
	Usage      *messagesUsage         `json:"usage"`
}

type messagesContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
}

// messagesUsage holds the counts Claude reports; message_delta repeats them cumulatively.
type messagesUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	OutputTokensDetails      *struct {
		ThinkingTokens *int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

// decodeMessagesResponse decodes a complete Message, as a gateway that ignores "stream": true returns.
func decodeMessagesResponse(body []byte) (textgen.DecodedResponse, error) {
	var response messagesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return textgen.DecodedResponse{}, err
	}
	if response.Type == "error" {
		return textgen.DecodedResponse{}, reportedMessagesError(body)
	}
	if response.Type != "message" {
		return textgen.DecodedResponse{}, errUnexpectedMessagesResponseType
	}
	decoded := textgen.DecodedResponse{ServedModel: response.Model, ResponseID: response.ID}
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			appendMessagesResponsePart(&decoded, textgen.ResponsePart{Text: block.Text})
		case "thinking":
			appendMessagesResponsePart(&decoded, textgen.ResponsePart{Text: block.Thinking, IsReasoning: true})
		}
	}
	if response.StopReason != nil {
		decoded.ProviderFinishReason = *response.StopReason
	}
	var usage messagesUsage
	usage.mergeCumulative(response.Usage)
	var err error
	if decoded.Usage, err = usage.textGenerationUsage(); err != nil {
		return textgen.DecodedResponse{}, err
	}
	return decoded, nil
}

// decodeMessagesStream decodes the streaming event flow documented at
// https://platform.claude.com/docs/en/build-with-claude/streaming, which ends at message_stop.
func decodeMessagesStream(events *providerhttp.ServerSentEventReader, writeTextDelta func(string) error) (textgen.DecodedResponse, error) {
	stream := &messagesStreamDecoder{writeTextDelta: writeTextDelta, blockTypes: map[int]string{}}
	for {
		event, err := events.ReadEvent()
		if errors.Is(err, io.EOF) {
			return textgen.DecodedResponse{}, io.ErrUnexpectedEOF
		}
		if err != nil {
			return textgen.DecodedResponse{}, fmt.Errorf("read messages stream: %w", err)
		}
		isStopped, err := stream.applyEvent(event)
		if err != nil {
			return textgen.DecodedResponse{}, err
		}
		if isStopped {
			return stream.complete()
		}
	}
}

// messagesStreamDecoder accumulates one Messages event stream.
type messagesStreamDecoder struct {
	writeTextDelta func(string) error
	decoded        textgen.DecodedResponse
	usage          messagesUsage
	// blockTypes records each content block's type by index, so a delta is applied only to its own kind of block.
	blockTypes map[int]string
	hasStarted bool
}

type messagesStreamEvent struct {
	Type         string                `json:"type"`
	Message      *messagesResponse     `json:"message"`
	Index        *int                  `json:"index"`
	ContentBlock *messagesContentBlock `json:"content_block"`
	Delta        json.RawMessage       `json:"delta"`
	Usage        *messagesUsage        `json:"usage"`
}

type messagesContentBlockDelta struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`
}

type messagesMessageDelta struct {
	StopReason *string `json:"stop_reason"`
}

// applyEvent applies one event and reports whether it was message_stop.
func (stream *messagesStreamDecoder) applyEvent(event providerhttp.ServerSentEvent) (bool, error) {
	var streamEvent messagesStreamEvent
	if err := json.Unmarshal([]byte(event.Data), &streamEvent); err != nil {
		return false, err
	}
	switch {
	case event.Type == "error" || streamEvent.Type == "error":
		return false, reportedMessagesError([]byte(event.Data))
	case streamEvent.Type == "message_start":
		return false, stream.startMessage(streamEvent.Message)
	case streamEvent.Type == "ping":
		return false, nil
	case !isKnownMessagesStreamEvent(streamEvent.Type):
		// Claude may add event types, and its versioning policy asks clients to ignore unknown ones.
		return false, nil
	case !stream.hasStarted:
		return false, errMessagesStreamNotStarted
	}
	switch streamEvent.Type {
	case "content_block_start":
		return false, stream.startContentBlock(streamEvent.Index, streamEvent.ContentBlock)
	case "content_block_delta":
		return false, stream.applyContentBlockDelta(streamEvent.Index, streamEvent.Delta)
	case "message_delta":
		return false, stream.applyMessageDelta(streamEvent.Delta, streamEvent.Usage)
	case "message_stop":
		return true, nil
	}
	return false, nil
}

func (stream *messagesStreamDecoder) startMessage(message *messagesResponse) error {
	if stream.hasStarted || message == nil {
		return errInvalidMessageStart
	}
	stream.hasStarted = true
	stream.decoded.ResponseID, stream.decoded.ServedModel = message.ID, message.Model
	stream.usage.mergeCumulative(message.Usage)
	return nil
}

func (stream *messagesStreamDecoder) startContentBlock(index *int, block *messagesContentBlock) error {
	if index == nil || block == nil {
		return errUnknownContentBlock
	}
	stream.blockTypes[*index] = block.Type
	switch block.Type {
	case "text":
		return stream.appendText(block.Text)
	case "thinking":
		appendMessagesResponsePart(&stream.decoded, textgen.ResponsePart{Text: block.Thinking, IsReasoning: true})
	}
	return nil
}

func (stream *messagesStreamDecoder) applyContentBlockDelta(index *int, rawDelta json.RawMessage) error {
	var delta messagesContentBlockDelta
	if err := json.Unmarshal(rawDelta, &delta); err != nil {
		return err
	}
	blockType, isKnownBlock := "", false
	if index != nil {
		blockType, isKnownBlock = stream.blockTypes[*index]
	}
	switch delta.Type {
	case "text_delta":
		if !isKnownBlock || blockType != "text" {
			return errUnknownContentBlock
		}
		return stream.appendText(delta.Text)
	case "thinking_delta":
		if !isKnownBlock || blockType != "thinking" {
			return errUnknownContentBlock
		}
		appendMessagesResponsePart(&stream.decoded, textgen.ResponsePart{Text: delta.Thinking, IsReasoning: true})
	}
	// Signature, citation, and tool-input deltas carry no text.
	return nil
}

func (stream *messagesStreamDecoder) applyMessageDelta(rawDelta json.RawMessage, usage *messagesUsage) error {
	var delta messagesMessageDelta
	if err := json.Unmarshal(rawDelta, &delta); err != nil {
		return err
	}
	if delta.StopReason != nil && *delta.StopReason != "" {
		stream.decoded.ProviderFinishReason = *delta.StopReason
	}
	stream.usage.mergeCumulative(usage)
	return nil
}

func (stream *messagesStreamDecoder) appendText(text string) error {
	if text == "" {
		return nil
	}
	appendMessagesResponsePart(&stream.decoded, textgen.ResponsePart{Text: text})
	return stream.writeTextDelta(text)
}

func (stream *messagesStreamDecoder) complete() (textgen.DecodedResponse, error) {
	usage, err := stream.usage.textGenerationUsage()
	if err != nil {
		return textgen.DecodedResponse{}, err
	}
	stream.decoded.Usage = usage
	return stream.decoded, nil
}

// mergeCumulative copies every count update reports, because later counts include earlier ones.
func (usage *messagesUsage) mergeCumulative(update *messagesUsage) {
	if update == nil {
		return
	}
	usage.InputTokens = cmp.Or(update.InputTokens, usage.InputTokens)
	usage.CacheCreationInputTokens = cmp.Or(update.CacheCreationInputTokens, usage.CacheCreationInputTokens)
	usage.CacheReadInputTokens = cmp.Or(update.CacheReadInputTokens, usage.CacheReadInputTokens)
	usage.OutputTokens = cmp.Or(update.OutputTokens, usage.OutputTokens)
	if update.OutputTokensDetails != nil && update.OutputTokensDetails.ThinkingTokens != nil {
		usage.OutputTokensDetails = update.OutputTokensDetails
	}
}

// textGenerationUsage counts cache writes and reads as input, as Claude's total input does.
func (usage *messagesUsage) textGenerationUsage() (textgen.Usage, error) {
	cachedInputTokens := messagesTokenCountValue(usage.CacheReadInputTokens)
	inputTokens, isValid := sumMessagesTokenCounts(messagesTokenCountValue(usage.InputTokens), messagesTokenCountValue(usage.CacheCreationInputTokens), cachedInputTokens)
	converted := textgen.Usage{InputTokens: inputTokens, CachedInputTokens: cachedInputTokens, OutputTokens: messagesTokenCountValue(usage.OutputTokens)}
	if usage.OutputTokensDetails != nil {
		converted.ReasoningTokens = messagesTokenCountValue(usage.OutputTokensDetails.ThinkingTokens)
	}
	totalTokens, isTotalValid := sumMessagesTokenCounts(converted.InputTokens, converted.OutputTokens)
	if !isValid || !isTotalValid || converted.ReasoningTokens < 0 {
		return textgen.Usage{}, errInvalidMessagesTokenCount
	}
	converted.TotalTokens = totalTokens
	return converted, nil
}

// reportedMessagesError keeps only the error envelope's bounded tokens, never its message.
func reportedMessagesError(body []byte) *textgen.ProviderReportedError {
	return &textgen.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, claudeErrorTokenPointers)}
}

func isKnownMessagesStreamEvent(eventType string) bool {
	switch eventType {
	case "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop":
		return true
	}
	return false
}

// schemaWithRootDescription carries the application's description, because Claude's format has no description field.
func schemaWithRootDescription(schema map[string]any, description string) map[string]any {
	if existing, _ := schema["description"].(string); description == "" || existing != "" {
		return schema
	}
	described := maps.Clone(schema)
	described["description"] = description
	return described
}

// appendMessagesResponsePart merges consecutive parts of the same kind so a stream yields few parts.
func appendMessagesResponsePart(decoded *textgen.DecodedResponse, part textgen.ResponsePart) {
	if part.Text == "" {
		return
	}
	if count := len(decoded.Parts); count > 0 && decoded.Parts[count-1].IsReasoning == part.IsReasoning {
		decoded.Parts[count-1].Text += part.Text
		return
	}
	decoded.Parts = append(decoded.Parts, part)
}

func messagesTokenCountValue(count *int64) int64 {
	if count == nil {
		return 0
	}
	return *count
}

// sumMessagesTokenCounts reports false for a negative count or an int64 overflow.
func sumMessagesTokenCounts(counts ...int64) (int64, bool) {
	var total int64
	for _, count := range counts {
		if count < 0 || total > math.MaxInt64-count {
			return 0, false
		}
		total += count
	}
	return total, true
}
