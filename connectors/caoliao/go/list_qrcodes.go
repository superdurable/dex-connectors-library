// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package caoliao

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const qrCodesPath = "/api/v2/rpc/qrcodes/list"

// ListQRCodesInput selects a bounded page of organization QR codes.
type ListQRCodesInput struct {
	// PageSize is 1 to 100; zero uses the provider's default of 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the opaque token returned as NextPageToken; blank starts the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// QRCodeSummary contains a QR-code name and coding identifier without scanned records.
type QRCodeSummary struct {
	// Name is the organization's QR-code name.
	Name string `json:"name"`
	// Coding identifies the QR code for later provider operations.
	Coding string `json:"coding"`
}

// ListQRCodesOutput contains one page of QR-code metadata.
type ListQRCodesOutput struct {
	// QRCodes are the returned QR-code summaries, possibly empty.
	QRCodes []QRCodeSummary `json:"qrCodes"`
	// NextPageToken is the provider cursor; blank indicates no next page.
	NextPageToken string `json:"nextPageToken"`
	// Total is the number of matching QR codes reported by Caoliao.
	Total int `json:"total"`
}

// ListQRCodesOperation reads one Caoliao organization QR-code page.
type ListQRCodesOperation struct{ client *Client }

// Definition returns the generated QR-code-list Query contract.
func (ListQRCodesOperation) Definition() sdkgo.QueryDefinition { return ListQRCodesDefinition }

// Invoke calls the read-only POST endpoint and discards other provider fields.
func (operation ListQRCodesOperation) Invoke(call sdkgo.Call, input ListQRCodesInput) sdkgo.QueryAttempt[ListQRCodesOutput] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 4096 || !isSafePageToken(input.PageToken) {
		return qrCodeBranch(ListQRCodesBranchDefect, sdkgo.FailureValidation, "Caoliao page size or token is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return qrCodeBranch(ListQRCodesBranchDefect, sdkgo.FailureAuthentication, "Caoliao API key is unavailable or invalid")
	}
	requestBody := struct {
		PageSize  int    `json:"page_size,omitempty"`
		PageToken string `json:"page_token,omitempty"`
	}{PageSize: input.PageSize, PageToken: input.PageToken}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return qrCodeBranch(ListQRCodesBranchDefect, sdkgo.FailureValidation, "Caoliao page request is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+qrCodesPath, bytes.NewReader(payload))
	if err != nil {
		return qrCodeBranch(ListQRCodesBranchDefect, sdkgo.FailureValidation, "Caoliao page request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return qrCodeRetry(sdkgo.FailureAvailability, "Caoliao QR-code list transport failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == 429 {
		return qrCodeRetry(sdkgo.FailureRateLimit, "Caoliao QR-code list rate limited", retryAfter)
	}
	if response.StatusCode == 408 || response.StatusCode >= 500 {
		return qrCodeRetry(sdkgo.FailureAvailability, "Caoliao QR-code list temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return qrCodeBranch(ListQRCodesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Caoliao QR-code list response exceeded the configured limit")
	}
	if err != nil {
		return qrCodeRetry(sdkgo.FailureAvailability, "Caoliao QR-code list response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = sdkgo.FailureAuthentication
		}
		return qrCodeBranch(ListQRCodesBranchProviderRejected, kind, "Caoliao rejected QR-code list with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code      *int `json:"code"`
		ErrorCode int  `json:"error_code"`
		Data      *struct {
			List          []QRCodeSummary `json:"list"`
			NextPageToken *string         `json:"next_page_token"`
			Total         int             `json:"total"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if decoder.Decode(&wire) != nil || wire.Code == nil {
		return qrCodeBranch(ListQRCodesBranchInvalidResponse, sdkgo.FailureProtocol, "Caoliao returned invalid QR-code list JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return qrCodeBranch(ListQRCodesBranchInvalidResponse, sdkgo.FailureProtocol, "Caoliao returned trailing QR-code list JSON")
	}
	if *wire.Code != 0 {
		if *wire.Code == 429 {
			return qrCodeRetry(sdkgo.FailureRateLimit, "Caoliao QR-code list rate limited", retryAfter)
		}
		if *wire.Code == 502 || wire.ErrorCode == 100300 {
			return qrCodeRetry(sdkgo.FailureAvailability, "Caoliao QR-code list temporarily unavailable", retryAfter)
		}
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 401 || wire.ErrorCode == 100315 || wire.ErrorCode == 100316 {
			kind = sdkgo.FailureAuthentication
		}
		return qrCodeBranch(ListQRCodesBranchProviderRejected, kind, "Caoliao rejected QR-code list with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.List == nil || wire.Data.Total < 0 {
		return qrCodeBranch(ListQRCodesBranchInvalidResponse, sdkgo.FailureProtocol, "Caoliao QR-code list is missing")
	}
	nextPageToken := ""
	if wire.Data.NextPageToken != nil {
		nextPageToken = *wire.Data.NextPageToken
	}
	if len(wire.Data.List) > 100 || len(nextPageToken) > 4096 || !isSafePageToken(nextPageToken) {
		return qrCodeBranch(ListQRCodesBranchInvalidResponse, sdkgo.FailureProtocol, "Caoliao returned invalid pagination metadata")
	}
	codes := make([]QRCodeSummary, 0, len(wire.Data.List))
	for _, item := range wire.Data.List {
		if item.Name == "" || len(item.Name) > 1024 || !utf8.ValidString(item.Name) || item.Coding == "" || len(item.Coding) > 256 || !isSafePageToken(item.Coding) {
			return qrCodeBranch(ListQRCodesBranchInvalidResponse, sdkgo.FailureProtocol, "Caoliao returned invalid QR-code metadata")
		}
		codes = append(codes, item)
	}
	return sdkgo.NewQueryBranch(ListQRCodesBranchListed, ListQRCodesOutput{QRCodes: codes, NextPageToken: nextPageToken, Total: wire.Data.Total}, nil, sdkgo.Receipt{})
}
func isSafePageToken(value string) bool {
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func qrCodeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListQRCodesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "caoliao", Operation: "listQRCodes", Message: message}
	return sdkgo.NewQueryBranch(branch, ListQRCodesOutput{}, &failure, sdkgo.Receipt{})
}
func qrCodeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListQRCodesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "caoliao", Operation: "listQRCodes", Message: message}
	return sdkgo.NewQueryRetry[ListQRCodesOutput](failure, delay)
}
