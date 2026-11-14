// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vika

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ListSpacesInput requires no caller-provided selector; the token identifies the user.
type ListSpacesInput struct{}

// Space is a Vika workspace the authorized user created or joined.
type Space struct {
	// ID is the stable Vika workspace ID.
	ID string `json:"id"`
	// Name is the workspace display name.
	Name string `json:"name"`
	// IsAdmin reports whether the authorized user is its primary administrator.
	IsAdmin bool `json:"isAdmin"`
}

// SpacePage contains all workspaces returned by Vika's unpaginated endpoint.
type SpacePage struct {
	// Spaces contains only workspace metadata and no datasheet records.
	Spaces []Space `json:"spaces"`
}

// ListSpacesOperation reads authorized workspace metadata.
type ListSpacesOperation struct{ client *Client }

// Definition returns the generated list-spaces contract.
func (ListSpacesOperation) Definition() sdkgo.QueryDefinition { return ListSpacesDefinition }

// Invoke queries Vika and classifies provider, protocol, and transport outcomes.
func (operation ListSpacesOperation) Invoke(call sdkgo.Call, _ ListSpacesInput) sdkgo.QueryAttempt[SpacePage] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validAPIToken(credentials.APIToken.Reveal()) {
		return spaceBranch(ListSpacesBranchDefect, sdkgo.FailureAuthentication, "Vika connection token is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/fusion/v1/spaces", nil)
	if err != nil {
		return spaceBranch(ListSpacesBranchDefect, sdkgo.FailureValidation, "workspace request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIToken.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return spaceRetry(sdkgo.FailureAvailability, "Vika request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return spaceRetry(sdkgo.FailureRateLimit, "Vika rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return spaceRetry(sdkgo.FailureAvailability, "Vika is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return spaceBranch(ListSpacesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Vika response exceeded the configured limit")
	}
	if err != nil {
		return spaceRetry(sdkgo.FailureAvailability, "Vika response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		return spaceBranch(ListSpacesBranchProviderRejected, classifySpaceHTTP(response.StatusCode), "Vika rejected the query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, code, err := decodeSpacePage(body)
	if err != nil {
		return spaceBranch(ListSpacesBranchInvalidResponse, sdkgo.FailureProtocol, "Vika returned an invalid workspace response")
	}
	if code == 429 {
		return spaceRetry(sdkgo.FailureRateLimit, "Vika rate limited the request", retryAfter)
	}
	if code >= 500 {
		return spaceRetry(sdkgo.FailureAvailability, "Vika is temporarily unavailable", retryAfter)
	}
	if code != 200 {
		return spaceBranch(ListSpacesBranchProviderRejected, sdkgo.FailureProviderRejection, "Vika rejected the query with code "+strconv.Itoa(code))
	}
	return sdkgo.NewQueryBranch(ListSpacesBranchListed, page, nil, sdkgo.Receipt{})
}
func decodeSpacePage(body []byte) (SpacePage, int, error) {
	var wire struct {
		Success *bool `json:"success"`
		Code    *int  `json:"code"`
		Data    *struct {
			Spaces []struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				IsAdmin bool   `json:"isAdmin"`
			} `json:"spaces"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil || wire.Code == nil {
		return SpacePage{}, 0, errors.New("invalid Vika JSON")
	}
	if !*wire.Success {
		return SpacePage{}, *wire.Code, nil
	}
	if *wire.Code != 200 || wire.Data == nil || wire.Data.Spaces == nil || len(wire.Data.Spaces) > 1000 {
		return SpacePage{}, 0, errors.New("invalid workspace list")
	}
	page := SpacePage{Spaces: make([]Space, 0, len(wire.Data.Spaces))}
	for _, space := range wire.Data.Spaces {
		if !strings.HasPrefix(space.ID, "spc") || space.Name == "" {
			return SpacePage{}, 0, errors.New("invalid workspace identity")
		}
		page.Spaces = append(page.Spaces, Space{ID: space.ID, Name: space.Name, IsAdmin: space.IsAdmin})
	}
	return page, 200, nil
}
func validAPIToken(token string) bool {
	if token == "" || len(token) > 512 || strings.TrimSpace(token) != token || strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifySpaceHTTP(status int) sdkgo.FailureKind {
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
func spaceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SpacePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "vika", Operation: "listSpaces", Message: message}
	return sdkgo.NewQueryBranch(branch, SpacePage{}, &failure, sdkgo.Receipt{})
}
func spaceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SpacePage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "vika", Operation: "listSpaces", Message: message}
	return sdkgo.NewQueryRetry[SpacePage](failure, delay)
}
