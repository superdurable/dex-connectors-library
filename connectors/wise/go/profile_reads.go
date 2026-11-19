// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListProfilesInput bounds the number of profiles returned to the Flow.
type ListProfilesInput struct {
	// MaxProfiles is a local result cap from 1 to 100; Wise still returns the full list.
	MaxProfiles int `json:"maxProfiles"`
}

// ProfileSummary excludes names, addresses, and other personal details.
type ProfileSummary struct {
	// ID is the Wise numeric profile ID.
	ID string `json:"id"`
	// Type is the Wise profile type, such as business or personal.
	Type string `json:"type"`
}

// ProfilesOutput contains accessible profile summaries.
type ProfilesOutput struct {
	// Profiles is the sanitized profile list.
	Profiles []ProfileSummary `json:"profiles"`
}

// GetProfileInput selects one accessible Wise profile.
type GetProfileInput struct {
	// ProfileID is a positive decimal Wise profile ID.
	ProfileID string `json:"profileId"`
}

// ListProfilesOperation reads accessible profile metadata.
type ListProfilesOperation struct{ client *Client }

// ListProfiles binds the Query to this client.
func (client *Client) ListProfiles() ListProfilesOperation { return ListProfilesOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListProfilesOperation) Definition() sdkgo.QueryDefinition { return ListProfilesDefinition }

// Invoke lists profile IDs and types without exposing profile details.
func (operation ListProfilesOperation) Invoke(call sdkgo.Call, input ListProfilesInput) sdkgo.QueryAttempt[ProfilesOutput] {
	if input.MaxProfiles < 1 || input.MaxProfiles > 100 {
		return wiseReadBranch[ProfilesOutput]("listProfiles", sdkgo.DefectBranchID, sdkgo.FailureValidation, "maxProfiles must be 1 to 100")
	}
	return readWiseJSON(call, operation.client, "listProfiles", "/profiles", ListProfilesBranchListed, false, func(body []byte) (ProfilesOutput, error) {
		profiles, err := decodeProfiles(body)
		if err == nil && len(profiles.Profiles) > input.MaxProfiles {
			profiles.Profiles = profiles.Profiles[:input.MaxProfiles]
		}
		return profiles, err
	})
}

// GetProfileOperation reads one profile's metadata.
type GetProfileOperation struct{ client *Client }

// GetProfile binds the Query to this client.
func (client *Client) GetProfile() GetProfileOperation { return GetProfileOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetProfileOperation) Definition() sdkgo.QueryDefinition { return GetProfileDefinition }

// Invoke gets a profile ID and type without exposing profile details.
func (operation GetProfileOperation) Invoke(call sdkgo.Call, input GetProfileInput) sdkgo.QueryAttempt[ProfileSummary] {
	if !validTransferID(input.ProfileID) {
		return wiseReadBranch[ProfileSummary]("getProfile", sdkgo.DefectBranchID, sdkgo.FailureValidation, "profile ID must be a positive decimal integer")
	}
	return readWiseJSON(call, operation.client, "getProfile", "/profiles/"+url.PathEscape(input.ProfileID), GetProfileBranchFound, true, decodeProfile)
}

func readWiseJSON[T any](call sdkgo.Call, client *Client, operationID, path string, happyBranch sdkgo.BranchID, hasNotFound bool, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	response := client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return wiseReadBranch[T](operationID, sdkgo.DefectBranchID, sdkgo.FailureAuthentication, "Wise credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return wiseReadRetry[T](operationID, sdkgo.FailureTransport, "Wise request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return wiseReadRetry[T](operationID, sdkgo.FailureRateLimit, "Wise rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return wiseReadRetry[T](operationID, sdkgo.FailureAvailability, "Wise is temporarily unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound && hasNotFound {
		return wiseReadBranch[T](operationID, "notFound", sdkgo.FailureNotFound, "Wise did not expose the resource")
	}
	if response.status != http.StatusOK {
		return wiseReadBranch[T](operationID, "providerRejected", sdkgo.FailureProviderRejection, "Wise rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return wiseReadBranch[T](operationID, "invalidResponse", sdkgo.FailureResponseTooLarge, "Wise response exceeded the configured limit")
	}
	if response.err != nil {
		return wiseReadRetry[T](operationID, sdkgo.FailureTransport, "Wise response was interrupted", 0)
	}
	value, err := decode(response.body)
	if err != nil {
		return wiseReadBranch[T](operationID, "invalidResponse", sdkgo.FailureProtocol, "Wise returned invalid resource JSON")
	}
	return sdkgo.NewQueryBranch(happyBranch, value, nil, sdkgo.Receipt{})
}

func wiseReadBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wise", Operation: operationID, Message: message}
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func wiseReadRetry[T any](operationID string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "wise", Operation: operationID, Message: message}, delay)
}

func decodeProfile(body []byte) (ProfileSummary, error) {
	var wire struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ProfileSummary{}, err
	}
	if wire.ID <= 0 || strings.TrimSpace(wire.Type) == "" {
		return ProfileSummary{}, fmt.Errorf("profile ID or type missing")
	}
	return ProfileSummary{ID: strconv.FormatInt(wire.ID, 10), Type: wire.Type}, nil
}

func decodeProfiles(body []byte) (ProfilesOutput, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return ProfilesOutput{}, err
	}
	if entries == nil {
		return ProfilesOutput{}, fmt.Errorf("profile list missing")
	}
	profiles := make([]ProfileSummary, 0, len(entries))
	for _, entry := range entries {
		profile, err := decodeProfile(entry)
		if err != nil {
			return ProfilesOutput{}, err
		}
		profiles = append(profiles, profile)
	}
	return ProfilesOutput{Profiles: profiles}, nil
}
