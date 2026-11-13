// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dataeyes

import (
	"bytes"
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

// ReadWebPageInput identifies one public HTTPS page.
type ReadWebPageInput struct {
	// URL is a public HTTPS page with a domain name and no query, fragment, or user info.
	URL string `json:"url"`
}

// ReadWebPageOutput contains the page title and bounded Markdown text.
type ReadWebPageOutput struct {
	// Title is the title reported by DataEyes, if available.
	Title string `json:"title"`
	// Markdown is the extracted page content, limited to 100000 bytes.
	Markdown string `json:"markdown"`
}

// ReadWebPageOperation reads a public page and may consume reading quota.
type ReadWebPageOperation struct{ client *Client }

// Definition returns the generated page-reading contract.
func (ReadWebPageOperation) Definition() sdkgo.QueryDefinition { return ReadWebPageDefinition }

// Invoke reads page Markdown without exposing the input URL in results or failures.
func (operation ReadWebPageOperation) Invoke(call sdkgo.Call, input ReadWebPageInput) sdkgo.QueryAttempt[ReadWebPageOutput] {
	if !isPublicPageURL(input.URL) {
		return readerBranch(ReadWebPageBranchDefect, sdkgo.FailureValidation, "DataEyes URL must be a public HTTPS page without query or fragment")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return readerBranch(ReadWebPageBranchDefect, sdkgo.FailureAuthentication, "DataEyes reading Key is unavailable or invalid")
	}
	payload, err := json.Marshal(struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}{URL: input.URL, Format: "markdown"})
	if err != nil {
		return readerBranch(ReadWebPageBranchDefect, sdkgo.FailureValidation, "DataEyes URL could not be encoded")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, readerEndpoint, bytes.NewReader(payload))
	if err != nil {
		return readerBranch(ReadWebPageBranchDefect, sdkgo.FailureValidation, "DataEyes reader request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return readerRetry(sdkgo.FailureAvailability, "DataEyes reader request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return readerBranch(ReadWebPageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "DataEyes response exceeded the configured limit")
	}
	if err != nil {
		return readerRetry(sdkgo.FailureAvailability, "DataEyes response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Metadata struct {
				Title string `json:"title"`
			} `json:"metadata"`
			Markdown *string `json:"markdown"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) == nil && wire.Code != nil && *wire.Code != 0 {
		switch *wire.Code {
		case 32000, 32001, 32003, 32004, 40001, 40003:
			return readerBranch(ReadWebPageBranchProviderRejected, sdkgo.FailureProviderRejection, "DataEyes could not read this page")
		case 32002, 50001:
			return readerRetry(sdkgo.FailureAvailability, "DataEyes reader is temporarily unavailable", retryAfter)
		default:
			return readerBranch(ReadWebPageBranchProviderRejected, sdkgo.FailureProviderRejection, "DataEyes rejected reading with code "+strconv.Itoa(*wire.Code))
		}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return readerRetry(sdkgo.FailureRateLimit, "DataEyes rate limited the read", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return readerRetry(sdkgo.FailureAvailability, "DataEyes reader is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyReaderHTTPStatus(response.StatusCode)
	}
	if wire.Code == nil || *wire.Code != 0 || wire.Data == nil || wire.Data.Markdown == nil || len(*wire.Data.Markdown) > 100000 || len(wire.Data.Metadata.Title) > 1000 {
		return readerBranch(ReadWebPageBranchInvalidResponse, sdkgo.FailureProtocol, "DataEyes returned invalid page data")
	}
	return sdkgo.NewQueryBranch(ReadWebPageBranchFound, ReadWebPageOutput{Title: wire.Data.Metadata.Title, Markdown: *wire.Data.Markdown}, nil, sdkgo.Receipt{})
}
func isPublicPageURL(value string) bool {
	if value == "" || len(value) > 2048 || strings.TrimSpace(value) != value {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return false
	}
	host := parsed.Hostname()
	if host == "" || net.ParseIP(host) != nil || !strings.Contains(host, ".") || strings.EqualFold(host, "localhost") {
		return false
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return false
	}
	for _, character := range host {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '.' || character == '-') {
			return false
		}
	}
	return true
}
func classifyReaderHTTPStatus(status int) sdkgo.QueryAttempt[ReadWebPageOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return readerBranch(ReadWebPageBranchProviderRejected, kind, "DataEyes rejected reading with HTTP "+strconv.Itoa(status))
}
func readerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ReadWebPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dataeyes", Operation: "readWebPage", Message: message}
	return sdkgo.NewQueryBranch(branch, ReadWebPageOutput{}, &failure, sdkgo.Receipt{})
}
func readerRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ReadWebPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "dataeyes", Operation: "readWebPage", Message: message}
	return sdkgo.NewQueryRetry[ReadWebPageOutput](failure, delay)
}
