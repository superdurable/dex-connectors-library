// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiankechat

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListKnowledgeEntriesInput selects one knowledge page. Zero page and limit use 1 and 20.
type ListKnowledgeEntriesInput struct {
	// Page is the one-based page number; zero selects the first page.
	Page int `json:"page"`
	// Limit is the number of entries to request, from 1 to 100; zero uses 20.
	Limit int `json:"limit"`
}

// KnowledgeEntry contains metadata only, excluding questions, answers, and attachments.
type KnowledgeEntry struct {
	// ID is the provider's knowledge entry identifier.
	ID int64 `json:"id"`
	// Category is the entry category.
	Category string `json:"category"`
	// Active reports whether the entry is enabled.
	Active bool `json:"active"`
	// HitCount is the accumulated number of matches.
	HitCount int64 `json:"hitCount"`
}

// ListKnowledgeEntriesOutput contains one knowledge page and pagination counts.
type ListKnowledgeEntriesOutput struct {
	// Entries are the returned knowledge metadata in provider order.
	Entries []KnowledgeEntry `json:"entries"`
	// Total is the provider's total entry count.
	Total int `json:"total"`
	// Page is the returned one-based page number.
	Page int `json:"page"`
	// Limit is the returned page size.
	Limit int `json:"limit"`
}

// ListKnowledgeEntriesOperation reads a TiankeChat knowledge page.
type ListKnowledgeEntriesOperation struct{ client *Client }

// Definition returns the generated knowledge-list contract.
func (ListKnowledgeEntriesOperation) Definition() sdkgo.QueryDefinition {
	return ListKnowledgeEntriesDefinition
}

// Invoke reads one knowledge page with a v3 HMAC signature.
func (operation ListKnowledgeEntriesOperation) Invoke(call sdkgo.Call, input ListKnowledgeEntriesInput) sdkgo.QueryAttempt[ListKnowledgeEntriesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppID(credentials.AppID) || !isValidAppSecret(credentials.AppSecret.Reveal()) {
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchDefect, sdkgo.FailureAuthentication, "application credentials are unavailable or invalid")
	}
	page, limit := input.Page, input.Limit
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = 20
	}
	if page < 1 || page > 1000000 || limit < 1 || limit > 100 {
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchDefect, sdkgo.FailureValidation, "page or limit is outside the supported range")
	}
	parameters := url.Values{}
	parameters.Set("page", strconv.Itoa(page))
	parameters.Set("limit", strconv.Itoa(limit))
	path := "/api/openapi/knowledge?" + parameters.Encode()
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return knowledgeCatalogRetry(sdkgo.FailureAvailability, "secure nonce generation failed", 0)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := hex.EncodeToString(nonceBytes)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+path, nil)
	if err != nil {
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchDefect, sdkgo.FailureValidation, "knowledge-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-App-Id", credentials.AppID)
	request.Header.Set("X-Timestamp", timestamp)
	request.Header.Set("X-Nonce", nonce)
	request.Header.Set("X-Signature", signTiankeRequest(credentials.AppSecret.Reveal(), timestamp, nonce, http.MethodGet, path, ""))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return knowledgeCatalogRetry(sdkgo.FailureAvailability, "TiankeChat request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return knowledgeCatalogRetry(sdkgo.FailureRateLimit, "TiankeChat rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return knowledgeCatalogRetry(sdkgo.FailureAvailability, "TiankeChat is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TiankeChat response exceeded the configured limit")
	}
	if err != nil {
		return knowledgeCatalogRetry(sdkgo.FailureAvailability, "TiankeChat response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchProviderRejected, kind, "TiankeChat rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		OK    *bool `json:"ok"`
		Total *int  `json:"total"`
		Page  *int  `json:"page"`
		Limit *int  `json:"limit"`
		Items []struct {
			ID       *int64 `json:"id"`
			Category string `json:"category"`
			Active   *bool  `json:"active"`
			HitCount *int64 `json:"hit_count"`
		} `json:"items"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.OK == nil {
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchInvalidResponse, sdkgo.FailureProtocol, "TiankeChat returned invalid knowledge-list data")
	}
	if !*wire.OK {
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchProviderRejected, sdkgo.FailureProviderRejection, "TiankeChat rejected the knowledge-list request")
	}
	if wire.Total == nil || wire.Page == nil || wire.Limit == nil || *wire.Total < 0 || *wire.Page < 1 || *wire.Limit < 1 || wire.Items == nil || len(wire.Items) > limit {
		return knowledgeCatalogBranch(ListKnowledgeEntriesBranchInvalidResponse, sdkgo.FailureProtocol, "TiankeChat returned invalid knowledge-list data")
	}
	entries := make([]KnowledgeEntry, 0, len(wire.Items))
	for _, item := range wire.Items {
		if item.ID == nil || *item.ID <= 0 || item.Active == nil || item.HitCount == nil || *item.HitCount < 0 || len(item.Category) > 256 {
			return knowledgeCatalogBranch(ListKnowledgeEntriesBranchInvalidResponse, sdkgo.FailureProtocol, "TiankeChat returned invalid knowledge metadata")
		}
		entries = append(entries, KnowledgeEntry{ID: *item.ID, Category: item.Category, Active: *item.Active, HitCount: *item.HitCount})
	}
	return sdkgo.NewQueryBranch(ListKnowledgeEntriesBranchListed, ListKnowledgeEntriesOutput{Entries: entries, Total: *wire.Total, Page: *wire.Page, Limit: *wire.Limit}, nil, sdkgo.Receipt{})
}

func signTiankeRequest(secret, timestamp, nonce, method, path, rawBody string) string {
	signatureText := strings.Join([]string{timestamp, nonce, method, path, rawBody}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signatureText))
	return hex.EncodeToString(mac.Sum(nil))
}
func isValidAppID(value string) bool {
	if !(strings.HasPrefix(value, "ap_") || strings.HasPrefix(value, "sb_")) || len(value) > 128 {
		return false
	}
	return providerhttp.IsHeaderSafeCredential(value)
}
func isValidAppSecret(value string) bool {
	return strings.HasPrefix(value, "tks_") && len(value) <= 512 && providerhttp.IsHeaderSafeCredential(value)
}
func knowledgeCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListKnowledgeEntriesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tiankechat", Operation: "listKnowledgeEntries", Message: message}
	return sdkgo.NewQueryBranch(branch, ListKnowledgeEntriesOutput{}, &failure, sdkgo.Receipt{})
}
func knowledgeCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListKnowledgeEntriesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tiankechat", Operation: "listKnowledgeEntries", Message: message}
	return sdkgo.NewQueryRetry[ListKnowledgeEntriesOutput](failure, delay)
}
