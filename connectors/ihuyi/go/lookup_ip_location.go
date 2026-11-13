// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ihuyi

import (
	"crypto/md5"
	"encoding/hex"
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
	// Country is the country name when returned by Ihuyi.
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

// Invoke sends a time-bound signature and excludes credentials and raw provider errors from the result.
func (operation LookupIPLocationOperation) Invoke(call sdkgo.Call, input LookupIPLocationInput) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	parsed := net.ParseIP(input.IPAddress)
	if parsed == nil || parsed.To4() == nil || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() || parsed.IsMulticast() {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "Ihuyi requires one public IPv4 address")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredentialValue(credentials.APIID) || !isValidCredentialValue(credentials.APIKey.Reveal()) {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureAuthentication, "Ihuyi credentials are unavailable or invalid")
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	digest := md5.Sum([]byte(credentials.APIID + credentials.APIKey.Reveal() + timestamp))
	form := url.Values{
		"account":  {credentials.APIID},
		"password": {hex.EncodeToString(digest[:])},
		"ip":       {parsed.To4().String()},
		"time":     {timestamp},
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, locationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return locationBranch(LookupIPLocationBranchDefect, sdkgo.FailureValidation, "Ihuyi lookup request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "Ihuyi lookup request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return locationRetry(sdkgo.FailureRateLimit, "Ihuyi rate limited the lookup", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return locationRetry(sdkgo.FailureAvailability, "Ihuyi lookup service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyLocationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ihuyi response exceeded the configured limit")
	}
	if err != nil {
		return locationRetry(sdkgo.FailureAvailability, "Ihuyi response was interrupted", 0)
	}
	var wire struct {
		Code   *int `json:"code"`
		Result *struct {
			Country  string `json:"country"`
			Province string `json:"porovince"`
			City     string `json:"city"`
			District string `json:"district"`
			Operator string `json:"operator"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "Ihuyi returned invalid lookup data")
	}
	if *wire.Code != 2 {
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 405 || *wire.Code == 4050 || *wire.Code == 40501 || *wire.Code == 40502 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.Code == 400 || *wire.Code == 4052 {
			kind = sdkgo.FailureAuthorization
		}
		return locationBranch(LookupIPLocationBranchProviderRejected, kind, "Ihuyi rejected lookup with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Result == nil || strings.TrimSpace(wire.Result.Province) == "" || strings.TrimSpace(wire.Result.City) == "" {
		return locationBranch(LookupIPLocationBranchInvalidResponse, sdkgo.FailureProtocol, "Ihuyi returned incomplete location data")
	}
	output := LookupIPLocationOutput{Country: wire.Result.Country, Province: wire.Result.Province, City: wire.Result.City, District: wire.Result.District, Operator: wire.Result.Operator}
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
	return locationBranch(LookupIPLocationBranchProviderRejected, kind, "Ihuyi rejected lookup with HTTP "+strconv.Itoa(status))
}
func locationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ihuyi", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryBranch(branch, LookupIPLocationOutput{}, &failure, sdkgo.Receipt{})
}
func locationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LookupIPLocationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "ihuyi", Operation: "lookupIPLocation", Message: message}
	return sdkgo.NewQueryRetry[LookupIPLocationOutput](failure, delay)
}
