// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gugudata

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const segmentationEndpoint = "https://api.gugudata.com/text/segment"

// SegmentTextInput selects short text for Chinese NLP segmentation.
type SegmentTextInput struct {
	// Text is nonblank UTF-8 text of at most 2000 UTF-16 code units.
	Text string `json:"text"`
}

// SegmentTextOutput contains GuGuData's comma-separated words in source order.
type SegmentTextOutput struct {
	// SegmentedText joins provider segments with the documented default comma.
	SegmentedText string `json:"segmentedText"`
}

// SegmentTextOperation reads a short text's segmentation without modifying provider state.
type SegmentTextOperation struct{ client *Client }

// Definition returns the generated segmentation contract.
func (SegmentTextOperation) Definition() sdkgo.QueryDefinition { return SegmentTextDefinition }

// Invoke sends text and AppKey to GuGuData over HTTPS and classifies provider and transport outcomes.
func (operation SegmentTextOperation) Invoke(call sdkgo.Call, input SegmentTextInput) sdkgo.QueryAttempt[SegmentTextOutput] {
	if !utf8.ValidString(input.Text) || strings.TrimSpace(input.Text) == "" || len(utf16.Encode([]rune(input.Text))) > 2000 {
		return segmentationBranch(SegmentTextBranchDefect, sdkgo.FailureValidation, "GuGuData requires nonblank text of at most 2000 UTF-16 units")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAppKey(credentials.AppKey.Reveal()) {
		return segmentationBranch(SegmentTextBranchDefect, sdkgo.FailureAuthentication, "GuGuData credentials are unavailable or invalid")
	}
	query := url.Values{"text": {input.Text}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, segmentationEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return segmentationBranch(SegmentTextBranchDefect, sdkgo.FailureValidation, "GuGuData segmentation request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-GUGUDATA-APPKEY", credentials.AppKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return segmentationRetry(sdkgo.FailureAvailability, "GuGuData segmentation request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return segmentationRetry(sdkgo.FailureRateLimit, "GuGuData rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return segmentationRetry(sdkgo.FailureAvailability, "GuGuData segmentation service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifySegmentationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return segmentationBranch(SegmentTextBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "GuGuData response exceeded the configured limit")
	}
	if err != nil {
		return segmentationRetry(sdkgo.FailureAvailability, "GuGuData response was interrupted", 0)
	}
	var wire struct {
		DataStatus struct {
			StatusCode *int `json:"StatusCode"`
		} `json:"DataStatus"`
		Data *struct {
			SegmentedText string `json:"SegmentaryText"`
		} `json:"Data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.DataStatus.StatusCode == nil {
		return segmentationBranch(SegmentTextBranchInvalidResponse, sdkgo.FailureProtocol, "GuGuData returned invalid segmentation data")
	}
	if *wire.DataStatus.StatusCode != 100 {
		return segmentationBranch(SegmentTextBranchProviderRejected, sdkgo.FailureProviderRejection, "GuGuData rejected segmentation with code "+strconv.Itoa(*wire.DataStatus.StatusCode))
	}
	if wire.Data == nil {
		return segmentationBranch(SegmentTextBranchInvalidResponse, sdkgo.FailureProtocol, "GuGuData returned no segmentation data")
	}
	return sdkgo.NewQueryBranch(SegmentTextBranchSegmented, SegmentTextOutput{SegmentedText: wire.Data.SegmentedText}, nil, sdkgo.Receipt{})
}

func isValidAppKey(value string) bool {
	if value == "" || len(value) > 1024 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}

func classifySegmentationHTTPStatus(status int) sdkgo.QueryAttempt[SegmentTextOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return segmentationBranch(SegmentTextBranchProviderRejected, kind, "GuGuData rejected segmentation with HTTP "+strconv.Itoa(status))
}

func segmentationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SegmentTextOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gugudata", Operation: "segmentText", Message: message}
	return sdkgo.NewQueryBranch(branch, SegmentTextOutput{}, &failure, sdkgo.Receipt{})
}

func segmentationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SegmentTextOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "gugudata", Operation: "segmentText", Message: message}
	return sdkgo.NewQueryRetry[SegmentTextOutput](failure, delay)
}
