// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contentful

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ContentfulReadOutput preserves one published resource or collection as JSON.
type ContentfulReadOutput struct {
	// Data contains only the bounded Contentful success response.
	Data json.RawMessage `json:"data"`
}

// GetAssetInput identifies one published asset.
type GetAssetInput struct {
	// SpaceID is the Contentful space ID.
	SpaceID string `json:"spaceId"`
	// EnvironmentID is the environment ID, usually master.
	EnvironmentID string `json:"environmentId"`
	// AssetID is the published asset ID.
	AssetID string `json:"assetId"`
}

// ContentfulPageInput selects one bounded collection page.
type ContentfulPageInput struct {
	// SpaceID is the Contentful space ID.
	SpaceID string `json:"spaceId"`
	// EnvironmentID is the environment ID, usually master.
	EnvironmentID string `json:"environmentId"`
	// Limit is 1 through 1000; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
}

// GetContentTypeInput identifies one published content type.
type GetContentTypeInput struct {
	// SpaceID is the Contentful space ID.
	SpaceID string `json:"spaceId"`
	// EnvironmentID is the environment ID, usually master.
	EnvironmentID string `json:"environmentId"`
	// ContentTypeID is the content type ID.
	ContentTypeID string `json:"contentTypeId"`
}

// GetSpaceInput identifies one space visible to the delivery token.
type GetSpaceInput struct {
	// SpaceID is the Contentful space ID.
	SpaceID string `json:"spaceId"`
}

// GetAssetOperation reads a published asset.
type GetAssetOperation struct{ client *Client }

// ListAssetsOperation reads a bounded asset collection.
type ListAssetsOperation struct{ client *Client }

// GetContentTypeOperation reads a published content model.
type GetContentTypeOperation struct{ client *Client }

// ListContentTypesOperation reads the content model collection.
type ListContentTypesOperation struct{ client *Client }

// GetSpaceOperation reads one space and its locale summary.
type GetSpaceOperation struct{ client *Client }

// ListLocalesOperation reads locales for one environment.
type ListLocalesOperation struct{ client *Client }

// Definition returns the generated asset Query contract.
func (GetAssetOperation) Definition() sdkgo.QueryDefinition { return GetAssetDefinition }

// Definition returns the generated asset collection Query contract.
func (ListAssetsOperation) Definition() sdkgo.QueryDefinition { return ListAssetsDefinition }

// Definition returns the generated content type Query contract.
func (GetContentTypeOperation) Definition() sdkgo.QueryDefinition { return GetContentTypeDefinition }

// Definition returns the generated content model Query contract.
func (ListContentTypesOperation) Definition() sdkgo.QueryDefinition {
	return ListContentTypesDefinition
}

// Definition returns the generated space Query contract.
func (GetSpaceOperation) Definition() sdkgo.QueryDefinition { return GetSpaceDefinition }

// Definition returns the generated locale Query contract.
func (ListLocalesOperation) Definition() sdkgo.QueryDefinition { return ListLocalesDefinition }

// Invoke reads one published asset.
func (operation GetAssetOperation) Invoke(call sdkgo.Call, input GetAssetInput) sdkgo.QueryAttempt[ContentfulReadOutput] {
	if !validDeliveryIDs(input.SpaceID, input.EnvironmentID, input.AssetID) {
		return deliveryDefect("getAsset", GetAssetBranchDefect)
	}
	path := deliveryEnvironmentPath(input.SpaceID, input.EnvironmentID) + "/assets/" + input.AssetID
	return readDeliveryResource(operation.client, call, "getAsset", path, nil, "sys", GetAssetBranchFound, GetAssetBranchNotFound, GetAssetBranchProviderRejected, GetAssetBranchInvalidResponse, GetAssetBranchDefect)
}

// Invoke reads one page of published assets.
func (operation ListAssetsOperation) Invoke(call sdkgo.Call, input ContentfulPageInput) sdkgo.QueryAttempt[ContentfulReadOutput] {
	query, err := deliveryPageQuery(input)
	if err != nil {
		return deliveryDefect("listAssets", ListAssetsBranchDefect)
	}
	path := deliveryEnvironmentPath(input.SpaceID, input.EnvironmentID) + "/assets"
	return readDeliveryResource(operation.client, call, "listAssets", path, query, "items", ListAssetsBranchListed, "", ListAssetsBranchProviderRejected, ListAssetsBranchInvalidResponse, ListAssetsBranchDefect)
}

