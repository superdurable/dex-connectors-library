// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini

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
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// promptBlockReasonTokenPrefix marks a promptFeedback.blockReason, whose OTHER value differs from the candidate finish reason OTHER.
	promptBlockReasonTokenPrefix = "blockReason:"
	// undocumentedPromptBlockReasonToken sends a block reason Google adds later to blocked, as generateContent does.
	undocumentedPromptBlockReasonToken = promptBlockReasonTokenPrefix + "UNDOCUMENTED"
)

var errMalformedGenerateContentResponse = errors.New("response is not a valid generateContent response")

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
var thinkingLevels = map[llm.ReasoningEffort]string{
	llm.ReasoningEffortMinimal: "MINIMAL", llm.ReasoningEffortLow: "LOW",
	llm.ReasoningEffortMedium: "MEDIUM", llm.ReasoningEffortHigh: "HIGH",
}

// thinkingLevelsWithoutMinimal are the levels Google documents for Gemini 3.7 Flash, 3.8 Flash, and 3.1 Pro.
var thinkingLevelsWithoutMinimal = map[llm.ReasoningEffort]string{
	llm.ReasoningEffortLow: "LOW", llm.ReasoningEffortMedium: "MEDIUM", llm.ReasoningEffortHigh: "HIGH",
}

var (
	// modelWithoutThinkingLevelPattern matches Gemini 1 and 2 models, which reject thinkingLevel with an error.
	modelWithoutThinkingLevelPattern   = regexp.MustCompile(`^gemini-[12]\.`)
	modelWithoutMinimalThinkingPattern = regexp.MustCompile(`^gemini-3\.[78]-flash|^gemini-3(\.1)?-pro`)
	// latestFlashOrProAliasPattern matches aliases for the newest Flash or Pro; 3.8 Flash and 3.1 Pro lack minimal.
	latestFlashOrProAliasPattern = regexp.MustCompile(`^gemini-(flash|pro)-latest$`)
)

// newGenerateTextWireFormat reuses generateContent's wire types and classification; the model travels in the path.
func newGenerateTextWireFormat() llm.WireFormat {
	return llm.WireFormat{
		ProviderName: providerName,
		ModelIDRule:  llm.ModelIDRulePathSegment,
		Features: llm.RequestFeatures{
			SupportsInstructions: true, SupportsStructuredOutput: true, SupportsMaxOutputTokens: true,
			SupportsTemperature: true, SupportsReasoningEffort: true,
		},
		CredentialHeader: llm.CredentialHeader{Name: "x-goog-api-key"},
		RulesForModel:    generateTextRulesForModel,
		EncodeRequest:    encodeGenerateTextRequest,
		DecodeResponse:   decodeGenerateTextResponse,
		FinishReasons:    generateTextFinishReasons(),
		ErrorRules: []llm.ErrorRule{
			// Gemini reports an invalid or expired key as 400 INVALID_ARGUMENT with this ErrorInfo reason.
			{StatusCode: http.StatusBadRequest, ErrorToken: "API_KEY_INVALID", Outcome: llm.ProviderRejectedOutcome(sdkgo.FailureAuthentication)},
		},
		ErrorTokenPointers:  googleErrorTokenPointers,
		ReadErrorRetryDelay: readGoogleRetryDelay,
		RequestIDHeaders:    []string{"X-Goog-Request-Id", "X-Request-Id"},
	}
}

func generateTextRulesForModel(model string) llm.ModelRequestRules {
	return llm.ModelRequestRules{
		Temperature:      llm.TemperatureRange(0, 2),
		ReasoningEfforts: thinkingLevelsForModel(model),
		StructuredOutput: llm.StructuredOutputRules{
			Mode: llm.StructuredOutputModeJSONSchema, KeywordsMovedToDescription: structuredOutputKeywordsMovedToDescription,
		},
	}
}

