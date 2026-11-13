// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shoplazza

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

// GetShopInput identifies the configured store; it needs no additional fields.
type GetShopInput struct{}

// GetShopOutput contains non-contact store metadata only.
type GetShopOutput struct {
	// ID is Shoplazza's stable numeric store identifier.
	ID int64 `json:"id"`
	// Name is the store's display name.
	Name string `json:"name"`
	// Domain is the store's public domain.
	Domain string `json:"domain"`
	// Currency is the store's ISO currency code.
	Currency string `json:"currency"`
}

// GetShopOperation reads identity metadata for the connected store.
type GetShopOperation struct{ client *Client }

// Definition returns the generated shop Query contract.
func (GetShopOperation) Definition() sdkgo.QueryDefinition { return GetShopDefinition }

// Invoke makes one safe, repeatable shop metadata read.
func (operation GetShopOperation) Invoke(call sdkgo.Call, _ GetShopInput) sdkgo.QueryAttempt[GetShopOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || validateAccessToken(credentials.AccessToken.Reveal()) != nil {
		return shopBranch(GetShopBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	query := url.Values{"fields": {"id", "name", "domain", "currency"}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.shopURL+"/openapi/2027-01/shop?"+query.Encode(), nil)
	if err != nil {
		return shopBranch(GetShopBranchDefect, sdkgo.FailureValidation, "shop request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Access-Token", credentials.AccessToken.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "Shoplazza request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return shopRetry(sdkgo.FailureRateLimit, "Shoplazza rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return shopRetry(sdkgo.FailureAvailability, "Shoplazza is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return shopBranch(GetShopBranchProviderRejected, sdkgo.FailureProviderRejection, "Shoplazza rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return shopBranch(GetShopBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Shoplazza response exceeded configured limit")
	}
	if err != nil {
		return shopRetry(sdkgo.FailureAvailability, "Shoplazza response was interrupted", 0)
	}
	output, err := decodeShopResponse(body)
	if err != nil {
		return shopBranch(GetShopBranchInvalidResponse, sdkgo.FailureProtocol, "Shoplazza returned an invalid response")
	}
	return sdkgo.NewQueryBranch(GetShopBranchFound, output, nil, sdkgo.Receipt{})
}

func decodeShopResponse(body []byte) (GetShopOutput, error) {
	var envelope struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return GetShopOutput{}, errors.New("invalid Shoplazza envelope")
	}
	var output GetShopOutput
	if err := json.Unmarshal(envelope.Data, &output); err != nil || output.ID <= 0 || strings.TrimSpace(output.Name) == "" || strings.TrimSpace(output.Domain) == "" || len(output.Currency) != 3 {
		return GetShopOutput{}, errors.New("invalid Shoplazza shop data")
	}
	return output, nil
}

func shopBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetShopOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shoplazza", Operation: "getShop", Message: message}
	return sdkgo.NewQueryBranch(branch, GetShopOutput{}, &failure, sdkgo.Receipt{})
}
func shopRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetShopOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shoplazza", Operation: "getShop", Message: message}
	return sdkgo.NewQueryRetry[GetShopOutput](failure, delay)
}
