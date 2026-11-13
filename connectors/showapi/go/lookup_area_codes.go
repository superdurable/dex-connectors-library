// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package showapi

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

// LookupAreaCodesInput names the place to resolve to weather area codes.
type LookupAreaCodesInput struct {
	// Area is a nonempty Chinese place name, limited to 100 Unicode characters.
	Area string `json:"area"`
}

// AreaCodeMatch is a weather-region match without customer data.
type AreaCodeMatch struct {
	// Province is the province returned by ShowAPI.
	Province string `json:"province"`
	// Area is the city or district returned by ShowAPI.
	Area string `json:"area"`
	// AreaCode is the weather region code for subsequent forecast calls.
	AreaCode string `json:"areaCode"`
}

// AreaCodeMatches contains the bounded matches for one place name.
type AreaCodeMatches struct {
	// Matches contains up to 100 provider matches.
	Matches []AreaCodeMatch `json:"matches"`
}

// LookupAreaCodesOperation resolves weather-area names through ShowAPI.
type LookupAreaCodesOperation struct{ client *Client }

// Definition returns the generated area-code lookup contract.
func (LookupAreaCodesOperation) Definition() sdkgo.QueryDefinition { return LookupAreaCodesDefinition }

// Invoke makes a billed, read-only lookup and classifies provider outcomes.
func (operation LookupAreaCodesOperation) Invoke(call sdkgo.Call, input LookupAreaCodesInput) sdkgo.QueryAttempt[AreaCodeMatches] {
	area := strings.TrimSpace(input.Area)
	if area == "" || len([]rune(area)) > 100 || strings.ContainsAny(area, "\r\n") {
		return areaBranch(LookupAreaCodesBranchDefect, sdkgo.FailureValidation, "ShowAPI place name is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAppKey(credentials.AppKey.Reveal()) {
		return areaBranch(LookupAreaCodesBranchDefect, sdkgo.FailureAuthentication, "ShowAPI AppKey is unavailable or invalid")
	}
	// ShowAPI documents appKey in the HTTPS query string. Never expose the URL in results or errors.
	endpoint := operation.client.baseURL + "/9-3?appKey=" + url.QueryEscape(credentials.AppKey.Reveal())
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, strings.NewReader(url.Values{"area": {area}}.Encode()))
	if err != nil {
		return areaBranch(LookupAreaCodesBranchDefect, sdkgo.FailureValidation, "ShowAPI request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return areaRetry(sdkgo.FailureAvailability, "ShowAPI request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return areaRetry(sdkgo.FailureRateLimit, "ShowAPI rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return areaRetry(sdkgo.FailureAvailability, "ShowAPI is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return areaBranch(LookupAreaCodesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ShowAPI response exceeded the configured limit")
	}
	if err != nil {
		return areaRetry(sdkgo.FailureAvailability, "ShowAPI response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		return areaBranch(LookupAreaCodesBranchProviderRejected, classifyAreaHTTP(response.StatusCode), "ShowAPI rejected the query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	matches, systemCode, businessCode, err := decodeAreaMatches(body)
	if err != nil {
		return areaBranch(LookupAreaCodesBranchInvalidResponse, sdkgo.FailureProtocol, "ShowAPI returned an invalid area-code response")
	}
	if systemCode != 0 {
		if systemCode == -1004 {
			return areaBranch(LookupAreaCodesBranchProviderRejected, sdkgo.FailureAuthentication, "ShowAPI rejected the AppKey")
		}
		return areaBranch(LookupAreaCodesBranchProviderRejected, sdkgo.FailureProviderRejection, "ShowAPI rejected the query with system code "+strconv.Itoa(systemCode))
	}
	if businessCode != 0 {
		return areaBranch(LookupAreaCodesBranchProviderRejected, sdkgo.FailureProviderRejection, "ShowAPI rejected the place name with code "+strconv.Itoa(businessCode))
	}
	return sdkgo.NewQueryBranch(LookupAreaCodesBranchFound, matches, nil, sdkgo.Receipt{})
}

func decodeAreaMatches(body []byte) (AreaCodeMatches, int, int, error) {
	var wire struct {
		SystemCode *int `json:"showapi_res_code"`
		Body       *struct {
			ReturnCode *int `json:"ret_code"`
			List       []struct {
				Province string `json:"prov"`
				Area     string `json:"area"`
				AreaCode string `json:"areaCode"`
			} `json:"list"`
		} `json:"showapi_res_body"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.SystemCode == nil {
		return AreaCodeMatches{}, 0, 0, errors.New("invalid ShowAPI envelope")
	}
	if *wire.SystemCode != 0 {
		return AreaCodeMatches{}, *wire.SystemCode, 0, nil
	}
	if wire.Body == nil || wire.Body.ReturnCode == nil {
		return AreaCodeMatches{}, 0, 0, errors.New("missing ShowAPI business code")
	}
	if *wire.Body.ReturnCode != 0 {
		return AreaCodeMatches{}, 0, *wire.Body.ReturnCode, nil
	}
	if wire.Body.List == nil || len(wire.Body.List) > 100 {
		return AreaCodeMatches{}, 0, 0, errors.New("invalid area list")
	}
	matches := AreaCodeMatches{Matches: make([]AreaCodeMatch, 0, len(wire.Body.List))}
	for _, match := range wire.Body.List {
		if match.Area == "" || match.AreaCode == "" {
			return AreaCodeMatches{}, 0, 0, errors.New("invalid area match")
		}
		matches.Matches = append(matches.Matches, AreaCodeMatch{Province: match.Province, Area: match.Area, AreaCode: match.AreaCode})
	}
	return matches, 0, 0, nil
}

func validAppKey(key string) bool {
	if key == "" || len(key) > 512 || strings.TrimSpace(key) != key {
		return false
	}
	for _, character := range key {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyAreaHTTP(status int) sdkgo.FailureKind {
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
func areaBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AreaCodeMatches] {
	failure := sdkgo.Failure{Kind: kind, Provider: "showapi", Operation: "lookupAreaCodes", Message: message}
	return sdkgo.NewQueryBranch(branch, AreaCodeMatches{}, &failure, sdkgo.Receipt{})
}
func areaRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[AreaCodeMatches] {
	failure := sdkgo.Failure{Kind: kind, Provider: "showapi", Operation: "lookupAreaCodes", Message: message}
	return sdkgo.NewQueryRetry[AreaCodeMatches](failure, delay)
}
