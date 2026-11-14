// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yikeapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// LookupIPLocationInput identifies one public IPv4 address.
type LookupIPLocationInput struct {
	// IPAddress is an IPv4 address, not a hostname or CIDR range.
	IPAddress string `json:"ipAddress"`
}

// LookupIPLocationOutput contains only coarse location and network fields.
type LookupIPLocationOutput struct {
	// Country is the country name when returned by YIKEAPI.
	Country string `json:"country"`
	// Province is the province name.
	Province string `json:"province"`
	// City is the city name.
	City string `json:"city"`
	// District is the district name.
	District string `json:"district"`
}

// LookupIPLocationOperation reads an IPv4 location without changing provider state.
type LookupIPLocationOperation struct{ client *Client }

// Definition returns the generated IP lookup contract.
func (LookupIPLocationOperation) Definition() sdkgo.QueryDefinition {
	return LookupIPLocationDefinition
}

// Invoke sends an HTTPS query and excludes credentials and raw provider errors from the result.
func (operation LookupIPLocationOperation) Invoke(call sdkgo.Call, input LookupIPLocationInput) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	parsed := net.ParseIP(input.IPAddress)
	if parsed == nil || parsed.To4() == nil || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() || parsed.IsMulticast() {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "YIKEAPI requires one public IPv4 address")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredentialValue(credentials.AppID.Reveal()) || !isValidCredentialValue(credentials.AppSecret.Reveal()) {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureAuthentication, "YIKEAPI credentials are unavailable or invalid")
	}
	requestURL, err := url.Parse(locationEndpoint)
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "YIKEAPI lookup request is invalid")
	}
	query := requestURL.Query()
	query.Set("appid", credentials.AppID.Reveal())
	query.Set("appsecret", credentials.AppSecret.Reveal())
	query.Set("ip", parsed.To4().String())
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "YIKEAPI lookup request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "YIKEAPI lookup request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return locationRetry(sdkgo.FailureRateLimit, "YIKEAPI rate limited the lookup", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return locationRetry(sdkgo.FailureAvailability, "YIKEAPI lookup service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyLocationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "YIKEAPI response exceeded the configured limit")
	}
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "YIKEAPI response was interrupted", 0)
	}
	var wire struct {
		ErrorCode *int   `json:"errcode"`
		IP        string `json:"ip"`
		Country   string `json:"country"`
		Province  string `json:"province"`
		City      string `json:"city"`
		District  string `json:"district"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.ErrorCode == nil {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "YikeAPI returned invalid lookup data")
	}
	if *wire.ErrorCode != 0 {
		return locationBranch(LookupIPLocationBranchProviderRejected, sdkgo.FailureProviderRejection, "YikeAPI rejected lookup with code "+strconv.Itoa(*wire.ErrorCode))
	}
	if wire.IP != parsed.To4().String() || strings.TrimSpace(wire.Country) == "" || strings.TrimSpace(wire.City) == "" {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "YikeAPI returned incomplete location data")
	}
	output := LookupIPLocationOutput{Country: wire.Country, Province: wire.Province, City: wire.City, District: wire.District}
	return sdkgo.NewQueryBranch(LookupIPLocationBranchFound, output, nil, sdkgo.Receipt{})
}

func classifyLocationHTTPStatus(status int) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return locationBranch(LookupIPLocationBranchProviderRejected, kind, "YIKEAPI rejected lookup with HTTP "+strconv.Itoa(status))
}
func locationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yikeapi", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryBranch(branch, LookupIPLocationOutput{}, &failure, sdkgo.Receipt{})
}
func locationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yikeapi", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryRetry[LookupIPLocationOutput](failure, delay)
}
