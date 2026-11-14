// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shixiseng

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
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

// ListIndustriesInput needs no operation-specific values.
type ListIndustriesInput struct{}

// ListIndustriesOutput contains only industry names, without applicant data.
type ListIndustriesOutput struct {
	// Industries are Shixiseng industry filter labels.
	Industries []string `json:"industries"`
}

// ListIndustriesOperation queries the public industry taxonomy.
type ListIndustriesOperation struct{ client *Client }

// Definition returns the generated industry-list Query contract.
func (ListIndustriesOperation) Definition() sdkgo.QueryDefinition { return ListIndustriesDefinition }

// Invoke signs a read request and returns only industry labels.
func (operation ListIndustriesOperation) Invoke(call sdkgo.Call, _ ListIndustriesInput) sdkgo.QueryAttempt[ListIndustriesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID.Reveal()) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return industryBranch(ListIndustriesBranchDefect, sdkgo.FailureAuthentication, "Shixiseng merchant credentials are unavailable or invalid")
	}
	timestamp := strconv.FormatInt(operation.client.clock().Unix(), 10)
	query := url.Values{"sign": {merchantSignature(credentials.AppID.Reveal(), credentials.AppSecret.Reveal(), timestamp)}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, industriesEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return industryBranch(ListIndustriesBranchDefect, sdkgo.FailureValidation, "Shixiseng industry request is invalid")
	}
	request.Header.Set("Authorization", base64.StdEncoding.EncodeToString([]byte(credentials.AppID.Reveal()+":"+timestamp)))
	request.Header.Set("Accept", "text/html")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return industryRetry(sdkgo.FailureAvailability, "Shixiseng industry request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryDelay := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return industryRetry(sdkgo.FailureRateLimit, "Shixiseng rate limited the industry request", retryDelay)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return industryRetry(sdkgo.FailureAvailability, "Shixiseng API is temporarily unavailable", retryDelay)
	}
	if response.StatusCode != http.StatusOK {
		return industryBranch(ListIndustriesBranchProviderRejected, sdkgo.FailureProviderRejection, "Shixiseng rejected industry request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return industryBranch(ListIndustriesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Shixiseng industry response exceeded configured limit")
	}
	if err != nil {
		return industryRetry(sdkgo.FailureAvailability, "Shixiseng industry response was interrupted", 0)
	}
	var wire struct {
		Code *int            `json:"code"`
		Msg  json.RawMessage `json:"msg"`
	}
	if json.Unmarshal(content, &wire) != nil || wire.Code == nil {
		return industryBranch(ListIndustriesBranchInvalidResponse, sdkgo.FailureProtocol, "Shixiseng returned malformed industry data")
	}
	if *wire.Code == 300 {
		return industryRetry(sdkgo.FailureAvailability, "Shixiseng reported internal industry failure", retryDelay)
	}
	if *wire.Code != 100 {
		return industryBranch(ListIndustriesBranchProviderRejected, sdkgo.FailureProviderRejection, "Shixiseng rejected industry request with code "+strconv.Itoa(*wire.Code))
	}
	var categories struct {
		IndustryList []string `json:"industry_list"`
	}
	if json.Unmarshal(wire.Msg, &categories) != nil || len(categories.IndustryList) == 0 || len(categories.IndustryList) > 256 {
		return industryBranch(ListIndustriesBranchInvalidResponse, sdkgo.FailureProtocol, "Shixiseng omitted a valid industry list")
	}
	for _, industry := range categories.IndustryList {
		if industry == "" || len(industry) > 256 || strings.TrimSpace(industry) != industry {
			return industryBranch(ListIndustriesBranchInvalidResponse, sdkgo.FailureProtocol, "Shixiseng returned an invalid industry label")
		}
	}
	return sdkgo.NewQueryBranch(ListIndustriesBranchFound, ListIndustriesOutput{Industries: categories.IndustryList}, nil, sdkgo.Receipt{})
}

func merchantSignature(appID, appSecret, timestamp string) string {
	digest := md5.Sum([]byte(appID + appSecret + timestamp))
	return strings.ToUpper(hex.EncodeToString(digest[:]))
}
func industryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListIndustriesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shixiseng", Operation: "listIndustries", Message: message}
	return sdkgo.NewQueryBranch(branch, ListIndustriesOutput{}, &failure, sdkgo.Receipt{})
}
func industryRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListIndustriesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "shixiseng", Operation: "listIndustries", Message: message}
	return sdkgo.NewQueryRetry[ListIndustriesOutput](failure, delay)
}
