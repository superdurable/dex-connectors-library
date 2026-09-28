// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claude

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
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// apiBaseURL is the only Claude API host; the wire format appends messagesPath.
	apiBaseURL   = "https://api.anthropic.com"
	messagesPath = "/v1/messages"
	// anthropicVersion is the API version header every Claude API request must send.
	anthropicVersion = "2023-06-01"
)

var workspaceIDPattern = regexp.MustCompile(`^wrkspc_[A-Za-z0-9]{1,128}$`)

var (
	errUnexpectedResponseType = errors.New("messages response is not a message")
	errStreamNotStarted       = errors.New("messages stream sent an event before message_start")
	errInvalidMessageStart    = errors.New("messages stream sent a missing or repeated message_start")
	errUnknownContentBlock    = errors.New("messages stream sent a delta for an unknown content block")
	errInvalidTokenCount      = errors.New("messages usage holds a negative or overflowing token count")
)

// finishReasons map Claude stop reasons; tool_use and pause_turn need tools this connector never sends.
var finishReasons = map[string]llm.FinishReason{
	"end_turn": llm.FinishReasonStop, "stop_sequence": llm.FinishReasonStop,
	"max_tokens": llm.FinishReasonLength, "model_context_window_exceeded": llm.FinishReasonLength,
	"refusal": llm.FinishReasonRefusal,
}

// errorTokenPointers read the error type and the spend-cap error code of Claude's error envelope.
var errorTokenPointers = []string{"/error/type", "/error/details/error_code"}

