// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jisuapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListProvincesInput needs no selection because the provider returns the province catalog.
type ListProvincesInput struct{}

// Province identifies one administrative province without additional provider data.
type Province struct {
	// ID is the provider's province identifier.
	ID string `json:"id"`
	// Name is the province display name.
	Name string `json:"name"`
}

// ListProvincesOutput contains only province IDs and names.
type ListProvincesOutput struct {
	// Provinces are the provinces in the provider response.
	Provinces []Province `json:"provinces"`
}

// ListProvincesOperation reads province metadata without modifying provider state.
type ListProvincesOperation struct{ client *Client }

// Definition returns the generated province-list contract.
func (ListProvincesOperation) Definition() sdkgo.QueryDefinition { return ListProvincesDefinition }

// Invoke queries the province catalog and keeps the query-string appkey out of failures.
func (operation ListProvincesOperation) Invoke(call sdkgo.Call, _ ListProvincesInput) sdkgo.QueryAttempt[ListProvincesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppKey(credentials.AppKey.Reveal()) {
		return provincesBranch(ListProvincesBranchDefect, sdkgo.FailureAuthentication, "JisuAPI appkey is unavailable or invalid")
	}
	endpoint := provincesEndpoint + "?appkey=" + url.QueryEscape(credentials.AppKey.Reveal())
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return provincesBranch(ListProvincesBranchDefect, sdkgo.FailureValidation, "JisuAPI province-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return provincesRetry(sdkgo.FailureAvailability, "JisuAPI province-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return provincesRetry(sdkgo.FailureRateLimit, "JisuAPI rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return provincesRetry(sdkgo.FailureAvailability, "JisuAPI province list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyProvincesHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return provincesBranch(ListProvincesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "JisuAPI response exceeded the configured limit")
	}
	if err != nil {
		return provincesRetry(sdkgo.FailureAvailability, "JisuAPI response was interrupted", 0)
	}
	var wire struct {
		Status *int `json:"status"`
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Status == nil {
		return provincesBranch(ListProvincesBranchInvalidResponse, sdkgo.FailureProtocol, "JisuAPI returned invalid province data")
	}
	switch *wire.Status {
	case 0:
	case 104, 106:
		return provincesRetry(sdkgo.FailureRateLimit, "JisuAPI daily or IP request limit was reached", 0)
	case 107:
		return provincesRetry(sdkgo.FailureAvailability, "JisuAPI is under maintenance", 0)
	default:
		return provincesBranch(ListProvincesBranchProviderRejected, sdkgo.FailureProviderRejection, "JisuAPI rejected the province-list request with code "+strconv.Itoa(*wire.Status))
	}
	if wire.Result == nil {
		return provincesBranch(ListProvincesBranchInvalidResponse, sdkgo.FailureProtocol, "JisuAPI returned incomplete province data")
	}
	output := ListProvincesOutput{Provinces: make([]Province, 0, len(wire.Result))}
	for _, province := range wire.Result {
		if province.ID == "" || province.Name == "" {
			return provincesBranch(ListProvincesBranchInvalidResponse, sdkgo.FailureProtocol, "JisuAPI returned an incomplete province")
		}
		output.Provinces = append(output.Provinces, Province{ID: province.ID, Name: province.Name})
	}
	return sdkgo.NewQueryBranch(ListProvincesBranchListed, output, nil, sdkgo.Receipt{})
}

func classifyProvincesHTTPStatus(status int) sdkgo.QueryAttempt[ListProvincesOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return provincesBranch(ListProvincesBranchProviderRejected, kind, "JisuAPI rejected the province-list request with HTTP "+strconv.Itoa(status))
}
func provincesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListProvincesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jisuapi", Operation: "listProvinces", Message: message}
	return sdkgo.NewQueryBranch(branch, ListProvincesOutput{}, &failure, sdkgo.Receipt{})
}
func provincesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListProvincesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jisuapi", Operation: "listProvinces", Message: message}
	return sdkgo.NewQueryRetry[ListProvincesOutput](failure, delay)
}
