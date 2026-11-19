// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package klaviyo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListProfilesInput selects one page and optionally an exact email address.
type ListProfilesInput struct {
	// PageSize is 1–100; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the opaque cursor from the prior page.
	Cursor string `json:"cursor,omitempty"`
	// Email filters by exact address; blank lists every visible profile.
	Email string `json:"email,omitempty"`
}

// ProfilesPage contains profile identities and a possible continuation cursor.
type ProfilesPage struct {
	// Profiles are the returned profile IDs and email addresses.
	Profiles []Profile `json:"profiles"`
	// NextCursor selects the following page when nonempty.
	NextCursor string `json:"nextCursor"`
}

// ListProfilesOperation retrieves one page of profile identities.
type ListProfilesOperation struct{ client *Client }

// ListProfiles binds the Query to this client.
func (client *Client) ListProfiles() ListProfilesOperation { return ListProfilesOperation{client} }

// Definition returns the generated profile-list contract.
func (ListProfilesOperation) Definition() sdkgo.QueryDefinition { return ListProfilesDefinition }

// Invoke reads one page under the existing profiles:read scope.
func (operation ListProfilesOperation) Invoke(call sdkgo.Call, input ListProfilesInput) sdkgo.QueryAttempt[ProfilesPage] {
	const name = "listProfiles"
	if input.PageSize < 0 || input.PageSize > 100 || len(input.Cursor) > 2048 || strings.ContainsAny(input.Cursor, "\x00\r\n") ||
		(input.Email != "" && (!validEventEmail(input.Email) || len(input.Email) > 320)) {
		return sdkgo.NewQueryBranch(ListProfilesBranchDefect, ProfilesPage{}, klaviyoFailure(name, sdkgo.FailureValidation, "profile page or email filter is invalid"), sdkgo.Receipt{})
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"fields[profile]": {"email"}, "page[size]": {strconv.Itoa(input.PageSize)}}
	if input.Cursor != "" {
		query.Set("page[cursor]", input.Cursor)
	}
	if input.Email != "" {
		query.Set("filter", "equals(email,"+strconv.Quote(input.Email)+")")
	}
	request, failure := operation.client.newRequest(call, http.MethodGet, "/profiles?"+query.Encode(), nil, name)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListProfilesBranchDefect, ProfilesPage{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[ProfilesPage](*klaviyoFailure(name, sdkgo.FailureTransport, "Klaviyo profile list is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if retryStatus(response.StatusCode) {
		return sdkgo.NewQueryRetry[ProfilesPage](*klaviyoFailure(name, sdkgo.FailureAvailability, "Klaviyo profile list is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode != http.StatusOK {
		return sdkgo.NewQueryBranch(ListProfilesBranchProviderRejected, ProfilesPage{}, klaviyoFailure(name, rejectionKind(response.StatusCode), "Klaviyo rejected profile list with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryBranch(ListProfilesBranchInvalidResponse, ProfilesPage{}, klaviyoFailure(name, sdkgo.FailureResponseTooLarge, "Klaviyo profile list exceeded the limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[ProfilesPage](*klaviyoFailure(name, sdkgo.FailureTransport, "Klaviyo profile list was interrupted"), 0)
	}
	page, err := decodeProfilesPage(body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListProfilesBranchInvalidResponse, ProfilesPage{}, klaviyoFailure(name, sdkgo.FailureProtocol, "Klaviyo returned an invalid profile list"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListProfilesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeProfilesPage(body []byte) (ProfilesPage, error) {
	var wire struct {
		Data []struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Email string `json:"email"`
			} `json:"attributes"`
		} `json:"data"`
		Links struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
		return ProfilesPage{}, errors.New("profile page missing")
	}
	page := ProfilesPage{Profiles: make([]Profile, 0, len(wire.Data))}
	for _, profile := range wire.Data {
		if profile.Type != "profile" || profile.ID == "" {
			return ProfilesPage{}, errors.New("profile identity missing")
		}
		page.Profiles = append(page.Profiles, Profile{ID: profile.ID, Email: profile.Attributes.Email})
	}
	if wire.Links.Next != nil && *wire.Links.Next != "" {
		nextURL, err := url.Parse(*wire.Links.Next)
		if err != nil || (nextURL.Scheme != "https" && nextURL.Scheme != "http") || nextURL.Host == "" {
			return ProfilesPage{}, errors.New("invalid pagination link")
		}
		cursor := nextURL.Query().Get("page[cursor]")
		if cursor == "" || len(cursor) > 2048 {
			return ProfilesPage{}, errors.New("pagination cursor missing")
		}
		page.NextCursor = cursor
	}
	return page, nil
}