// errorRules follow https://platform.claude.com/docs/en/api/errors and the spend cap in api/rate-limits.
var errorRules = []llm.ErrorRule{
	// The tier spend-cap 429 has no retry-after and keeps failing until the next month.
	{ErrorToken: "enforced_spend_limit_reached", Outcome: llm.QuotaExhaustedOutcome()},
	{ErrorToken: "billing_error", Outcome: llm.QuotaExhaustedOutcome()},
	// Keeps the shared 501 default, because the api_error rule below also matches every status.
	{StatusCode: http.StatusNotImplemented, Outcome: llm.ProviderRejectedOutcome(sdkgo.FailureProviderRejection)},
	// Transient types; a stream error event carries them after a 200 status.
	{ErrorToken: "overloaded_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
	{ErrorToken: "api_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
	{ErrorToken: "timeout_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
	{ErrorToken: "rate_limit_error", Outcome: llm.RetryOutcome(sdkgo.FailureRateLimit)},
}

var rateLimitHeaders = []string{
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

// newMessagesWireFormat returns the streaming wire format for https://platform.claude.com/docs/en/api/messages/create.
// Its errors never repeat configuration values.
func newMessagesWireFormat(config *Config) (llm.WireFormat, error) {
	if config.DefaultMaxOutputTokens < 1 {
		return llm.WireFormat{}, fmt.Errorf("configuration defaultMaxOutputTokens must be positive")
	}
	format := &messagesWireFormat{fixedHeaders: http.Header{}, defaultMaxOutputTokens: config.DefaultMaxOutputTokens}
	format.fixedHeaders.Set("anthropic-version", anthropicVersion)
	if workspaceID := strings.TrimSpace(config.WorkspaceID); workspaceID != "" {
		if !workspaceIDPattern.MatchString(workspaceID) {
			return llm.WireFormat{}, fmt.Errorf("configuration workspaceId must be a wrkspc_ workspace ID")
		}
		format.fixedHeaders.Set("anthropic-workspace-id", workspaceID)
	}
	return llm.WireFormat{
		ProviderName: ConnectorID,
		ModelIDRule:  llm.ModelIDRuleBody,
		Features: llm.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true,
			SupportsTemperature: true, SupportsReasoningEffort: true,
		},
		CredentialHeader:   llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "},
		RulesForModel:      rulesForModel,
		EncodeRequest:      format.encodeRequest,
		DecodeResponse:     decodeMessagesResponse,
		DecodeStream:       decodeMessagesStream,
		FinishReasons:      finishReasons,
		ErrorRules:         errorRules,
		ErrorTokenPointers: errorTokenPointers,
		RequestIDHeaders:   []string{"request-id"},
		RateLimitHeaders:   rateLimitHeaders,
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

func (format *messagesWireFormat) encodeRequest(input llm.EncodeRequestInput) (llm.EncodedRequest, error) {
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
		return llm.EncodedRequest{}, fmt.Errorf("the request body is not JSON serializable")
	}
	return llm.EncodedRequest{Path: messagesPath, Body: encoded, Header: format.fixedHeaders.Clone(), IsStreaming: true}, nil
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
func decodeMessagesResponse(body []byte) (llm.DecodedResponse, error) {
	var response messagesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return llm.DecodedResponse{}, err
	}
	if response.Type == "error" {
		return llm.DecodedResponse{}, reportedMessagesError(body)
	}
	if response.Type != "message" {
		return llm.DecodedResponse{}, errUnexpectedResponseType
	}
	decoded := llm.DecodedResponse{ServedModel: response.Model, ResponseID: response.ID}
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			appendResponsePart(&decoded, llm.ResponsePart{Text: block.Text})
		case "thinking":
			appendResponsePart(&decoded, llm.ResponsePart{Text: block.Thinking, IsReasoning: true})
		}
	}
	if response.StopReason != nil {
		decoded.ProviderFinishReason = *response.StopReason
	}
	var usage messagesUsage
	usage.mergeCumulative(response.Usage)
	var err error
	if decoded.Usage, err = usage.textGenerationUsage(); err != nil {
		return llm.DecodedResponse{}, err
	}
	return decoded, nil
}

// decodeMessagesStream decodes the streaming event flow documented at
// https://platform.claude.com/docs/en/build-with-claude/streaming, which ends at message_stop.
func decodeMessagesStream(events *providerhttp.ServerSentEventReader, writeTextDelta func(string) error) (llm.DecodedResponse, error) {
	stream := &messagesStreamDecoder{writeTextDelta: writeTextDelta, blockTypes: map[int]string{}}
	for {
		event, err := events.ReadEvent()
		if errors.Is(err, io.EOF) {
			return llm.DecodedResponse{}, io.ErrUnexpectedEOF
		}
		if err != nil {
			return llm.DecodedResponse{}, fmt.Errorf("read messages stream: %w", err)
		}
		isStopped, err := stream.applyEvent(event)
		if err != nil {
			return llm.DecodedResponse{}, err
		}
		if isStopped {
			return stream.complete()
		}
	}
}

// messagesStreamDecoder accumulates one Messages event stream.
type messagesStreamDecoder struct {
	writeTextDelta func(string) error
	decoded        llm.DecodedResponse
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
		return false, errStreamNotStarted
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
		appendResponsePart(&stream.decoded, llm.ResponsePart{Text: block.Thinking, IsReasoning: true})
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
		appendResponsePart(&stream.decoded, llm.ResponsePart{Text: delta.Thinking, IsReasoning: true})
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
	appendResponsePart(&stream.decoded, llm.ResponsePart{Text: text})
	return stream.writeTextDelta(text)
}

func (stream *messagesStreamDecoder) complete() (llm.DecodedResponse, error) {
	usage, err := stream.usage.textGenerationUsage()
	if err != nil {
		return llm.DecodedResponse{}, err
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
func (usage *messagesUsage) textGenerationUsage() (llm.Usage, error) {
	cachedInputTokens := tokenCountValue(usage.CacheReadInputTokens)
	inputTokens, isValid := sumTokenCounts(tokenCountValue(usage.InputTokens), tokenCountValue(usage.CacheCreationInputTokens), cachedInputTokens)
	converted := llm.Usage{InputTokens: inputTokens, CachedInputTokens: cachedInputTokens, OutputTokens: tokenCountValue(usage.OutputTokens)}
	if usage.OutputTokensDetails != nil {
		converted.ReasoningTokens = tokenCountValue(usage.OutputTokensDetails.ThinkingTokens)
	}
	totalTokens, isTotalValid := sumTokenCounts(converted.InputTokens, converted.OutputTokens)
	if !isValid || !isTotalValid || converted.ReasoningTokens < 0 {
		return llm.Usage{}, errInvalidTokenCount
	}
	converted.TotalTokens = totalTokens
	return converted, nil
}

// reportedMessagesError keeps only the error envelope's bounded tokens, never its message.
func reportedMessagesError(body []byte) *llm.ProviderReportedError {
	return &llm.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, errorTokenPointers)}
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

// appendResponsePart merges consecutive parts of the same kind so a stream yields few parts.
func appendResponsePart(decoded *llm.DecodedResponse, part llm.ResponsePart) {
	if part.Text == "" {
		return
	}
	if count := len(decoded.Parts); count > 0 && decoded.Parts[count-1].IsReasoning == part.IsReasoning {
		decoded.Parts[count-1].Text += part.Text
		return
	}
	decoded.Parts = append(decoded.Parts, part)
}

func tokenCountValue(count *int64) int64 {
	if count == nil {
		return 0
	}
	return *count
}

// sumTokenCounts reports false for a negative count or an int64 overflow.
func sumTokenCounts(counts ...int64) (int64, bool) {
	var total int64
	for _, count := range counts {
		if count < 0 || total > math.MaxInt64-count {
			return 0, false
		}
		total += count
	}
	return total, true
}
