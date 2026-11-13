// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chinaz

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetSiteTDKInput selects one public website by domain name.
type GetSiteTDKInput struct {
	// Domain is a hostname without scheme, port, or path.
	Domain string `json:"domain"`
}

// SiteTDK contains selected public metadata from the Chinaz site index.
type SiteTDK struct {
	// Domain is the requested domain name.
	Domain string `json:"domain"`
	// Title is the indexed site title.
	Title string `json:"title"`
	// Keywords is the indexed site keyword text.
	Keywords string `json:"keywords"`
	// Description is the indexed site description.
	Description string `json:"description"`
	// IPAddress is the provider-reported site IP address.
	IPAddress string `json:"ipAddress"`
}

// GetSiteTDKOperation reads site metadata for one domain.
type GetSiteTDKOperation struct{ client *Client }

// Definition returns the generated site-TDK Query contract.
func (GetSiteTDKOperation) Definition() sdkgo.QueryDefinition { return GetSiteTDKDefinition }

// Invoke queries Chinaz and classifies provider, protocol, and transport outcomes.
func (operation GetSiteTDKOperation) Invoke(call sdkgo.Call, input GetSiteTDKInput) sdkgo.QueryAttempt[SiteTDK] {
	if !validDomain(input.Domain) {
		return siteBranch(GetSiteTDKBranchDefect, sdkgo.FailureValidation, "Chinaz domain must be a valid hostname without scheme or path")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAPIKey(credentials.APIKey.Reveal()) {
		return siteBranch(GetSiteTDKBranchDefect, sdkgo.FailureAuthentication, "Chinaz API key is unavailable or invalid")
	}
	formBody := &bytes.Buffer{}
	formWriter := multipart.NewWriter(formBody)
	if err := formWriter.WriteField("key", credentials.APIKey.Reveal()); err != nil {
		return siteBranch(GetSiteTDKBranchDefect, sdkgo.FailureValidation, "site TDK form is invalid")
	}
	if err := formWriter.WriteField("domain", input.Domain); err != nil {
		return siteBranch(GetSiteTDKBranchDefect, sdkgo.FailureValidation, "site TDK form is invalid")
	}
	if err := formWriter.Close(); err != nil {
		return siteBranch(GetSiteTDKBranchDefect, sdkgo.FailureValidation, "site TDK form is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/single/tdk", formBody)
	if err != nil {
		return siteBranch(GetSiteTDKBranchDefect, sdkgo.FailureValidation, "site TDK request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", formWriter.FormDataContentType())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return siteRetry(sdkgo.FailureAvailability, "Chinaz site TDK request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return siteRetry(sdkgo.FailureRateLimit, "Chinaz rate limited site TDK requests", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return siteRetry(sdkgo.FailureAvailability, "Chinaz site TDK is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return siteBranch(GetSiteTDKBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Chinaz response exceeded the configured limit")
	}
	if err != nil {
		return siteRetry(sdkgo.FailureAvailability, "Chinaz response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return siteBranch(GetSiteTDKBranchProviderRejected, classifySiteHTTP(response.StatusCode), "Chinaz rejected site TDK query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	site, state, err := decodeSiteTDK(input.Domain, body)
	if err != nil {
		return siteBranch(GetSiteTDKBranchInvalidResponse, sdkgo.FailureProtocol, "Chinaz returned an invalid site TDK response")
	}
	switch state {
	case 1:
		return sdkgo.NewQueryBranch(GetSiteTDKBranchFound, site, nil, sdkgo.Receipt{})
	case 10025:
		return siteBranch(GetSiteTDKBranchNotFound, sdkgo.FailureProviderRejection, "Chinaz has no site TDK data for this domain")
	case 10011, 10012, 10013, 10026:
		return siteRetry(sdkgo.FailureRateLimit, "Chinaz rate limited site TDK requests", retryAfter)
	case -1, 10020, 10024, 10029, 10034:
		return siteRetry(sdkgo.FailureAvailability, "Chinaz site TDK is temporarily unavailable", retryAfter)
	case 10001, 10003, 10004, 10009:
		return siteBranch(GetSiteTDKBranchProviderRejected, sdkgo.FailureAuthentication, "Chinaz rejected the API key")
	case 10002, 10005, 10008, 10022, 10027, 10032:
		return siteBranch(GetSiteTDKBranchProviderRejected, sdkgo.FailureAuthorization, "Chinaz rejected the API plan or account")
	default:
		return siteBranch(GetSiteTDKBranchProviderRejected, sdkgo.FailureProviderRejection, "Chinaz rejected site TDK query with state "+strconv.Itoa(state))
	}
}

func decodeSiteTDK(domain string, body []byte) (SiteTDK, int, error) {
	var wire struct {
		StateCode *int `json:"StateCode"`
		Result    *struct {
			Title       string `json:"Title"`
			Keywords    string `json:"Keywords"`
			Description string `json:"Description"`
			IP          string `json:"IP"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.StateCode == nil {
		return SiteTDK{}, 0, errors.New("invalid Chinaz JSON")
	}
	if *wire.StateCode != 1 {
		return SiteTDK{}, *wire.StateCode, nil
	}
	if wire.Result == nil {
		return SiteTDK{}, 0, errors.New("missing Chinaz site result")
	}
	return SiteTDK{Domain: domain, Title: wire.Result.Title, Keywords: wire.Result.Keywords, Description: wire.Result.Description, IPAddress: wire.Result.IP}, 1, nil
}
func validDomain(domain string) bool {
	if len(domain) < 3 || len(domain) > 253 || strings.ToLower(domain) != domain || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}
func validAPIKey(key string) bool {
	if key == "" || len(key) > 2048 || strings.TrimSpace(key) != key {
		return false
	}
	for _, character := range key {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifySiteHTTP(status int) sdkgo.FailureKind {
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
func siteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SiteTDK] {
	failure := sdkgo.Failure{Kind: kind, Provider: "chinaz", Operation: "getSiteTDK", Message: message}
	return sdkgo.NewQueryBranch(branch, SiteTDK{}, &failure, sdkgo.Receipt{})
}
func siteRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SiteTDK] {
	failure := sdkgo.Failure{Kind: kind, Provider: "chinaz", Operation: "getSiteTDK", Message: message}
	return sdkgo.NewQueryRetry[SiteTDK](failure, delay)
}
