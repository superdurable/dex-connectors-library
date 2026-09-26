// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gemini implements the Google Gemini API models.generateContent
// method as a Dex Connector.
package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	providerName = "gemini"
	operationID  = "generateContent"
	jsonMIMEType = "application/json"

	// defaultRequestTimeout stays below the 300-second Execute timeout, so a stalled exchange returns Retry first.
	defaultRequestTimeout = 270 * time.Second
	maxErrorResponseBytes = 64 << 10
	maxProviderRetryDelay = time.Hour
)

var (
	modelIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	providerEnumPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	protoDurationPattern = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,9})?s$`)
)

// contentPolicyFinishReasons are the documented finish reasons that stop a
// candidate because of safety, recitation, or another content policy.
var contentPolicyFinishReasons = map[string]bool{
	"SAFETY": true, "RECITATION": true, "LANGUAGE": true, "BLOCKLIST": true,
	"PROHIBITED_CONTENT": true, "SPII": true, "IMAGE_SAFETY": true,
	"IMAGE_PROHIBITED_CONTENT": true, "IMAGE_RECITATION": true, "IMAGE_OTHER": true,
	"ESCALATION": true, "PUP_LIMITED_DISABLED": true,
}

// Option configures a code dependency that is not part of the serializable Config.
type Option func(*clientOptions)

type clientOptions struct{ httpClient *http.Client }

// WithHTTPClient supplies the HTTP client used for provider calls. The
// connector uses a copy, disables redirects so the API key header is never
// forwarded, and applies a 270-second timeout when the client sets none.
// The caller retains ownership of the original client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client calls the Gemini API with credentials resolved for each provider call.
// Its methods never modify it, so concurrent calls are safe when the credential
// provider is safe for concurrent use.
type Client struct {
	endpoint         string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
}

// Part is one text part of a Content.
type Part struct {
	// Text is the part text. It must not be empty.
	Text string `json:"text"`
}

// Content is one conversation turn. Role is "user", "model", or empty for a
// single-turn request.
type Content struct {
	// Role is "user", "model", or empty for a single-turn request.
	Role string `json:"role,omitempty"`
	// Parts holds the turn's text parts in order. At least one part is required.
	Parts []Part `json:"parts"`
}

// GenerateContentRequest is the provider input for GenerateContent.
//
// Model accepts a model ID such as "gemini-3.5-flash-lite" or "models/gemini-3.5-flash-lite".
// ResponseJSONSchema maps to generationConfig.responseJsonSchema and defaults
// ResponseMIMEType to application/json. ThinkingBudget maps to
// generationConfig.thinkingConfig.thinkingBudget: nil leaves the model default,
// -1 selects dynamic thinking, 0 disables thinking where the model allows it,
// and a positive value sets a token budget. Gemini 3 models treat
// thinkingBudget as a legacy setting that may not turn thinking off, and Google
// recommends their default temperature of 1.0, so leave ThinkingBudget and
// Temperature nil for them.
type GenerateContentRequest struct {
	// Model is the Gemini model ID, with or without the "models/" prefix.
	Model string `json:"model"`
	// SystemInstruction is optional system text sent as systemInstruction.parts[0].text.
	SystemInstruction string `json:"systemInstruction,omitempty"`
	// Contents holds the conversation turns in order. At least one Content is required.
	Contents []Content `json:"contents"`
	// ResponseMIMEType maps to generationConfig.responseMimeType. Empty omits it
	// unless ResponseJSONSchema is set.
	ResponseMIMEType string `json:"responseMimeType,omitempty"`
	// ResponseJSONSchema maps to generationConfig.responseJsonSchema and requires
	// the application/json response MIME type. Nil omits it.
	ResponseJSONSchema map[string]any `json:"responseJsonSchema,omitempty"`
	// Temperature maps to generationConfig.temperature and must be between 0 and 2.
	// Nil leaves the model default.
	Temperature *float64 `json:"temperature,omitempty"`
	// MaxOutputTokens maps to generationConfig.maxOutputTokens and includes thought
	// tokens. Zero leaves the model default; a negative value is invalid.
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	// ThinkingBudget maps to generationConfig.thinkingConfig.thinkingBudget. Nil
	// leaves the model default, -1 is dynamic, 0 disables thinking where the model
	// allows it, and a positive value is a token budget.
	ThinkingBudget *int `json:"thinkingBudget,omitempty"`
}

// Usage is the token usage Gemini reported for one request.
type Usage struct {
	// PromptTokens is usageMetadata.promptTokenCount.
	PromptTokens int `json:"promptTokens"`
	// CandidateTokens is usageMetadata.candidatesTokenCount.
	CandidateTokens int `json:"candidateTokens"`
	// ThoughtsTokens is usageMetadata.thoughtsTokenCount.
	ThoughtsTokens int `json:"thoughtsTokens"`
	// CachedContentTokens is usageMetadata.cachedContentTokenCount, the cached subset of PromptTokens.
	CachedContentTokens int `json:"cachedContentTokens"`
	// TotalTokens is usageMetadata.totalTokenCount.
	TotalTokens int `json:"totalTokens"`
}

// GenerateContentResponse is the bounded result of one generateContent call.
//
// Text concatenates the non-thought text parts of the first candidate. Only the
// generated and truncated branches carry Text; blocked and invalidResponse
// results keep identity, finish or block reason, and usage without content.
type GenerateContentResponse struct {
	// ResponseID is the Gemini responseId, also recorded as the Receipt provider object ID.
	ResponseID string `json:"responseId,omitempty"`
	// ModelVersion is the Gemini modelVersion that produced the response.
	ModelVersion string `json:"modelVersion,omitempty"`
	// Text is the first candidate's non-thought text. It is empty outside the
	// generated and truncated branches.
	Text string `json:"text"`
	// FinishReason is the first candidate's finish reason, such as STOP or MAX_TOKENS.
	FinishReason string `json:"finishReason,omitempty"`
	// BlockReason is promptFeedback.blockReason when Gemini blocked the prompt.
	BlockReason string `json:"blockReason,omitempty"`
	// Usage is the token usage Gemini reported.
	Usage Usage `json:"usage"`
}

// GenerateContentOperation calls models.generateContent. It is a Query:
// the method is stateless and creates no provider resource, so repeating it
// after an ambiguous failure is safe and only bills the tokens again. The
// Gemini API accepts no idempotency key.
type GenerateContentOperation struct{ client *Client }

type wirePart struct {
	Text string `json:"text"`
}

type wireContent struct {
	Role  string     `json:"role,omitempty"`
	Parts []wirePart `json:"parts"`
}

type wireThinkingConfig struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type wireGenerationConfig struct {
	ResponseMIMEType   string              `json:"responseMimeType,omitempty"`
	ResponseJSONSchema json.RawMessage     `json:"responseJsonSchema,omitempty"`
	Temperature        *float64            `json:"temperature,omitempty"`
	MaxOutputTokens    int                 `json:"maxOutputTokens,omitempty"`
	ThinkingConfig     *wireThinkingConfig `json:"thinkingConfig,omitempty"`
}

type wireGenerateContentRequest struct {
	Contents          []wireContent         `json:"contents"`
	SystemInstruction *wireContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *wireGenerationConfig `json:"generationConfig,omitempty"`
}

type wireGenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text    string `json:"text"`
				Thought bool   `json:"thought"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
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

