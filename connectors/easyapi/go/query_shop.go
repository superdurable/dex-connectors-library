// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package easyapi

import (
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

// QueryShopInput identifies one authorized invoicing company.
type QueryShopInput struct {
	// TaxNumber is the company's taxpayer identification number, 15, 17, 18, 19, or 20 characters.
	TaxNumber string `json:"taxNumber"`
}

// ShopResult contains only non-sensitive invoicing service configuration.
type ShopResult struct {
	// ShopID is EasyAPI's company ID.
	ShopID int64 `json:"shopId"`
	// Name is the company's registered name.
	Name string `json:"name"`
	// APIState is 1 normal, 0 expired, or -1 not activated.
	APIState int `json:"apiState"`
	// EndTime is the service expiry time in provider local time, or blank if absent.
	EndTime string `json:"endTime"`
}

// QueryShopOperation reads one company's service configuration.
type QueryShopOperation struct{ client *Client }

// Definition returns the generated company Query contract.
func (QueryShopOperation) Definition() sdkgo.QueryDefinition { return QueryShopDefinition }

// Invoke reads one company and discards tax, banking, address, and staff fields.
func (operation QueryShopOperation) Invoke(call sdkgo.Call, input QueryShopInput) sdkgo.QueryAttempt[ShopResult] {
	if !validTaxNumber(input.TaxNumber) {
		return shopBranch(QueryShopBranchDefect, sdkgo.FailureValidation, "EasyAPI tax number is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validCredential(credentials.AppKey.Reveal()) || !validCredential(credentials.AppSecret.Reveal()) {
		return shopBranch(QueryShopBranchDefect, sdkgo.FailureAuthentication, "EasyAPI credentials are unavailable or invalid")
	}
	query := url.Values{"appKey": {credentials.AppKey.Reveal()}, "appSecret": {credentials.AppSecret.Reveal()}, "taxNumber": {input.TaxNumber}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/shop/query?"+query.Encode(), nil)
	if err != nil {
		return shopBranch(QueryShopBranchDefect, sdkgo.FailureValidation, "EasyAPI company request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "EasyAPI company request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return shopRetry(sdkgo.FailureRateLimit, "EasyAPI rate limited company query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return shopRetry(sdkgo.FailureAvailability, "EasyAPI company API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return shopBranch(QueryShopBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "EasyAPI response exceeded the configured limit")
	}
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "EasyAPI company response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return shopBranch(QueryShopBranchProviderRejected, classifyShopHTTP(response.StatusCode), "EasyAPI rejected company query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	result, code, err := decodeShop(body)
	if err != nil {
		return shopBranch(QueryShopBranchInvalidResponse, sdkgo.FailureProtocol, "EasyAPI returned an invalid company response")
	}
	if code != 1 {
		return shopBranch(QueryShopBranchProviderRejected, sdkgo.FailureProviderRejection, "EasyAPI rejected company query")
	}
	return sdkgo.NewQueryBranch(QueryShopBranchFound, result, nil, sdkgo.Receipt{})
}

func decodeShop(body []byte) (ShopResult, int, error) {
	var envelope struct {
		Code    *int `json:"code"`
		Content *struct {
			ShopID   int64   `json:"shopId"`
			Name     string  `json:"name"`
			APIState *int    `json:"apiState"`
			EndTime  *string `json:"endTime"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code == nil {
		return ShopResult{}, 0, errors.New("invalid EasyAPI response")
	}
	if *envelope.Code != 1 {
		return ShopResult{}, *envelope.Code, nil
	}
	if envelope.Content == nil || envelope.Content.ShopID <= 0 || envelope.Content.Name == "" || envelope.Content.APIState == nil || len(envelope.Content.Name) > 256 {
		return ShopResult{}, 0, errors.New("invalid EasyAPI company")
	}
	switch *envelope.Content.APIState {
	case -1, 0, 1:
	default:
		return ShopResult{}, 0, errors.New("unknown EasyAPI API state")
	}
	result := ShopResult{ShopID: envelope.Content.ShopID, Name: envelope.Content.Name, APIState: *envelope.Content.APIState}
	if envelope.Content.EndTime != nil {
		if len(*envelope.Content.EndTime) > 32 {
			return ShopResult{}, 0, errors.New("invalid EasyAPI expiry")
		}
		result.EndTime = *envelope.Content.EndTime
	}
	return result, *envelope.Code, nil
}
func validTaxNumber(value string) bool {
	switch len(value) {
	case 15, 17, 18, 19, 20:
	default:
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}
func validCredential(value string) bool {
	return value != "" && len(value) <= 2048 && strings.TrimSpace(value) == value
}
func classifyShopHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func shopBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ShopResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "easyapi", Operation: "queryShop", Message: message}
	return sdkgo.NewQueryBranch(branch, ShopResult{}, &failure, sdkgo.Receipt{})
}
func shopRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ShopResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "easyapi", Operation: "queryShop", Message: message}
	return sdkgo.NewQueryRetry[ShopResult](failure, delay)
}
