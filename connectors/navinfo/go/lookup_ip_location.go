// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package navinfo

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LookupIPLocationInput identifies one IPv4 address.
type LookupIPLocationInput struct {
	// IPAddress is a literal address, not a hostname or CIDR range.
	IPAddress string `json:"ipAddress"`
}

// LookupIPLocationOutput contains only coarse NavInfo location fields.
type LookupIPLocationOutput struct {
	// Province is the province or municipality name.
	Province string `json:"province"`
	// City is the city name.
	City string `json:"city"`
	// Adcode is the city administrative code.
	Adcode string `json:"adcode"`
}

// LookupIPLocationOperation reads city-level location without changing provider state.
type LookupIPLocationOperation struct{ client *Client }

// Definition returns the generated location-lookup contract.
func (LookupIPLocationOperation) Definition() sdkgo.QueryDefinition {
	return LookupIPLocationDefinition
}

// Invoke queries one validated IP and excludes the AK-bearing URL from failures.
func (operation LookupIPLocationOperation) Invoke(call sdkgo.Call, input LookupIPLocationInput) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	parsed := net.ParseIP(input.IPAddress)
	if parsed != nil {
		parsed = parsed.To4()
	}
	if parsed == nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "NavInfo requires one valid IP address")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAK(credentials.AK.Reveal()) {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureAuthentication, "NavInfo AK is unavailable or invalid")
	}
	query := url.Values{"ak": {credentials.AK.Reveal()}, "ip": {parsed.String()}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, locationEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "NavInfo location request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "NavInfo location request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return locationRetry(sdkgo.FailureRateLimit, "NavInfo rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return locationRetry(sdkgo.FailureAvailability, "NavInfo location service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyLocationHTTPStatus(response.StatusCode)
	}
	payload, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "NavInfo response exceeded the configured limit")
	}
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "NavInfo response was interrupted", 0)
	}
	var wire struct {
		Status *int `json:"status"`
		Data   *struct {
			Province string `json:"prov"`
			City     string `json:"city"`
			Adcode   string `json:"adcode"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &wire) != nil || wire.Status == nil {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "NavInfo returned invalid location data")
	}
	switch *wire.Status {
	case 200:
		if wire.Data == nil || len(wire.Data.Province) > 128 || len(wire.Data.City) > 128 || len(wire.Data.Adcode) > 16 || strings.ContainsAny(wire.Data.Province+wire.Data.City+wire.Data.Adcode, "\r\n\x00") || strings.Contains(wire.Data.Province+wire.Data.City+wire.Data.Adcode, credentials.AK.Reveal()) {
			return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "NavInfo returned incomplete location data")
		}
		return sdkgo.NewQueryBranch(LookupIPLocationBranchFound, LookupIPLocationOutput{Province: wire.Data.Province, City: wire.Data.City, Adcode: wire.Data.Adcode}, nil, sdkgo.Receipt{})
	case 1008, 1010:
		return locationRetry(sdkgo.FailureRateLimit, "NavInfo is temporarily busy", 0)
	default:
		return locationBranch(LookupIPLocationBranchProviderRejected, sdkgo.FailureProviderRejection, "NavInfo rejected location request with status "+strconv.Itoa(*wire.Status))
	}
}
func classifyLocationHTTPStatus(status int) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return locationBranch(LookupIPLocationBranchProviderRejected, kind, "NavInfo rejected location request with HTTP "+strconv.Itoa(status))
}
func locationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "navinfo", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryBranch(branch, LookupIPLocationOutput{}, &failure, sdkgo.Receipt{})
}
func locationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "navinfo", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryRetry[LookupIPLocationOutput](failure, delay)
}
