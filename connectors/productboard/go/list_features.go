// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListFeaturesInput selects a page by its opaque, non-secret cursor.
type ListFeaturesInput struct {
	// PageCursor is blank for the first page or a previous NextCursor value.
	PageCursor string `json:"pageCursor"`
}

// FeatureMetadata contains only the feature ID and name.
type FeatureMetadata struct {
	// ID is the Productboard entity ID.
	ID string `json:"id"`
	// Name is the feature name.
	Name string `json:"name"`
}

// FeaturesPage contains one page and the next opaque cursor, if any.
type FeaturesPage struct {
	// Features excludes descriptions and other provider fields.
	Features []FeatureMetadata `json:"features"`
	// NextCursor is blank at the last page.
	NextCursor string `json:"nextCursor"`
}

// ListFeaturesOperation reads one feature page without exposing other entity fields.
type ListFeaturesOperation struct{ client *Client }

// ListFeatures binds the Query to this client.
func (client *Client) ListFeatures() ListFeaturesOperation { return ListFeaturesOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListFeaturesOperation) Definition() sdkgo.QueryDefinition { return ListFeaturesDefinition }

// Invoke reads one page and classifies retryable and definitive outcomes.
func (operation ListFeaturesOperation) Invoke(call sdkgo.Call, input ListFeaturesInput) sdkgo.QueryAttempt[FeaturesPage] {
	if !validCursor(input.PageCursor) {
		return featuresBranch(ListFeaturesBranchDefect, sdkgo.FailureValidation, "page cursor contains invalid characters or exceeds 2048 bytes")
	}
	query := url.Values{"type[]": {"feature"}, "fields[]": {"name"}}
	if input.PageCursor != "" {
		query.Set("pageCursor", input.PageCursor)
	}
	response := operation.client.get(call, "/entities?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return featuresBranch(ListFeaturesBranchDefect, sdkgo.FailureAuthentication, "Productboard credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[FeaturesPage](featuresFailure(sdkgo.FailureTransport, "Productboard request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[FeaturesPage](featuresFailure(sdkgo.FailureRateLimit, "Productboard rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[FeaturesPage](featuresFailure(sdkgo.FailureAvailability, "Productboard is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return featuresBranch(ListFeaturesBranchProviderRejected, sdkgo.FailureProviderRejection, "Productboard rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return featuresBranch(ListFeaturesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Productboard response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[FeaturesPage](featuresFailure(sdkgo.FailureTransport, "Productboard response was interrupted"), 0)
	}
	page, err := decodeFeaturesPage(response.body)
	if err != nil {
		return featuresBranch(ListFeaturesBranchInvalidResponse, sdkgo.FailureProtocol, "Productboard returned invalid feature JSON")
	}
	return sdkgo.NewQueryBranch(ListFeaturesBranchListed, page, nil, sdkgo.Receipt{})
}

func validCursor(cursor string) bool {
	if len(cursor) > 2048 || !utf8.ValidString(cursor) {
		return false
	}
	for _, character := range cursor {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func decodeFeaturesPage(body []byte) (FeaturesPage, error) {
	var wire struct {
		Data []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Fields struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"data"`
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return FeaturesPage{}, err
	}
	if wire.Data == nil {
		return FeaturesPage{}, fmt.Errorf("missing data array")
	}
	page := FeaturesPage{Features: make([]FeatureMetadata, 0, len(wire.Data))}
	for _, entity := range wire.Data {
		if entity.ID == "" || entity.Type != "feature" || strings.TrimSpace(entity.Fields.Name) == "" {
			return FeaturesPage{}, fmt.Errorf("missing feature identity or name")
		}
		page.Features = append(page.Features, FeatureMetadata{ID: entity.ID, Name: entity.Fields.Name})
	}
	if wire.Links.Next != "" {
		nextURL, err := url.Parse(wire.Links.Next)
		if err != nil || nextURL.Scheme != "https" || nextURL.Host != "api.productboard.com" || nextURL.Path != "/v2/entities" {
			return FeaturesPage{}, fmt.Errorf("invalid next page URL")
		}
		cursors := nextURL.Query()["pageCursor"]
		if len(cursors) != 1 || cursors[0] == "" || !validCursor(cursors[0]) {
			return FeaturesPage{}, fmt.Errorf("invalid next page cursor")
		}
		page.NextCursor = cursors[0]
	}
	return page, nil
}

func featuresFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "productboard", Operation: "listFeatures", Message: message}
}

func featuresBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[FeaturesPage] {
	failure := featuresFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, FeaturesPage{}, &failure, sdkgo.Receipt{})
}
