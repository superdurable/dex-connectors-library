// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListSocialProfilesInput requests profiles visible to the connected user.
type ListSocialProfilesInput struct{}

// SocialProfile contains one visible publishing channel.
type SocialProfile struct {
	// ID is Hootsuite's stable social profile ID.
	ID string `json:"id"`
	// Type is the connected social network, such as FACEBOOK.
	Type string `json:"type"`
	// Username is the account label shown by Hootsuite.
	Username string `json:"username"`
}

// SocialProfilesPage contains the bounded list returned by Hootsuite.
type SocialProfilesPage struct {
	// Profiles are the connected user's visible social profiles.
	Profiles []SocialProfile `json:"profiles"`
}

// ListSocialProfilesOperation reads the connected user's social profiles.
type ListSocialProfilesOperation struct{ client *Client }

// ListSocialProfiles binds the profile Query to this client.
func (client *Client) ListSocialProfiles() ListSocialProfilesOperation {
	return ListSocialProfilesOperation{client}
}

// Definition returns the manifest-generated profile Query contract.
func (ListSocialProfilesOperation) Definition() sdkgo.QueryDefinition {
	return ListSocialProfilesDefinition
}

// Invoke reads visible social profiles and projects only identity and network fields.
func (operation ListSocialProfilesOperation) Invoke(call sdkgo.Call, _ ListSocialProfilesInput) sdkgo.QueryAttempt[SocialProfilesPage] {
	response := operation.client.get(call, "/v1/socialProfiles")
	if errors.Is(response.err, errCredential) {
		return profileBranch(ListSocialProfilesBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return profileRetry(sdkgo.FailureTransport, "Hootsuite request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return profileRetry(sdkgo.FailureRateLimit, "Hootsuite rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return profileRetry(sdkgo.FailureAvailability, "Hootsuite is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return profileBranch(ListSocialProfilesBranchProviderRejected, sdkgo.FailureProviderRejection, "Hootsuite rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return profileBranch(ListSocialProfilesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Hootsuite response exceeded the configured limit")
	}
	if response.err != nil {
		return profileRetry(sdkgo.FailureTransport, "Hootsuite response was interrupted", 0)
	}
	var wire struct {
		Data []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Username string `json:"socialNetworkUsername"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data == nil || len(wire.Data) > 1000 {
		return profileBranch(ListSocialProfilesBranchInvalidResponse, sdkgo.FailureProtocol, "Hootsuite returned invalid social profile JSON")
	}
	page := SocialProfilesPage{Profiles: make([]SocialProfile, 0, len(wire.Data))}
	for _, profile := range wire.Data {
		if profile.ID == "" || profile.Type == "" || profile.Username == "" {
			return profileBranch(ListSocialProfilesBranchInvalidResponse, sdkgo.FailureProtocol, "Hootsuite returned incomplete social profile metadata")
		}
		page.Profiles = append(page.Profiles, SocialProfile{ID: profile.ID, Type: profile.Type, Username: profile.Username})
	}
	return sdkgo.NewQueryBranch(ListSocialProfilesBranchListed, page, nil, sdkgo.Receipt{})
}

func profileBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SocialProfilesPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "listSocialProfiles", Message: message}
	return sdkgo.NewQueryBranch(branch, SocialProfilesPage{}, &failure, sdkgo.Receipt{})
}

func profileRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SocialProfilesPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "listSocialProfiles", Message: message}
	return sdkgo.NewQueryRetry[SocialProfilesPage](failure, delay)
}
