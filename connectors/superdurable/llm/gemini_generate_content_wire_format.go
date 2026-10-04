// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
)

const (
	// geminiAPIBaseURL is Google's public Gemini API; the wire format appends /models/{model}:generateContent.
	geminiAPIBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	geminiJSONMIMEType = "application/json"
	// promptBlockReasonTokenPrefix marks a promptFeedback.blockReason, whose OTHER value differs from the candidate finish reason OTHER.
	promptBlockReasonTokenPrefix = "blockReason:"
	// undocumentedPromptBlockReasonToken sends a block reason Google adds later to blocked, as generateContent does.
	undocumentedPromptBlockReasonToken = promptBlockReasonTokenPrefix + "UNDOCUMENTED"
	maxGoogleRetryDelay                = time.Hour
)

var (
	googleEnumPattern          = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	googleProtoDurationPattern = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,9})?s$`)
)

var errMalformedGenerateContentResponse = errors.New("response is not a valid generateContent response")

// geminiContentPolicyFinishReasons are the documented finish reasons that stop a
// candidate because of safety, recitation, or another content policy.
var geminiContentPolicyFinishReasons = map[string]bool{
	"SAFETY": true, "RECITATION": true, "LANGUAGE": true, "BLOCKLIST": true,
	"PROHIBITED_CONTENT": true, "SPII": true, "IMAGE_SAFETY": true,
	"IMAGE_PROHIBITED_CONTENT": true, "IMAGE_RECITATION": true, "IMAGE_OTHER": true,
	"ESCALATION": true, "PUP_LIMITED_DISABLED": true,
}

// promptBlockReasons are the documented promptFeedback.blockReason values other than BLOCK_REASON_UNSPECIFIED.
var promptBlockReasons = []string{"SAFETY", "OTHER", "BLOCKLIST", "PROHIBITED_CONTENT", "IMAGE_SAFETY"}

// googleErrorTokenPointers read the google.rpc status name and ErrorInfo reason, including the array-wrapped error form.
var googleErrorTokenPointers = []string{
	"/error/status", "/error/details/0/reason", "/error/details/1/reason", "/error/details/2/reason",
	"/0/error/status", "/0/error/details/0/reason", "/0/error/details/1/reason", "/0/error/details/2/reason",
}

// structuredOutputKeywordsMovedToDescription are keywords responseJsonSchema omits; format moves whole, because its uuid is undocumented.
var structuredOutputKeywordsMovedToDescription = []string{"minLength", "maxLength", "format"}

// thinkingLevels maps each effort to generationConfig.thinkingConfig.thinkingLevel for Gemini 3 and later.
var thinkingLevels = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortMinimal: "MINIMAL", textgen.ReasoningEffortLow: "LOW",
	textgen.ReasoningEffortMedium: "MEDIUM", textgen.ReasoningEffortHigh: "HIGH",
}

// thinkingLevelsWithoutMinimal are the levels Google documents for Gemini 3.7 Flash, 3.8 Flash, and 3.1 Pro.
var thinkingLevelsWithoutMinimal = map[textgen.ReasoningEffort]string{
	textgen.ReasoningEffortLow: "LOW", textgen.ReasoningEffortMedium: "MEDIUM", textgen.ReasoningEffortHigh: "HIGH",
}

var (
	// modelWithoutThinkingLevelPattern matches Gemini 1 and 2 models, which reject thinkingLevel with an error.
	modelWithoutThinkingLevelPattern   = regexp.MustCompile(`^gemini-[12]\.`)
	modelWithoutMinimalThinkingPattern = regexp.MustCompile(`^gemini-3\.[78]-flash|^gemini-3(\.1)?-pro`)
	// latestFlashOrProAliasPattern matches aliases for the newest Flash or Pro; 3.8 Flash and 3.1 Pro lack minimal.
	latestFlashOrProAliasPattern = regexp.MustCompile(`^gemini-(flash|pro)-latest$`)
)

type geminiWirePart struct {
	Text string `json:"text"`
}

type geminiWireContent struct {
	Role  string           `json:"role,omitempty"`
	Parts []geminiWirePart `json:"parts"`
}

type geminiWireThinkingConfig struct {
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

type geminiWireGenerationConfig struct {
	ResponseMIMEType   string                    `json:"responseMimeType,omitempty"`
	ResponseJSONSchema json.RawMessage           `json:"responseJsonSchema,omitempty"`
	Temperature        *float64                  `json:"temperature,omitempty"`
	MaxOutputTokens    int                       `json:"maxOutputTokens,omitempty"`
	ThinkingConfig     *geminiWireThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiWireGenerateContentRequest struct {
	Contents          []geminiWireContent         `json:"contents"`
	SystemInstruction *geminiWireContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiWireGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiWireGenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text    string `json:"text"`
				Thought bool   `json:"thought"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	// Error classifies an error object inside a 2xx body.
	Error          json.RawMessage `json:"error"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
		TotalTokenCount         int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
	ResponseID   string `json:"responseId"`
}

type googleWireError struct {
	Error struct {
		Details []struct {
			Type       string `json:"@type"`
			RetryDelay string `json:"retryDelay"`
		} `json:"details"`
	} `json:"error"`
}

// newGeminiGenerateContentWireFormat sends models.generateContent without streaming; the model travels in the path.
func newGeminiGenerateContentWireFormat(*Config) (textgen.WireFormat, error) {
	return textgen.WireFormat{
		ProviderName: string(ProviderGemini),
		ModelIDRule:  textgen.ModelIDRulePathSegment,
		Features: textgen.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true,
			SupportsTemperature: true, SupportsReasoningEffort: true,
		},
		CredentialHeader: textgen.CredentialHeader{Name: "x-goog-api-key"},
		RulesForModel:    generateContentRulesForModel,
		EncodeRequest:    encodeGenerateContentRequest,
		DecodeResponse:   decodeGenerateContentResponse,
		FinishReasons:    generateContentFinishReasons(),
		ErrorRules: []textgen.ErrorRule{
			// Gemini reports an invalid or expired key as 400 INVALID_ARGUMENT with this ErrorInfo reason.
			{StatusCode: http.StatusBadRequest, ErrorToken: "API_KEY_INVALID", Outcome: textgen.ProviderRejectedOutcome(sdkgo.FailureAuthentication)},
		},
		ErrorTokenPointers:  googleErrorTokenPointers,
		ReadErrorRetryDelay: readGoogleRetryDelay,
		RequestIDHeaders:    []string{"X-Goog-Request-Id", "X-Request-Id"},
	}, nil
}

func generateContentRulesForModel(model string) textgen.ModelRequestRules {
	return textgen.ModelRequestRules{
		Temperature:      textgen.TemperatureRange(0, 2),
		ReasoningEfforts: thinkingLevelsForModel(model),
		StructuredOutput: textgen.StructuredOutputRules{
			Mode: textgen.StructuredOutputModeJSONSchema, KeywordsMovedToDescription: structuredOutputKeywordsMovedToDescription,
		},
	}
}

// thinkingLevelsForModel accepts every level for unlisted models, so a new Gemini model needs no release.
func thinkingLevelsForModel(model string) map[textgen.ReasoningEffort]string {
	switch {
	case modelWithoutThinkingLevelPattern.MatchString(model):
		return nil
	case modelWithoutMinimalThinkingPattern.MatchString(model) && !strings.Contains(model, "-lite"),
		latestFlashOrProAliasPattern.MatchString(model):
		return thinkingLevelsWithoutMinimal
	default:
		return thinkingLevels
	}
}

func encodeGenerateContentRequest(input textgen.EncodeRequestInput) (textgen.EncodedRequest, error) {
	request := input.Request
	payload := geminiWireGenerateContentRequest{Contents: make([]geminiWireContent, 0, len(request.Messages))}
	for _, message := range request.Messages {
		role := "user"
		if message.Role == textgen.MessageRoleAssistant {
			role = "model"
		}
		payload.Contents = append(payload.Contents, geminiWireContent{Role: role, Parts: []geminiWirePart{{Text: message.Text}}})
	}
	if request.Instructions != "" {
		payload.SystemInstruction = &geminiWireContent{Parts: []geminiWirePart{{Text: request.Instructions}}}
	}
	generationConfig := geminiWireGenerationConfig{Temperature: request.Temperature, MaxOutputTokens: request.MaxOutputTokens}
	if request.StructuredOutput != nil {
		responseSchema, err := json.Marshal(responseSchemaWithDescription(request.StructuredOutput))
		if err != nil {
			return textgen.EncodedRequest{}, errors.New("the structured output schema is not JSON serializable")
		}
		generationConfig.ResponseMIMEType = geminiJSONMIMEType
		generationConfig.ResponseJSONSchema = responseSchema
	}
	if request.ReasoningEffort != "" {
		generationConfig.ThinkingConfig = &geminiWireThinkingConfig{ThinkingLevel: input.Rules.ReasoningEfforts[request.ReasoningEffort]}
	}
	if !generationConfig.isEmpty() {
		payload.GenerationConfig = &generationConfig
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return textgen.EncodedRequest{}, errors.New("the request is not JSON serializable")
	}
	return textgen.EncodedRequest{Path: "/models/" + request.Model + ":generateContent", Body: body}, nil
}

func (generationConfig geminiWireGenerationConfig) isEmpty() bool {
	return generationConfig.ResponseMIMEType == "" && generationConfig.ResponseJSONSchema == nil &&
		generationConfig.Temperature == nil && generationConfig.MaxOutputTokens == 0 && generationConfig.ThinkingConfig == nil
}

// responseSchemaWithDescription carries StructuredOutput.Description in the
// root description, because generateContent has no schema description field.
func responseSchemaWithDescription(output *textgen.StructuredOutput) map[string]any {
	if output.Description == "" {
		return output.Schema
	}
	schema := maps.Clone(output.Schema)
	description := output.Description
	if existing, isString := schema["description"].(string); isString && existing != "" {
		description += "\n\n" + existing
	}
	schema["description"] = description
	return schema
}

// decodeGenerateContentResponse reads the first candidate; a prompt block becomes a blockReason: token.
func decodeGenerateContentResponse(body []byte) (textgen.DecodedResponse, error) {
	var wireResponse geminiWireGenerateContentResponse
	if err := json.Unmarshal(body, &wireResponse); err != nil {
		return textgen.DecodedResponse{}, errMalformedGenerateContentResponse
	}
	if len(wireResponse.Error) > 0 && string(wireResponse.Error) != "null" {
		return textgen.DecodedResponse{}, &textgen.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, googleErrorTokenPointers)}
	}
	usage := wireResponse.UsageMetadata
	decoded := textgen.DecodedResponse{
		ServedModel: wireResponse.ModelVersion, ResponseID: wireResponse.ResponseID,
		Usage: textgen.Usage{
			InputTokens: int64(usage.PromptTokenCount), CachedInputTokens: int64(usage.CachedContentTokenCount),
			OutputTokens:    int64(usage.CandidatesTokenCount) + int64(usage.ThoughtsTokenCount),
			ReasoningTokens: int64(usage.ThoughtsTokenCount), TotalTokens: int64(usage.TotalTokenCount),
		},
	}
	if blockReason := wireResponse.PromptFeedback.BlockReason; blockReason != "" && blockReason != "BLOCK_REASON_UNSPECIFIED" {
		decoded.ProviderFinishReason = promptBlockReasonToken(blockReason)
		return decoded, nil
	}
	if len(wireResponse.Candidates) == 0 {
		// No finish reason selects invalidResponse and keeps the identity, usage, and receipt.
		return decoded, nil
	}
	candidate := wireResponse.Candidates[0]
	decoded.ProviderFinishReason = candidate.FinishReason
	decoded.Parts = make([]textgen.ResponsePart, 0, len(candidate.Content.Parts))
	for _, part := range candidate.Content.Parts {
		decoded.Parts = append(decoded.Parts, textgen.ResponsePart{Text: part.Text, IsReasoning: part.Thought})
	}
	return decoded, nil
}

// promptBlockReasonToken returns the finish token for a block reason; an ill-formed reason returns none, selecting invalidResponse.
func promptBlockReasonToken(blockReason string) string {
	switch {
	case slices.Contains(promptBlockReasons, blockReason):
		return promptBlockReasonTokenPrefix + blockReason
	case googleEnumPattern.MatchString(blockReason):
		return undocumentedPromptBlockReasonToken
	default:
		return ""
	}
}

// generateContentFinishReasons accepts the tokens Gemini's generateContent
// classifies as generated, truncated, or blocked; every other token selects invalidResponse.
func generateContentFinishReasons() map[string]textgen.FinishReason {
	finishReasons := map[string]textgen.FinishReason{"STOP": textgen.FinishReasonStop, "MAX_TOKENS": textgen.FinishReasonLength}
	for finishReason := range geminiContentPolicyFinishReasons {
		finishReasons[finishReason] = textgen.FinishReasonContentPolicy
	}
	for _, blockReason := range promptBlockReasons {
		finishReasons[promptBlockReasonTokenPrefix+blockReason] = textgen.FinishReasonContentPolicy
	}
	finishReasons[undocumentedPromptBlockReasonToken] = textgen.FinishReasonContentPolicy
	return finishReasons
}

// readGoogleRetryDelay reads the google.rpc RetryInfo delay from a bounded error body, including the array-wrapped form.
func readGoogleRetryDelay(errorBody []byte) time.Duration {
	var wireError googleWireError
	if json.Unmarshal(errorBody, &wireError) != nil {
		var wrappedErrors []googleWireError
		if json.Unmarshal(errorBody, &wrappedErrors) != nil || len(wrappedErrors) == 0 {
			return 0
		}
		wireError = wrappedErrors[0]
	}
	var retryDelay time.Duration
	for _, detail := range wireError.Error.Details {
		if detail.Type == "type.googleapis.com/google.rpc.RetryInfo" {
			retryDelay = parseGoogleProtoDuration(detail.RetryDelay)
		}
	}
	return retryDelay
}

// parseGoogleProtoDuration parses a google.protobuf.Duration JSON value such as "37s".
func parseGoogleProtoDuration(value string) time.Duration {
	if !googleProtoDurationPattern.MatchString(value) {
		return 0
	}
	delay, err := time.ParseDuration(value)
	if err != nil || delay <= 0 {
		return 0
	}
	return min(delay, maxGoogleRetryDelay)
}
