// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zhihu

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListQuotasInput selects all API quotas visible to the Access Secret.
type ListQuotasInput struct{}

// Quota describes one enabled Zhihu API's allocation and usage.
type Quota struct {
	// APIID is the provider's API category identifier.
	APIID string `json:"apiId"`
	// APIName is the provider's category display name.
	APIName string `json:"apiName"`
	// TotalQuota is the allocated request count for the current natural day.
	TotalQuota int64 `json:"totalQuota"`
	// TotalUsed is the consumed request count for the current natural day.
	TotalUsed int64 `json:"totalUsed"`
	// RemainingQuota is the provider's nonnegative remaining count for the current natural day.
	RemainingQuota int64 `json:"remainingQuota"`
}

// QuotaPage contains account quota metadata and no customer content.
type QuotaPage struct {
	// Quotas is empty when the account has no visible API allocation.
	Quotas []Quota `json:"quotas"`
}

// ListQuotasOperation reads current account quotas.
type ListQuotasOperation struct{ client *Client }

// Definition returns the generated list-quotas contract.
func (ListQuotasOperation) Definition() sdkgo.QueryDefinition { return ListQuotasDefinition }

// Invoke queries Zhihu and classifies provider, protocol, and transport outcomes.
func (operation ListQuotasOperation) Invoke(call sdkgo.Call, _ ListQuotasInput) sdkgo.QueryAttempt[QuotaPage] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAccessSecret(credentials.AccessSecret.Reveal()) {
		return quotaBranch(ListQuotasBranchDefect, sdkgo.FailureAuthentication, "Zhihu connection secret is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/api/v1/quota", nil)
	if err != nil {
		return quotaBranch(ListQuotasBranchDefect, sdkgo.FailureValidation, "quota request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessSecret.Reveal())
	request.Header.Set("X-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return quotaRetry(sdkgo.FailureAvailability, "Zhihu request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return quotaRetry(sdkgo.FailureRateLimit, "Zhihu rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return quotaRetry(sdkgo.FailureAvailability, "Zhihu is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return quotaBranch(ListQuotasBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Zhihu response exceeded the configured limit")
	}
	if err != nil {
		return quotaRetry(sdkgo.FailureAvailability, "Zhihu response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return quotaBranch(ListQuotasBranchProviderRejected, classifyQuotaHTTP(response.StatusCode), "Zhihu rejected the query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, code, err := decodeQuotaPage(body)
	if err != nil {
		return quotaBranch(ListQuotasBranchInvalidResponse, sdkgo.FailureProtocol, "Zhihu returned an invalid quota response")
	}
	switch code {
	case 0:
		return sdkgo.NewQueryBranch(ListQuotasBranchListed, page, nil, sdkgo.Receipt{})
	case 30001:
		return quotaRetry(sdkgo.FailureRateLimit, "Zhihu rate limited the request", retryAfter)
	case 90001:
		return quotaRetry(sdkgo.FailureAvailability, "Zhihu quota data is temporarily unavailable", retryAfter)
	case 20001:
		return quotaBranch(ListQuotasBranchProviderRejected, sdkgo.FailureAuthentication, "Zhihu rejected the Access Secret")
	case 10001:
		return quotaBranch(ListQuotasBranchProviderRejected, sdkgo.FailureValidation, "Zhihu rejected the quota request")
	default:
		return quotaBranch(ListQuotasBranchProviderRejected, sdkgo.FailureProviderRejection, "Zhihu rejected the query with code "+strconv.Itoa(code))
	}
}

func decodeQuotaPage(body []byte) (QuotaPage, int, error) {
	var wire struct {
		Code *int `json:"Code"`
		Data []struct {
			APIID          string `json:"APIID"`
			APIName        string `json:"APIName"`
			TotalQuota     *int64 `json:"TotalQuota"`
			TotalUsed      *int64 `json:"TotalUsed"`
			RemainingQuota *int64 `json:"RemainingQuota"`
		} `json:"Data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return QuotaPage{}, 0, errors.New("invalid Zhihu JSON")
	}
	if *wire.Code != 0 {
		return QuotaPage{}, *wire.Code, nil
	}
	if wire.Data == nil || len(wire.Data) > 100 {
		return QuotaPage{}, 0, errors.New("invalid quota list")
	}
	page := QuotaPage{Quotas: make([]Quota, 0, len(wire.Data))}
	for _, item := range wire.Data {
		if item.APIID == "" || item.APIName == "" || item.TotalQuota == nil || item.TotalUsed == nil || item.RemainingQuota == nil || *item.TotalQuota < 0 || *item.TotalUsed < 0 || *item.RemainingQuota < 0 {
			return QuotaPage{}, 0, errors.New("invalid quota entry")
		}
		page.Quotas = append(page.Quotas, Quota{APIID: item.APIID, APIName: item.APIName, TotalQuota: *item.TotalQuota, TotalUsed: *item.TotalUsed, RemainingQuota: *item.RemainingQuota})
	}
	return page, 0, nil
}
func validAccessSecret(secret string) bool {
	if secret == "" || len(secret) > 2048 || strings.TrimSpace(secret) != secret || strings.HasPrefix(strings.ToLower(secret), "bearer ") {
		return false
	}
	for _, character := range secret {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyQuotaHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func quotaBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[QuotaPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zhihu", Operation: "listQuotas", Message: message}
	return sdkgo.NewQueryBranch(branch, QuotaPage{}, &failure, sdkgo.Receipt{})
}
func quotaRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[QuotaPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zhihu", Operation: "listQuotas", Message: message}
	return sdkgo.NewQueryRetry[QuotaPage](failure, delay)
}