// thinkingLevelsForModel accepts every level for unlisted models, so a new Gemini model needs no release.
func thinkingLevelsForModel(model string) map[llm.ReasoningEffort]string {
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

func encodeGenerateTextRequest(input llm.EncodeRequestInput) (llm.EncodedRequest, error) {
	request := input.Request
	payload := wireGenerateContentRequest{Contents: make([]wireContent, 0, len(request.Messages))}
	for _, message := range request.Messages {
		role := "user"
		if message.Role == llm.MessageRoleAssistant {
			role = "model"
		}
		payload.Contents = append(payload.Contents, wireContent{Role: role, Parts: []wirePart{{Text: message.Text}}})
	}
	if request.Instructions != "" {
		payload.SystemInstruction = &wireContent{Parts: []wirePart{{Text: request.Instructions}}}
	}
	generationConfig := wireGenerationConfig{Temperature: request.Temperature, MaxOutputTokens: request.MaxOutputTokens}
	if request.StructuredOutput != nil {
		responseSchema, err := json.Marshal(responseSchemaWithDescription(request.StructuredOutput))
		if err != nil {
			return llm.EncodedRequest{}, errors.New("the structured output schema is not JSON serializable")
		}
		generationConfig.ResponseMIMEType = jsonMIMEType
		generationConfig.ResponseJSONSchema = responseSchema
	}
	if request.ReasoningEffort != "" {
		generationConfig.ThinkingConfig = &wireThinkingConfig{ThinkingLevel: input.Rules.ReasoningEfforts[request.ReasoningEffort]}
	}
	if !generationConfig.isEmpty() {
		payload.GenerationConfig = &generationConfig
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return llm.EncodedRequest{}, errors.New("the request is not JSON serializable")
	}
	return llm.EncodedRequest{Path: "/models/" + request.Model + ":generateContent", Body: body}, nil
}

// responseSchemaWithDescription carries StructuredOutput.Description in the
// root description, because generateContent has no schema description field.
func responseSchemaWithDescription(output *llm.StructuredOutput) map[string]any {
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

// decodeGenerateTextResponse reads the first candidate; a prompt block becomes a blockReason: token.
func decodeGenerateTextResponse(body []byte) (llm.DecodedResponse, error) {
	var wireResponse wireGenerateContentResponse
	if err := json.Unmarshal(body, &wireResponse); err != nil {
		return llm.DecodedResponse{}, errMalformedGenerateContentResponse
	}
	if len(wireResponse.Error) > 0 && string(wireResponse.Error) != "null" {
		return llm.DecodedResponse{}, &llm.ProviderReportedError{ErrorTokens: providerhttp.ReadErrorTokens(body, googleErrorTokenPointers)}
	}
	usage := wireResponse.UsageMetadata
	decoded := llm.DecodedResponse{
		ServedModel: wireResponse.ModelVersion, ResponseID: wireResponse.ResponseID,
		Usage: llm.Usage{
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
	decoded.Parts = make([]llm.ResponsePart, 0, len(candidate.Content.Parts))
	for _, part := range candidate.Content.Parts {
		decoded.Parts = append(decoded.Parts, llm.ResponsePart{Text: part.Text, IsReasoning: part.Thought})
	}
	return decoded, nil
}

// promptBlockReasonToken returns the finish token for a block reason; an ill-formed reason returns none, selecting invalidResponse.
func promptBlockReasonToken(blockReason string) string {
	switch {
	case slices.Contains(promptBlockReasons, blockReason):
		return promptBlockReasonTokenPrefix + blockReason
	case providerEnumPattern.MatchString(blockReason):
		return undocumentedPromptBlockReasonToken
	default:
		return ""
	}
}

// generateTextFinishReasons accepts the tokens generateContent classifies as
// generated, truncated, or blocked; every other token selects invalidResponse.
func generateTextFinishReasons() map[string]llm.FinishReason {
	finishReasons := map[string]llm.FinishReason{"STOP": llm.FinishReasonStop, "MAX_TOKENS": llm.FinishReasonLength}
	for finishReason := range contentPolicyFinishReasons {
		finishReasons[finishReason] = llm.FinishReasonContentPolicy
	}
	for _, blockReason := range promptBlockReasons {
		finishReasons[promptBlockReasonTokenPrefix+blockReason] = llm.FinishReasonContentPolicy
	}
	finishReasons[undocumentedPromptBlockReasonToken] = llm.FinishReasonContentPolicy
	return finishReasons
}

func readGoogleRetryDelay(errorBody []byte) time.Duration {
	return parseGoogleErrorDetails(errorBody).retryDelay
}