// Invoke reads one published content type.
func (operation GetContentTypeOperation) Invoke(call sdkgo.Call, input GetContentTypeInput) sdkgo.QueryAttempt[ContentfulReadOutput] {
	if !validDeliveryIDs(input.SpaceID, input.EnvironmentID, input.ContentTypeID) {
		return deliveryDefect("getContentType", GetContentTypeBranchDefect)
	}
	path := deliveryEnvironmentPath(input.SpaceID, input.EnvironmentID) + "/content_types/" + input.ContentTypeID
	return readDeliveryResource(operation.client, call, "getContentType", path, nil, "sys", GetContentTypeBranchFound, GetContentTypeBranchNotFound, GetContentTypeBranchProviderRejected, GetContentTypeBranchInvalidResponse, GetContentTypeBranchDefect)
}

// Invoke reads one bounded content model collection.
func (operation ListContentTypesOperation) Invoke(call sdkgo.Call, input ContentfulPageInput) sdkgo.QueryAttempt[ContentfulReadOutput] {
	query, err := deliveryPageQuery(input)
	if err != nil {
		return deliveryDefect("listContentTypes", ListContentTypesBranchDefect)
	}
	path := deliveryEnvironmentPath(input.SpaceID, input.EnvironmentID) + "/content_types"
	return readDeliveryResource(operation.client, call, "listContentTypes", path, query, "items", ListContentTypesBranchListed, "", ListContentTypesBranchProviderRejected, ListContentTypesBranchInvalidResponse, ListContentTypesBranchDefect)
}

// Invoke reads a space's published metadata and locale summary.
func (operation GetSpaceOperation) Invoke(call sdkgo.Call, input GetSpaceInput) sdkgo.QueryAttempt[ContentfulReadOutput] {
	if !validDeliveryIDs(input.SpaceID) {
		return deliveryDefect("getSpace", GetSpaceBranchDefect)
	}
	return readDeliveryResource(operation.client, call, "getSpace", "/spaces/"+input.SpaceID, nil, "sys", GetSpaceBranchFound, GetSpaceBranchNotFound, GetSpaceBranchProviderRejected, GetSpaceBranchInvalidResponse, GetSpaceBranchDefect)
}

// Invoke reads one bounded collection of locales.
func (operation ListLocalesOperation) Invoke(call sdkgo.Call, input ContentfulPageInput) sdkgo.QueryAttempt[ContentfulReadOutput] {
	query, err := deliveryPageQuery(input)
	if err != nil {
		return deliveryDefect("listLocales", ListLocalesBranchDefect)
	}
	path := deliveryEnvironmentPath(input.SpaceID, input.EnvironmentID) + "/locales"
	return readDeliveryResource(operation.client, call, "listLocales", path, query, "items", ListLocalesBranchListed, "", ListLocalesBranchProviderRejected, ListLocalesBranchInvalidResponse, ListLocalesBranchDefect)
}

func readDeliveryResource(client *Client, call sdkgo.Call, operationID, path string, query url.Values, shape string, success, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.QueryAttempt[ContentfulReadOutput] {
	response := client.send(call, operationID, http.MethodGet, path, query, nil, notFound != "", http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[ContentfulReadOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		branch := rejected
		switch response.branch {
		case sdkgo.DefectBranchID:
			branch = defect
		case "notFound":
			branch = notFound
		case "invalidResponse":
			branch = invalid
		}
		return sdkgo.NewQueryBranch(branch, ContentfulReadOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, shape) {
		return sdkgo.NewQueryBranch(invalid, ContentfulReadOutput{}, failure(operationID, sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(success, ContentfulReadOutput{Data: json.RawMessage(response.body)}, nil, sdkgo.Receipt{})
}

func deliveryDefect(operationID string, branch sdkgo.BranchID) sdkgo.QueryAttempt[ContentfulReadOutput] {
	return sdkgo.NewQueryBranch(branch, ContentfulReadOutput{}, failure(operationID, sdkgo.FailureValidation, "invalid Contentful resource ID or page bounds"), sdkgo.Receipt{})
}

func validDeliveryIDs(ids ...string) bool {
	for _, id := range ids {
		if !identifier.MatchString(id) {
			return false
		}
	}
	return true
}

func deliveryEnvironmentPath(spaceID, environmentID string) string {
	return "/spaces/" + spaceID + "/environments/" + environmentID
}

func deliveryPageQuery(input ContentfulPageInput) (url.Values, error) {
	if !validDeliveryIDs(input.SpaceID, input.EnvironmentID) || input.Limit < 0 || input.Limit > 1000 || input.Skip < 0 {
		return nil, fmt.Errorf("invalid Contentful page input")
	}
	query := url.Values{}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Skip > 0 {
		query.Set("skip", strconv.Itoa(input.Skip))
	}
	return query, nil
}
