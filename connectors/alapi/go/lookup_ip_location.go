// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package alapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
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
	// Country is the country name when returned by ALAPI.
	Country string `json:"country"`
	// Province is the province name.
	Province string `json:"province"`
	// City is the city name.
	City string `json:"city"`
	// District is the district name.
	District string `json:"district"`
	// Operator is the network operator name.
	Operator string `json:"operator"`
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
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "ALAPI requires one public IPv4 address")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredentialValue(credentials.Token.Reveal()) {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureAuthentication, "ALAPI credentials are unavailable or invalid")
	}
	requestBody, err := json.Marshal(struct {
		IP string `json:"ip"`
	}{IP: parsed.To4().String()})
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "ALAPI lookup request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, locationEndpoint, strings.NewReader(string(requestBody)))
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "ALAPI lookup request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("token", credentials.Token.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "ALAPI lookup request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return locationRetry(sdkgo.FailureRateLimit, "ALAPI rate limited the lookup", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return locationRetry(sdkgo.FailureAvailability, "ALAPI lookup service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyLocationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ALAPI response exceeded the configured limit")
	}
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "ALAPI response was interrupted", 0)
	}
	var wire struct {
		Success *bool `json:"success"`
		Code    *int  `json:"code"`
		Data    *struct {
			Country  string `json:"country"`
			Province string `json:"province"`
			City     string `json:"city"`
			District string `json:"district"`
			ISP      string `json:"isp"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil || wire.Success == nil {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "ALAPI returned invalid lookup data")
	}
	if *wire.Code == 429 {
		return locationRetry(sdkgo.FailureRateLimit, "ALAPI rate limited the lookup", retryAfter)
	}
	if *wire.Code != 200 || !*wire.Success {
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 10001 || *wire.Code == 10002 || *wire.Code == 10008 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.Code == 10004 || *wire.Code == 10009 {
			kind = sdkgo.FailureAuthorization
		}
		if *wire.Code == 10005 || *wire.Code == 10010 {
			kind = sdkgo.FailureQuotaExhausted
		}
		return locationBranch(LookupIPLocationBranchProviderRejected, kind, "ALAPI rejected lookup with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || strings.TrimSpace(wire.Data.Country) == "" || strings.TrimSpace(wire.Data.City) == "" {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "ALAPI returned incomplete location data")
	}
	location := wire.Data
	output := LookupIPLocationOutput{Country: location.Country, Province: location.Province, City: location.City, District: location.District, Operator: location.ISP}
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
	return locationBranch(LookupIPLocationBranchProviderRejected, kind, "ALAPI rejected lookup with HTTP "+strconv.Itoa(status))
}
func locationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "alapi", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryBranch(branch, LookupIPLocationOutput{}, &failure, sdkgo.Receipt{})
}
func locationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "alapi", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryRetry[LookupIPLocationOutput](failure, delay)
}
