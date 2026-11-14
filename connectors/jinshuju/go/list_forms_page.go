// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jinshuju

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListFormsPageInput optionally continues from a previously returned page cursor.
type ListFormsPageInput struct {
	// Next is the opaque cursor returned by the previous page; blank starts at the first page.
	Next string `json:"next"`
}

// FormMetadata identifies a form without its creator or submissions.
type FormMetadata struct {
	// Name is the form's display name.
	Name string `json:"name"`
	// Token is the form identifier used by other Jinshuju API paths.
	Token string `json:"token"`
}

// ListFormsPageOutput contains one page of safe form metadata.
type ListFormsPageOutput struct {
	// Total is the number of accessible forms.
	Total int `json:"total"`
	// Count is the number of forms in this page.
	Count int `json:"count"`
	// Forms are the accessible forms in this page.
	Forms []FormMetadata `json:"forms"`
	// Next is the opaque cursor for the next page; blank means no next page.
	Next string `json:"next"`
}

// ListFormsPageOperation reads form metadata without modifying provider state.
type ListFormsPageOperation struct{ client *Client }

// Definition returns the generated form-list contract.
func (ListFormsPageOperation) Definition() sdkgo.QueryDefinition { return ListFormsPageDefinition }

// Invoke reads one page and excludes creators and submitted data from durable output.
func (operation ListFormsPageOperation) Invoke(call sdkgo.Call, input ListFormsPageInput) sdkgo.QueryAttempt[ListFormsPageOutput] {
	if !isValidCursor(input.Next) {
		return formBranch(ListFormsPageBranchDefect, sdkgo.FailureValidation, "Jinshuju page cursor is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessToken(credentials.AccessToken.Reveal()) {
		return formBranch(ListFormsPageBranchDefect, sdkgo.FailureAuthentication, "Jinshuju Access Token is unavailable or invalid")
	}
	requestURL := formsEndpoint
	if input.Next != "" {
		requestURL += "?next=" + url.QueryEscape(input.Next)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, requestURL, nil)
	if err != nil {
		return formBranch(ListFormsPageBranchDefect, sdkgo.FailureValidation, "Jinshuju form-list request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return formRetry(sdkgo.FailureAvailability, "Jinshuju form-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return formRetry(sdkgo.FailureRateLimit, "Jinshuju rate limited the form-list request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return formRetry(sdkgo.FailureAvailability, "Jinshuju form list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyFormHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return formBranch(ListFormsPageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Jinshuju response exceeded the configured limit")
	}
	if err != nil {
		return formRetry(sdkgo.FailureAvailability, "Jinshuju form-list response was interrupted", 0)
	}
	var wire struct {
		Total *int `json:"total"`
		Count *int `json:"count"`
		Data  []struct {
			Name  string `json:"name"`
			Token string `json:"token"`
		} `json:"data"`
		Next string `json:"next"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Total == nil || wire.Count == nil || wire.Data == nil || *wire.Total < 0 || *wire.Count < 0 || *wire.Count > 50 || len(wire.Data) != *wire.Count || *wire.Total < *wire.Count || !isValidCursor(wire.Next) {
		return formBranch(ListFormsPageBranchInvalidResponse, sdkgo.FailureProtocol, "Jinshuju returned invalid form metadata")
	}
	output := ListFormsPageOutput{Total: *wire.Total, Count: *wire.Count, Forms: make([]FormMetadata, 0, len(wire.Data)), Next: wire.Next}
	for _, form := range wire.Data {
		if form.Name == "" || form.Token == "" {
			return formBranch(ListFormsPageBranchInvalidResponse, sdkgo.FailureProtocol, "Jinshuju returned incomplete form metadata")
		}
		output.Forms = append(output.Forms, FormMetadata{Name: form.Name, Token: form.Token})
	}
	return sdkgo.NewQueryBranch(ListFormsPageBranchListed, output, nil, sdkgo.Receipt{})
}
func classifyFormHTTPStatus(status int) sdkgo.QueryAttempt[ListFormsPageOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return formBranch(ListFormsPageBranchProviderRejected, kind, "Jinshuju rejected the form-list request with HTTP "+strconv.Itoa(status))
}
func formBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListFormsPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jinshuju", Operation: "listFormsPage", Message: message}
	return sdkgo.NewQueryBranch(branch, ListFormsPageOutput{}, &failure, sdkgo.Receipt{})
}
func formRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListFormsPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "jinshuju", Operation: "listFormsPage", Message: message}
	return sdkgo.NewQueryRetry[ListFormsPageOutput](failure, delay)
}
