// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package cohere provides Embed and Rerank Query Steps for Cohere v2.
package cohere

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// EmbedInput asks Cohere to embed up to 96 texts as float vectors.
type EmbedInput struct {
	// Model is a Cohere Embed model ID.
	Model string `json:"model"`
	// Texts are text inputs; the response has one vector per text.
	Texts []string `json:"texts"`
	// InputType is search_document, search_query, classification, or clustering.
	InputType string `json:"input_type"`
}

// EmbedOutput contains float embeddings in input order.
type EmbedOutput struct {
	// ID is Cohere's embedding response ID.
	ID string `json:"id"`
	// Embeddings contains one float vector per text.
	Embeddings [][]float64 `json:"embeddings"`
}

// RerankInput asks Cohere to rank documents for a text query.
type RerankInput struct {
	// Model is a Cohere Rerank model ID.
	Model string `json:"model"`
	// Query is the relevance question.
	Query string `json:"query"`
	// Documents are text passages to rank, in original order.
	Documents []string `json:"documents"`
	// TopN limits returned results; zero returns all.
	TopN int `json:"top_n,omitempty"`
}

// RankedDocument identifies one input document and its relevance score.
type RankedDocument struct {
	// Index is the document's zero-based position in the input.
	Index int `json:"index"`
	// RelevanceScore is Cohere's score for that document.
	RelevanceScore float64 `json:"relevanceScore"`
}

// RerankOutput contains document indices sorted by relevance.
type RerankOutput struct {
	// Results are the ranked document indices and scores.
	Results []RankedDocument `json:"results"`
}

// New validates configuration without contacting Cohere.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Cohere credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Cohere base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// Embed returns the bound embedding Query.
func (client *Client) Embed() EmbedOperation { return EmbedOperation{client} }

// EmbedOperation performs one Cohere v2 embedding request.
type EmbedOperation struct{ client *Client }

// Definition returns the manifest-generated embedding contract.
func (EmbedOperation) Definition() sdkgo.QueryDefinition { return EmbedDefinition }

// Invoke validates text and requests float embeddings.
func (operation EmbedOperation) Invoke(call sdkgo.Call, input EmbedInput) sdkgo.QueryAttempt[EmbedOutput] {
	if strings.TrimSpace(input.Model) == "" || len(input.Texts) == 0 || len(input.Texts) > 96 {
		return failedBranch[EmbedOutput]("embed", EmbedBranchDefect, sdkgo.FailureValidation, "model and 1 to 96 texts are required")
	}
	for _, text := range input.Texts {
		if strings.TrimSpace(text) == "" {
			return failedBranch[EmbedOutput]("embed", EmbedBranchDefect, sdkgo.FailureValidation, "texts must be nonempty")
		}
	}
	switch input.InputType {
	case "search_document", "search_query", "classification", "clustering":
	default:
		return failedBranch[EmbedOutput]("embed", EmbedBranchDefect, sdkgo.FailureValidation, "unsupported input_type")
	}
	payload := struct {
		Model          string   `json:"model"`
		Texts          []string `json:"texts"`
		InputType      string   `json:"input_type"`
		EmbeddingTypes []string `json:"embedding_types"`
	}{input.Model, input.Texts, input.InputType, []string{"float"}}
	return invokeJSON(operation.client, call, "embed", "/v2/embed", EmbedBranchEmbedded, payload, func(body []byte) (EmbedOutput, error) { return decodeEmbed(body, len(input.Texts)) })
}
func decodeEmbed(body []byte, expected int) (EmbedOutput, error) {
	var wire struct {
		ID         string `json:"id"`
		Embeddings struct {
			Float [][]float64 `json:"float"`
		} `json:"embeddings"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EmbedOutput{}, err
	}
	if wire.ID == "" || len(wire.Embeddings.Float) != expected {
		return EmbedOutput{}, fmt.Errorf("incomplete embeddings")
	}
	for _, vector := range wire.Embeddings.Float {
		if len(vector) == 0 {
			return EmbedOutput{}, fmt.Errorf("empty embedding")
		}
	}
	return EmbedOutput{ID: wire.ID, Embeddings: wire.Embeddings.Float}, nil
}

// Rerank returns the bound reranking Query.
func (client *Client) Rerank() RerankOperation { return RerankOperation{client} }

// RerankOperation performs one Cohere v2 reranking request.
type RerankOperation struct{ client *Client }

// Definition returns the manifest-generated rerank contract.
func (RerankOperation) Definition() sdkgo.QueryDefinition { return RerankDefinition }

// Invoke validates documents and requests relevance scores.
func (operation RerankOperation) Invoke(call sdkgo.Call, input RerankInput) sdkgo.QueryAttempt[RerankOutput] {
	if strings.TrimSpace(input.Model) == "" || strings.TrimSpace(input.Query) == "" || len(input.Documents) == 0 || len(input.Documents) > 1000 || input.TopN < 0 || input.TopN > len(input.Documents) {
		return failedBranch[RerankOutput]("rerank", RerankBranchDefect, sdkgo.FailureValidation, "model, query, 1 to 1000 documents, and valid topN are required")
	}
	for _, document := range input.Documents {
		if strings.TrimSpace(document) == "" {
			return failedBranch[RerankOutput]("rerank", RerankBranchDefect, sdkgo.FailureValidation, "documents must be nonempty")
		}
	}
	return invokeJSON(operation.client, call, "rerank", "/v2/rerank", RerankBranchRanked, input, func(body []byte) (RerankOutput, error) { return decodeRerank(body, len(input.Documents)) })
}
func decodeRerank(body []byte, documentCount int) (RerankOutput, error) {
	var wire struct {
		Results []struct {
			Index int     `json:"index"`
			Score float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return RerankOutput{}, err
	}
	if wire.Results == nil {
		return RerankOutput{}, fmt.Errorf("missing ranking")
	}
	output := RerankOutput{Results: make([]RankedDocument, 0, len(wire.Results))}
	seen := make(map[int]bool, len(wire.Results))
	for _, result := range wire.Results {
		if result.Index < 0 || result.Index >= documentCount || seen[result.Index] {
			return RerankOutput{}, fmt.Errorf("invalid ranking index")
		}
		seen[result.Index] = true
		output.Results = append(output.Results, RankedDocument{Index: result.Index, RelevanceScore: result.Score})
	}
	return output, nil
}

func invokeJSON[T any](client *Client, call sdkgo.Call, operationID, path string, successBranch sdkgo.BranchID, input any, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureValidation, "input cannot be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureLocalDefect, "request cannot be built")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "Cohere request failed", 0)
	}
	// Closing a query response cannot change its outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		switch {
		case response.StatusCode == 429:
			return retryBranch[T](operationID, sdkgo.FailureRateLimit, "Cohere rate limit", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == 408 || response.StatusCode == 409 || response.StatusCode >= 500:
			return retryBranch[T](operationID, sdkgo.FailureAvailability, "Cohere temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == 401 || response.StatusCode == 498:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthentication, "Cohere rejected the API key")
		case response.StatusCode == 403:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthorization, "Cohere denied the request")
		default:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureProviderRejection, "Cohere rejected HTTP "+strconv.Itoa(response.StatusCode))
		}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureResponseTooLarge, "Cohere response exceeded configured limit")
	}
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "Cohere response interrupted", 0)
	}
	value, err := decode(body)
	if err != nil {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureProtocol, "Cohere returned unusable JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}
func failedBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "cohere", Operation: operationID, Message: message}
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func retryBranch[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "cohere", Operation: operationID, Message: message}
	return sdkgo.NewQueryRetry[T](failure, after)
}