type googleErrorDetails struct {
	status     string
	reason     string
	retryDelay time.Duration
}

type wireGoogleError struct {
	Error struct {
		Status  string `json:"status"`
		Details []struct {
			Type       string `json:"@type"`
			Reason     string `json:"reason"`
			RetryDelay string `json:"retryDelay"`
		} `json:"details"`
	} `json:"error"`
}

// New creates a Gemini API client. It applies Config defaults, validates the
// endpoint and maxResponseBytes, and returns an error for a nil credential
// provider or a nil Option. Credentials are resolved again for every call.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := validateAndTrimEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Gemini maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Gemini connector option is nil")
		}
		option(&dependencies)
	}
	httpClient := &http.Client{Timeout: defaultRequestTimeout}
	if dependencies.httpClient != nil {
		callerHTTPClient := *dependencies.httpClient
		if callerHTTPClient.Timeout == 0 {
			callerHTTPClient.Timeout = defaultRequestTimeout
		}
		httpClient = &callerHTTPClient
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		endpoint: endpoint, httpClient: httpClient, credentials: credentials,
		maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

// String returns a redacted description, so formatting a Client never reveals credentials.
func (*Client) String() string { return "gemini.Client{[REDACTED]}" }

// GoString returns the same redacted description for the %#v verb.
func (*Client) GoString() string { return "gemini.Client{[REDACTED]}" }

// GenerateContent returns the generateContent Query operation.
func (client *Client) GenerateContent() GenerateContentOperation {
	return GenerateContentOperation{client: client}
}

// Definition returns the immutable generateContent Query definition.
func (GenerateContentOperation) Definition() sdkgo.QueryDefinition {
	return GenerateContentDefinition
}

// Invoke makes one models.generateContent call and classifies the attempt.
// Invalid local input or credentials select defect without dispatch; transport
// failures and retryable HTTP statuses return Retry; every other outcome
// selects a branch. Failures never contain provider text, prompts, or the key.
func (operation GenerateContentOperation) Invoke(call sdkgo.Call, input GenerateContentRequest) sdkgo.QueryAttempt[GenerateContentResponse] {
	model, body, failure := encodeGenerateContentRequest(input)
	if failure != nil {
		return sdkgo.NewQueryBranch(GenerateContentBranchDefect, GenerateContentResponse{}, failure, sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, model, body)
	if failure != nil {
		return sdkgo.NewQueryBranch(GenerateContentBranchDefect, GenerateContentResponse{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[GenerateContentResponse](geminiFailure(sdkgo.FailureTransport, "provider request failed before a response was received"), 0)
	}
	defer response.Body.Close()
	requestID := googleRequestID(response.Header)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return statusAttempt(call, response, requestID)
	}
	responseBody, failure := readBoundedResponse(response.Body, operation.client.maxResponseBytes)
	if failure != nil {
		if failure.Kind == sdkgo.FailureResponseTooLarge {
			return sdkgo.NewQueryBranch(GenerateContentBranchInvalidResponse, GenerateContentResponse{}, failure, responseReceipt(call, requestID, ""))
		}
		return sdkgo.NewQueryRetry[GenerateContentResponse](*failure, 0)
	}
	var wireResponse wireGenerateContentResponse
	if err := json.Unmarshal(responseBody, &wireResponse); err != nil {
		failure := geminiFailure(sdkgo.FailureProtocol, "provider response is not a valid generateContent response")
		return sdkgo.NewQueryBranch(GenerateContentBranchInvalidResponse, GenerateContentResponse{}, &failure, responseReceipt(call, requestID, ""))
	}
	branch, value, failure := classifyGenerateContentResponse(wireResponse)
	return sdkgo.NewQueryBranch(branch, value, failure, responseReceipt(call, requestID, value.ResponseID))
}

// encodeGenerateContentRequest validates input locally, so a request that the
// provider would certainly reject selects defect without dispatch.
func encodeGenerateContentRequest(input GenerateContentRequest) (string, []byte, *sdkgo.Failure) {
	model := strings.TrimPrefix(input.Model, "models/")
	if !modelIDPattern.MatchString(model) {
		return "", nil, failurePointer(sdkgo.FailureValidation, "model must be a Gemini model ID such as gemini-3.5-flash-lite")
	}
	if len(input.Contents) == 0 {
		return "", nil, failurePointer(sdkgo.FailureValidation, "at least one content is required")
	}
	payload := wireGenerateContentRequest{Contents: make([]wireContent, 0, len(input.Contents))}
	for _, content := range input.Contents {
		if content.Role != "" && content.Role != "user" && content.Role != "model" {
			return "", nil, failurePointer(sdkgo.FailureValidation, "content role must be user, model, or empty")
		}
		if len(content.Parts) == 0 {
			return "", nil, failurePointer(sdkgo.FailureValidation, "every content requires at least one part")
		}
		parts := make([]wirePart, 0, len(content.Parts))
		for _, part := range content.Parts {
			if part.Text == "" {
				return "", nil, failurePointer(sdkgo.FailureValidation, "every part requires text")
			}
			parts = append(parts, wirePart(part))
		}
		payload.Contents = append(payload.Contents, wireContent{Role: content.Role, Parts: parts})
	}
	if input.SystemInstruction != "" {
		payload.SystemInstruction = &wireContent{Parts: []wirePart{{Text: input.SystemInstruction}}}
	}
	generationConfig := wireGenerationConfig{ResponseMIMEType: input.ResponseMIMEType}
	if input.ResponseJSONSchema != nil {
		if generationConfig.ResponseMIMEType == "" {
			generationConfig.ResponseMIMEType = jsonMIMEType
		}
		if generationConfig.ResponseMIMEType != jsonMIMEType {
			return "", nil, failurePointer(sdkgo.FailureValidation, "responseJsonSchema requires the application/json response MIME type")
		}
		responseSchema, err := json.Marshal(input.ResponseJSONSchema)
		if err != nil {
			return "", nil, failurePointer(sdkgo.FailureValidation, "responseJsonSchema must be JSON serializable")
		}
		generationConfig.ResponseJSONSchema = responseSchema
	}
	if input.Temperature != nil {
		temperature := *input.Temperature
		if math.IsNaN(temperature) || temperature < 0 || temperature > 2 {
			return "", nil, failurePointer(sdkgo.FailureValidation, "temperature must be between 0 and 2")
		}
		generationConfig.Temperature = &temperature
	}
	if input.MaxOutputTokens < 0 {
		return "", nil, failurePointer(sdkgo.FailureValidation, "maxOutputTokens cannot be negative")
	}
	generationConfig.MaxOutputTokens = input.MaxOutputTokens
	if input.ThinkingBudget != nil {
		if *input.ThinkingBudget < -1 {
			return "", nil, failurePointer(sdkgo.FailureValidation, "thinkingBudget must be -1, 0, or a positive token budget")
		}
		generationConfig.ThinkingConfig = &wireThinkingConfig{ThinkingBudget: *input.ThinkingBudget}
	}
	if !generationConfig.isEmpty() {
		payload.GenerationConfig = &generationConfig
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, failurePointer(sdkgo.FailureValidation, "request is not JSON serializable")
	}
	return model, body, nil
}

func (generationConfig wireGenerationConfig) isEmpty() bool {
	return generationConfig.ResponseMIMEType == "" && generationConfig.ResponseJSONSchema == nil &&
		generationConfig.Temperature == nil && generationConfig.MaxOutputTokens == 0 && generationConfig.ThinkingConfig == nil
}

func (client *Client) newRequest(call sdkgo.Call, model string, body []byte) (*http.Request, *sdkgo.Failure) {
	credential, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, failurePointer(sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if err := credential.Validate(); err != nil || !isValidAPIKey(credential.APIKey.Reveal()) {
		return nil, failurePointer(sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	target := client.endpoint + "/models/" + url.PathEscape(model) + ":generateContent"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, failurePointer(sdkgo.FailureLocalDefect, "request could not be built")
	}
	// The API key travels only in this header; it never appears in the URL.
	request.Header.Set("x-goog-api-key", credential.APIKey.Reveal())
	request.Header.Set("Content-Type", jsonMIMEType)
	request.Header.Set("Accept", jsonMIMEType)
	return request, nil
}

// classifyGenerateContentResponse maps a decoded 2xx response to one branch.
// It never copies provider text into a Failure.
func classifyGenerateContentResponse(wireResponse wireGenerateContentResponse) (sdkgo.BranchID, GenerateContentResponse, *sdkgo.Failure) {
	usage := wireResponse.UsageMetadata
	value := GenerateContentResponse{
		ResponseID: wireResponse.ResponseID, ModelVersion: wireResponse.ModelVersion,
		Usage: Usage{
			PromptTokens: usage.PromptTokenCount, CandidateTokens: usage.CandidatesTokenCount,
			ThoughtsTokens: usage.ThoughtsTokenCount, CachedContentTokens: usage.CachedContentTokenCount,
			TotalTokens: usage.TotalTokenCount,
		},
	}
	if blockReason := wireResponse.PromptFeedback.BlockReason; blockReason != "" && blockReason != "BLOCK_REASON_UNSPECIFIED" {
		if !providerEnumPattern.MatchString(blockReason) {
			return invalidResponseClassification(value, "provider returned an unrecognized block reason")
		}
		value.BlockReason = blockReason
		return GenerateContentBranchBlocked, value, failurePointer(sdkgo.FailureProviderRejection, "provider blocked the prompt: "+blockReason)
	}
	if len(wireResponse.Candidates) == 0 {
		return invalidResponseClassification(value, "provider returned no candidates")
	}
	candidate := wireResponse.Candidates[0]
	finishReason := candidate.FinishReason
	if finishReason == "" {
		return invalidResponseClassification(value, "provider candidate has no finish reason")
	}
	if !providerEnumPattern.MatchString(finishReason) {
		return invalidResponseClassification(value, "provider returned an unrecognized finish reason")
	}
	value.FinishReason = finishReason
	var text strings.Builder
	for _, part := range candidate.Content.Parts {
		if !part.Thought {
			text.WriteString(part.Text)
		}
	}
	switch {
	case finishReason == "STOP":
		if text.Len() == 0 {
			return invalidResponseClassification(value, "provider candidate finished without text")
		}
		value.Text = text.String()
		return GenerateContentBranchGenerated, value, nil
	case finishReason == "MAX_TOKENS":
		value.Text = text.String()
		return GenerateContentBranchTruncated, value, failurePointer(sdkgo.FailureResponseTooLarge, "provider stopped the candidate at the output token limit")
	case contentPolicyFinishReasons[finishReason]:
		return GenerateContentBranchBlocked, value, failurePointer(sdkgo.FailureProviderRejection, "provider stopped the candidate: "+finishReason)
	default:
		return invalidResponseClassification(value, "provider stopped the candidate without a usable result: "+finishReason)
	}
}

func invalidResponseClassification(value GenerateContentResponse, message string) (sdkgo.BranchID, GenerateContentResponse, *sdkgo.Failure) {
	return GenerateContentBranchInvalidResponse, value, failurePointer(sdkgo.FailureProtocol, message)
}

func readBoundedResponse(body io.Reader, maxBytes int64) ([]byte, *sdkgo.Failure) {
	responseBody, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, failurePointer(sdkgo.FailureTransport, "provider response could not be read")
	}
	if int64(len(responseBody)) > maxBytes {
		return nil, failurePointer(sdkgo.FailureResponseTooLarge, "provider response exceeds the configured size limit")
	}
	return responseBody, nil
}

func statusAttempt(call sdkgo.Call, response *http.Response, requestID string) sdkgo.QueryAttempt[GenerateContentResponse] {
	status := response.StatusCode
	details := readGoogleErrorDetails(response.Body)
	message := "provider returned HTTP " + strconv.Itoa(status)
	if details.status != "" {
		message += " " + details.status
	}
	failure := geminiFailure(statusFailureKind(status, details.reason), message)
	if isRetryableStatus(status) {
		delay := details.retryDelay
		if delay == 0 {
			delay = retryAfterDelay(response.Header, time.Now())
		}
		return sdkgo.NewQueryRetry[GenerateContentResponse](failure, delay)
	}
	return sdkgo.NewQueryBranch(GenerateContentBranchProviderRejected, GenerateContentResponse{}, &failure, responseReceipt(call, requestID, ""))
}

// readGoogleErrorDetails extracts only the google.rpc status name, ErrorInfo
// reason, and RetryInfo delay from a bounded error body.
func readGoogleErrorDetails(body io.Reader) googleErrorDetails {
	errorBody, err := io.ReadAll(io.LimitReader(body, maxErrorResponseBytes))
	if err != nil {
		return googleErrorDetails{}
	}
	var wireError wireGoogleError
	if json.Unmarshal(errorBody, &wireError) != nil {
		var wrappedErrors []wireGoogleError
		if json.Unmarshal(errorBody, &wrappedErrors) != nil || len(wrappedErrors) == 0 {
			return googleErrorDetails{}
		}
		wireError = wrappedErrors[0]
	}
	details := googleErrorDetails{}
	if providerEnumPattern.MatchString(wireError.Error.Status) {
		details.status = wireError.Error.Status
	}
	for _, detail := range wireError.Error.Details {
		switch detail.Type {
		case "type.googleapis.com/google.rpc.ErrorInfo":
			if providerEnumPattern.MatchString(detail.Reason) {
				details.reason = detail.Reason
			}
		case "type.googleapis.com/google.rpc.RetryInfo":
			details.retryDelay = parseProtoDuration(detail.RetryDelay)
		}
	}
	return details
}

func isRetryableStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests ||
		(status >= 500 && status != http.StatusNotImplemented)
}

func statusFailureKind(status int, reason string) sdkgo.FailureKind {
	switch {
	case status == http.StatusUnauthorized || reason == "API_KEY_INVALID":
		return sdkgo.FailureAuthentication
	case status == http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case status == http.StatusNotFound:
		return sdkgo.FailureNotFound
	case status == http.StatusConflict:
		return sdkgo.FailureConflict
	case status == http.StatusTooManyRequests:
		return sdkgo.FailureRateLimit
	case status == http.StatusRequestTimeout || (status >= 500 && status != http.StatusNotImplemented):
		return sdkgo.FailureAvailability
	default:
		return sdkgo.FailureProviderRejection
	}
}

// parseProtoDuration parses a google.protobuf.Duration JSON value such as "37s".
func parseProtoDuration(value string) time.Duration {
	if !protoDurationPattern.MatchString(value) {
		return 0
	}
	delay, err := time.ParseDuration(value)
	if err != nil || delay <= 0 {
		return 0
	}
	return min(delay, maxProviderRetryDelay)
}

// retryAfterDelay parses a Retry-After header in delay-seconds or HTTP-date form.
func retryAfterDelay(header http.Header, now time.Time) time.Duration {
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds >= int64(maxProviderRetryDelay/time.Second) {
			return maxProviderRetryDelay
		}
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return min(retryAt.Sub(now), maxProviderRetryDelay)
	}
	return 0
}

func validateAndTrimEndpoint(value string) (string, error) {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return "", fmt.Errorf("Gemini endpoint must be an absolute URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return "", fmt.Errorf("Gemini endpoint cannot contain user info, a query, or a fragment")
	}
	switch endpoint.Scheme {
	case "https":
	case "http":
		if !isLoopback(endpoint.Hostname()) {
			return "", fmt.Errorf("Gemini endpoint must use HTTPS unless it is a loopback test server")
		}
	default:
		return "", fmt.Errorf("Gemini endpoint must use HTTPS")
	}
	return strings.TrimRight(endpoint.String(), "/"), nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// isValidAPIKey rejects values that could not travel safely in one header.
func isValidAPIKey(key string) bool {
	if key == "" {
		return false
	}
	for index := 0; index < len(key); index++ {
		if key[index] <= ' ' || key[index] > '~' {
			return false
		}
	}
	return true
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}

func failurePointer(kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := geminiFailure(kind, message)
	return &failure
}

func geminiFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func responseReceipt(call sdkgo.Call, requestID string, responseID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, Provider: providerName, ProviderObjectID: responseID,
		ProviderRequestID: requestID, ObservedAt: time.Now().UTC(),
	}
}
