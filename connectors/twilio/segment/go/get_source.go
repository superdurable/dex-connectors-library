// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package segment

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetSourceInput selects a Source by its non-secret Segment ID.
type GetSourceInput struct {
	// SourceID is the opaque ID returned by the Segment Public API.
	SourceID string `json:"sourceId"`
}

// SourceProfile excludes write keys and Source settings.
type SourceProfile struct {
	// ID is the Segment Source ID.
	ID string `json:"id"`
	// Slug is the Source's human-readable identifier.
	Slug string `json:"slug"`
	// Name is the optional display name, which may be blank.
	Name string `json:"name"`
}

// GetSourceOperation reads one source's identity.
type GetSourceOperation struct{ client *Client }

// GetSource binds the Query to this client.
func (client *Client) GetSource() GetSourceOperation {
	return GetSourceOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetSourceOperation) Definition() sdkgo.QueryDefinition { return GetSourceDefinition }

// Invoke reads one source and classifies retryable and definitive outcomes.
func (operation GetSourceOperation) Invoke(call sdkgo.Call, input GetSourceInput) sdkgo.QueryAttempt[SourceProfile] {
	if !validSourceID(input.SourceID) {
		return sourceBranch(GetSourceBranchDefect, sdkgo.FailureValidation, "sourceId must contain 1–255 URL-safe characters")
	}
	response := operation.client.get(call, "/sources/"+url.PathEscape(input.SourceID))
	if errors.Is(response.err, errCredential) {
		return sourceBranch(GetSourceBranchDefect, sdkgo.FailureAuthentication, "Segment credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SourceProfile](sourceFailure(sdkgo.FailureTransport, "Segment request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SourceProfile](sourceFailure(sdkgo.FailureRateLimit, "Segment rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SourceProfile](sourceFailure(sdkgo.FailureAvailability, "Segment is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sourceBranch(GetSourceBranchNotFound, sdkgo.FailureNotFound, "Segment did not expose the source")
	}
	if response.status != http.StatusOK {
		return sourceBranch(GetSourceBranchProviderRejected, sdkgo.FailureProviderRejection, "Segment rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sourceBranch(GetSourceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Segment response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SourceProfile](sourceFailure(sdkgo.FailureTransport, "Segment response was interrupted"), 0)
	}
	sourceProfile, err := decodeSource(response.body, input.SourceID)
	if err != nil {
		return sourceBranch(GetSourceBranchInvalidResponse, sdkgo.FailureProtocol, "Segment returned invalid source JSON")
	}
	return sdkgo.NewQueryBranch(GetSourceBranchFound, sourceProfile, nil, sdkgo.Receipt{})
}

func validSourceID(sourceID string) bool {
	if len(sourceID) == 0 || len(sourceID) > 255 {
		return false
	}
	for _, character := range sourceID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeSource(body []byte, requestedSourceID string) (SourceProfile, error) {
	var wire struct {
		Data struct {
			Source struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
				Name *string `json:"name"`
			} `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SourceProfile{}, err
	}
	if wire.Data.Source.ID != requestedSourceID || strings.TrimSpace(wire.Data.Source.Slug) == "" || wire.Data.Source.Name == nil {
		return SourceProfile{}, fmt.Errorf("source identity, slug, or name missing")
	}
	return SourceProfile{ID: wire.Data.Source.ID, Slug: wire.Data.Source.Slug, Name: *wire.Data.Source.Name}, nil
}

func sourceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "segment", Operation: "getSource", Message: message}
}

func sourceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SourceProfile] {
	failure := sourceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, SourceProfile{}, &failure, sdkgo.Receipt{})
}
