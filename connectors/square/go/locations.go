// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"encoding/json"
	"errors"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Location preserves the complete Square seller location response.
type Location struct {
	// Location contains the provider location fields.
	Location map[string]any `json:"location"`
}

// LocationList contains all seller locations returned by Square.
type LocationList struct {
	// Locations contains provider location fields, including IDs and status.
	Locations []map[string]any `json:"locations"`
}

// ListLocationsInput selects the seller's locations without additional filters.
type ListLocationsInput struct{}

// GetLocationInput identifies one seller location.
type GetLocationInput struct {
	// LocationID is a Square location ID or `main` for the primary location.
	LocationID string `json:"locationId"`
}

// ListLocationsOperation reads seller locations.
type ListLocationsOperation struct{ client *Client }

// GetLocationOperation reads one seller location.
type GetLocationOperation struct{ client *Client }

// ListLocations binds the listLocations Query to this client.
func (client *Client) ListLocations() ListLocationsOperation { return ListLocationsOperation{client} }

// GetLocation binds the getLocation Query to this client.
func (client *Client) GetLocation() GetLocationOperation { return GetLocationOperation{client} }

// Definition returns the manifest-generated contract.
func (ListLocationsOperation) Definition() sdkgo.QueryDefinition { return ListLocationsDefinition }

// Definition returns the manifest-generated contract.
func (GetLocationOperation) Definition() sdkgo.QueryDefinition { return GetLocationDefinition }

// Invoke lists the seller's locations, including inactive locations.
func (operation ListLocationsOperation) Invoke(call sdkgo.Call, _ ListLocationsInput) sdkgo.QueryAttempt[LocationList] {
	return executeRead(operation.client, call, "listLocations", "/locations", url.Values{}, ListLocationsBranchListed, ListLocationsBranchProviderRejected, ListLocationsBranchProviderRejected, ListLocationsBranchInvalidResponse, ListLocationsBranchDefect, decodeLocationList)
}

// Invoke reads one seller location or the primary `main` location.
func (operation GetLocationOperation) Invoke(call sdkgo.Call, input GetLocationInput) sdkgo.QueryAttempt[Location] {
	if !validPathSegment(input.LocationID) {
		return failedAttempt[Location]("getLocation", GetLocationBranchDefect, sdkgo.FailureValidation, "locationId is required")
	}
	return executeRead(operation.client, call, "getLocation", "/locations/"+url.PathEscape(input.LocationID), url.Values{}, GetLocationBranchFound, GetLocationBranchNotFound, GetLocationBranchProviderRejected, GetLocationBranchInvalidResponse, GetLocationBranchDefect, decodeLocation)
}

func decodeLocationList(body []byte) (LocationList, error) {
	var result LocationList
	if err := json.Unmarshal(body, &result); err != nil {
		return LocationList{}, err
	}
	if result.Locations == nil {
		result.Locations = []map[string]any{}
	}
	return result, nil
}

func decodeLocation(body []byte) (Location, error) {
	var result Location
	if err := json.Unmarshal(body, &result); err != nil {
		return Location{}, err
	}
	if result.Location == nil || result.Location["id"] == nil {
		return Location{}, errors.New("missing location")
	}
	return result, nil
}
