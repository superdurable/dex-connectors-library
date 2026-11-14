// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ipip

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// LookupIPLocationInput identifies one IPv4 or IPv6 address.
type LookupIPLocationInput struct {
	// IPAddress is a valid IPv4 or IPv6 address, not a hostname or CIDR range.
	IPAddress string `json:"ipAddress"`
}

// LookupIPLocationOutput contains only the API's default coarse location fields.
type LookupIPLocationOutput struct {
	// CountryName is the location country name.
	CountryName string `json:"countryName"`
	// RegionName is the province or state name.
	RegionName string `json:"regionName"`
	// CityName is the city name.
	CityName string `json:"cityName"`
	// CountryCode is the two-letter country code.
	CountryCode string `json:"countryCode"`
}

// LookupIPLocationOperation reads location metadata without changing provider state.
type LookupIPLocationOperation struct{ client *Client }

// Definition returns the generated location-lookup contract.
func (LookupIPLocationOperation) Definition() sdkgo.QueryDefinition {
	return LookupIPLocationDefinition
}

// Invoke queries one validated IP and excludes the query-string token from failures.
func (operation LookupIPLocationOperation) Invoke(call sdkgo.Call, input LookupIPLocationInput) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	parsed := net.ParseIP(input.IPAddress)
	if parsed == nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "IPIP requires one valid IP address")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidToken(credentials.Token.Reveal()) {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureAuthentication, "IPIP token is unavailable or invalid")
	}
	// IPIP v2 query requires a token query parameter: https://www.ipip.net/document/api_v2.html
	endpoint := locationEndpoint + url.PathEscape(parsed.String()) + "?token=" + url.QueryEscape(credentials.Token.Reveal())
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "IPIP location request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "IPIP location request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return locationRetry(sdkgo.FailureRateLimit, "IPIP rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return locationRetry(sdkgo.FailureAvailability, "IPIP location service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyLocationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "IPIP response exceeded the configured limit")
	}
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "IPIP response was interrupted", 0)
	}
	var wire struct {
		Ret  string `json:"ret"`
		Data struct {
			Info struct {
				CountryName *string `json:"country_name"`
				RegionName  *string `json:"region_name"`
				CityName    *string `json:"city_name"`
				CountryCode *string `json:"country_code"`
			} `json:"info"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Ret == "" {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "IPIP returned invalid location data")
	}
	if wire.Ret == "err" {
		return locationBranch(LookupIPLocationBranchProviderRejected, sdkgo.FailureProviderRejection, "IPIP rejected the location request")
	}
	if wire.Ret != "ok" || wire.Data.Info.CountryName == nil || wire.Data.Info.RegionName == nil || wire.Data.Info.CityName == nil || wire.Data.Info.CountryCode == nil || *wire.Data.Info.CountryName == "" || *wire.Data.Info.CountryCode == "" {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "IPIP returned incomplete location data")
	}
	info := wire.Data.Info
	return sdkgo.NewQueryBranch(LookupIPLocationBranchFound, LookupIPLocationOutput{CountryName: *info.CountryName, RegionName: *info.RegionName, CityName: *info.CityName, CountryCode: *info.CountryCode}, nil, sdkgo.Receipt{})
}
func classifyLocationHTTPStatus(status int) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return locationBranch(LookupIPLocationBranchProviderRejected, kind, "IPIP rejected location request with HTTP "+strconv.Itoa(status))
}
func locationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ipip", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryBranch(branch, LookupIPLocationOutput{}, &failure, sdkgo.Receipt{})
}
func locationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ipip", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryRetry[LookupIPLocationOutput](failure, delay)
}
