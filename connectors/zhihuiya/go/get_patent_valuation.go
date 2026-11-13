// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zhihuiya

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var publicationNumberPattern = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{5,24}$`)

// GetPatentValuationInput identifies one patent by its publication number.
type GetPatentValuationInput struct {
	// PublicationNumber is the patent publication number, for example JP6012277B2.
	PublicationNumber string `json:"publicationNumber"`
}

// GetPatentValuationOutput includes only the provider's valuation and five numeric scores.
type GetPatentValuationOutput struct {
	// PublicationNumber is the provider's matching patent publication number.
	PublicationNumber string `json:"publicationNumber"`
	// PatentID is the provider's stable patent identifier.
	PatentID string `json:"patentID"`
	// PatentValueUSD is the provider's estimated patent value in US dollars.
	PatentValueUSD int64 `json:"patentValueUSD"`
	// LegalScore is the provider's legal value score.
	LegalScore int64 `json:"legalScore"`
	// MarketScore is the provider's market value score.
	MarketScore int64 `json:"marketScore"`
	// EconomyScore is the provider's economic value score.
	EconomyScore int64 `json:"economyScore"`
	// StrategicScore is the provider's strategic value score.
	StrategicScore int64 `json:"strategicScore"`
	// TechnologyScore is the provider's technology value score.
	TechnologyScore int64 `json:"technologyScore"`
}

// GetPatentValuationOperation reads one valuation without changing provider state.
type GetPatentValuationOperation struct{ client *Client }

// Definition returns the generated patent valuation contract.
func (GetPatentValuationOperation) Definition() sdkgo.QueryDefinition {
	return GetPatentValuationDefinition
}

// Invoke reads a patent valuation and excludes the API Key and raw provider response from the result.
func (operation GetPatentValuationOperation) Invoke(call sdkgo.Call, input GetPatentValuationInput) sdkgo.QueryAttempt[GetPatentValuationOutput] {
	if !publicationNumberPattern.MatchString(input.PublicationNumber) {
		return valuationBranch(GetPatentValuationBranchDefect, sdkgo.FailureValidation, "Zhihuiya requires a patent publication number such as JP6012277B2")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return valuationBranch(GetPatentValuationBranchDefect, sdkgo.FailureAuthentication, "Zhihuiya API Key is unavailable or invalid")
	}
	endpoint := patentValuationEndpoint + "?patent_number=" + url.QueryEscape(input.PublicationNumber)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return valuationBranch(GetPatentValuationBranchDefect, sdkgo.FailureValidation, "Zhihuiya valuation request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return valuationRetry(sdkgo.FailureAvailability, "Zhihuiya valuation request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return valuationRetry(sdkgo.FailureRateLimit, "Zhihuiya rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return valuationRetry(sdkgo.FailureAvailability, "Zhihuiya valuation service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return valuationBranch(GetPatentValuationBranchNoData, sdkgo.FailureNotFound, "Zhihuiya did not find a valuation")
	}
	if response.StatusCode != http.StatusOK {
		return classifyValuationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return valuationBranch(GetPatentValuationBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Zhihuiya response exceeded the configured limit")
	}
	if err != nil {
		return valuationRetry(sdkgo.FailureAvailability, "Zhihuiya response was interrupted", 0)
	}
	var wire struct {
		Status    *bool `json:"status"`
		ErrorCode *int  `json:"error_code"`
		Data      []struct {
			PublicationNumber string `json:"pn"`
			PatentID          string `json:"patent_id"`
			Valuation         *struct {
				PatentValueUSD  int64 `json:"patent_value"`
				LegalScore      int64 `json:"patsnap_legal"`
				MarketScore     int64 `json:"patsnap_market"`
				EconomyScore    int64 `json:"patsnap_economy"`
				StrategicScore  int64 `json:"patsnap_strategic"`
				TechnologyScore int64 `json:"patsnap_technology"`
			} `json:"patsnap_patent_valuation"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Status == nil || wire.ErrorCode == nil {
		return valuationBranch(GetPatentValuationBranchInvalidResponse, sdkgo.FailureProtocol, "Zhihuiya returned malformed valuation data")
	}
	if !*wire.Status || *wire.ErrorCode != 0 {
		if *wire.ErrorCode == 67200000 || *wire.ErrorCode == 67200001 || *wire.ErrorCode == 67200002 {
			return valuationRetry(sdkgo.FailureRateLimit, "Zhihuiya rate limited the request", retryAfter)
		}
		if *wire.ErrorCode == 68300005 || *wire.ErrorCode == 68300008 || *wire.ErrorCode == 67200100 {
			return valuationRetry(sdkgo.FailureAvailability, "Zhihuiya valuation service is temporarily unavailable", retryAfter)
		}
		return valuationBranch(GetPatentValuationBranchProviderRejected, sdkgo.FailureProviderRejection, "Zhihuiya rejected valuation request with code "+strconv.Itoa(*wire.ErrorCode))
	}
	if len(wire.Data) == 0 {
		return valuationBranch(GetPatentValuationBranchNoData, sdkgo.FailureNotFound, "Zhihuiya returned no matching valuation")
	}
	if len(wire.Data) != 1 || wire.Data[0].Valuation == nil || wire.Data[0].PublicationNumber != input.PublicationNumber || wire.Data[0].PatentID == "" {
		return valuationBranch(GetPatentValuationBranchInvalidResponse, sdkgo.FailureProtocol, "Zhihuiya returned an unexpected patent valuation")
	}
	item := wire.Data[0]
	value := item.Valuation
	output := GetPatentValuationOutput{PublicationNumber: item.PublicationNumber, PatentID: item.PatentID, PatentValueUSD: value.PatentValueUSD, LegalScore: value.LegalScore, MarketScore: value.MarketScore, EconomyScore: value.EconomyScore, StrategicScore: value.StrategicScore, TechnologyScore: value.TechnologyScore}
	return sdkgo.NewQueryBranch(GetPatentValuationBranchFound, output, nil, sdkgo.Receipt{})
}

func classifyValuationHTTPStatus(status int) sdkgo.QueryAttempt[GetPatentValuationOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return valuationBranch(GetPatentValuationBranchProviderRejected, kind, "Zhihuiya rejected valuation request with HTTP "+strconv.Itoa(status))
}
func valuationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetPatentValuationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zhihuiya", Operation: "getPatentValuation", Message: message}
	return sdkgo.NewQueryBranch(branch, GetPatentValuationOutput{}, &failure, sdkgo.Receipt{})
}
func valuationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetPatentValuationOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zhihuiya", Operation: "getPatentValuation", Message: message}
	return sdkgo.NewQueryRetry[GetPatentValuationOutput](failure, delay)
}
