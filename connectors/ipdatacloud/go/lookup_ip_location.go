// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ipdatacloud

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
	// Country is the country name when returned by IPDataCloud.
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
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "IPDataCloud requires one public IPv4 address")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredentialValue(credentials.APIKey.Reveal()) {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureAuthentication, "IPDataCloud credentials are unavailable or invalid")
	}
	query := url.Values{"ip": {parsed.To4().String()}, "key": {credentials.APIKey.Reveal()}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, locationEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "IPDataCloud lookup request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "IPDataCloud lookup request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return locationRetry(sdkgo.FailureRateLimit, "IPDataCloud rate limited the lookup", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return locationRetry(sdkgo.FailureAvailability, "IPDataCloud lookup service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyLocationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "IPDataCloud response exceeded the configured limit")
	}
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "IPDataCloud response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Location *struct {
				Country  string `json:"country"`
				Province string `json:"province"`
				City     string `json:"city"`
				District string `json:"district"`
				ISP      string `json:"isp"`
			} `json:"location"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "IPDataCloud returned invalid lookup data")
	}
	if *wire.Code != 200 {
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 401 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.Code == 403 {
			kind = sdkgo.FailureAuthorization
		}
		return locationBranch(LookupIPLocationBranchProviderRejected, kind, "IPDataCloud rejected lookup with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.Location == nil || strings.TrimSpace(wire.Data.Location.Country) == "" || strings.TrimSpace(wire.Data.Location.City) == "" {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "IPDataCloud returned incomplete location data")
	}
	location := wire.Data.Location
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
	return locationBranch(LookupIPLocationBranchProviderRejected, kind, "IPDataCloud rejected lookup with HTTP "+strconv.Itoa(status))
}
func locationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ipdatacloud", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryBranch(branch, LookupIPLocationOutput{}, &failure, sdkgo.Receipt{})
}
func locationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ipdatacloud", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryRetry[LookupIPLocationOutput](failure, delay)
}
