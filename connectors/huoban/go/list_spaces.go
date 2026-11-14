// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package huoban

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListSpacesInput requires no selection because the API Key determines authorized workspaces.
type ListSpacesInput struct{}

// SpaceMetadata identifies an authorized workspace without cover images or member data.
type SpaceMetadata struct {
	// SpaceID is the provider's opaque workspace identifier.
	SpaceID string `json:"spaceId"`
	// Name is the workspace display name.
	Name string `json:"name"`
}

// ListSpacesOutput contains only authorized workspace IDs and names.
type ListSpacesOutput struct {
	// Spaces are the workspaces returned for this API Key.
	Spaces []SpaceMetadata `json:"spaces"`
}

// ListSpacesOperation reads workspace metadata without modifying provider state.
type ListSpacesOperation struct{ client *Client }

// Definition returns the generated workspace-list contract.
func (ListSpacesOperation) Definition() sdkgo.QueryDefinition { return ListSpacesDefinition }

// Invoke lists workspaces and discards cover images and other response fields.
func (operation ListSpacesOperation) Invoke(call sdkgo.Call, _ ListSpacesInput) sdkgo.QueryAttempt[ListSpacesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return spacesBranch(ListSpacesBranchDefect, sdkgo.FailureAuthentication, "Huoban API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, spacesEndpoint, nil)
	if err != nil {
		return spacesBranch(ListSpacesBranchDefect, sdkgo.FailureValidation, "Huoban workspace-list request is invalid")
	}
	request.Header.Set("Open-Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return spacesRetry(sdkgo.FailureAvailability, "Huoban workspace-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return spacesRetry(sdkgo.FailureRateLimit, "Huoban rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return spacesRetry(sdkgo.FailureAvailability, "Huoban workspace list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifySpacesHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return spacesBranch(ListSpacesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Huoban response exceeded the configured limit")
	}
	if err != nil {
		return spacesRetry(sdkgo.FailureAvailability, "Huoban response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			Spaces []struct {
				SpaceID string `json:"space_id"`
				Name    string `json:"name"`
			} `json:"spaces"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return spacesBranch(ListSpacesBranchInvalidResponse, sdkgo.FailureProtocol, "Huoban returned invalid workspace metadata")
	}
	if *wire.Code != 0 {
		return spacesBranch(ListSpacesBranchProviderRejected, sdkgo.FailureProviderRejection, "Huoban rejected the workspace-list request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.Spaces == nil {
		return spacesBranch(ListSpacesBranchInvalidResponse, sdkgo.FailureProtocol, "Huoban returned incomplete workspace metadata")
	}
	output := ListSpacesOutput{Spaces: make([]SpaceMetadata, 0, len(wire.Data.Spaces))}
	for _, space := range wire.Data.Spaces {
		if space.SpaceID == "" || space.Name == "" {
			return spacesBranch(ListSpacesBranchInvalidResponse, sdkgo.FailureProtocol, "Huoban returned an incomplete workspace")
		}
		output.Spaces = append(output.Spaces, SpaceMetadata{SpaceID: space.SpaceID, Name: space.Name})
	}
	return sdkgo.NewQueryBranch(ListSpacesBranchListed, output, nil, sdkgo.Receipt{})
}

func classifySpacesHTTPStatus(status int) sdkgo.QueryAttempt[ListSpacesOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return spacesBranch(ListSpacesBranchProviderRejected, kind, "Huoban rejected the workspace-list request with HTTP "+strconv.Itoa(status))
}
func spacesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListSpacesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "huoban", Operation: "listSpaces", Message: message}
	return sdkgo.NewQueryBranch(branch, ListSpacesOutput{}, &failure, sdkgo.Receipt{})
}
func spacesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListSpacesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "huoban", Operation: "listSpaces", Message: message}
	return sdkgo.NewQueryRetry[ListSpacesOutput](failure, delay)
}
