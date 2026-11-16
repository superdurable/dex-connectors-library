// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// profileCursorVersion versions the committed cursor token; another value records a new baseline.
const profileCursorVersion = 1

// profileFetcher compares each UserInfo profile with the last; LinkedIn has no profile change feed.
type profileFetcher struct {
	client *Client
	call   sdkgo.Call
}

// profileCursor is the committed token; Sequence counts delivered changes for event IDs.
type profileCursor struct {
	Version  int           `json:"version"`
	Subject  string        `json:"subject"`
	Hash     string        `json:"hash"`
	Sequence int64         `json:"sequence,omitempty"`
	Profile  ProfileClaims `json:"profile"`
}

func newProfileFetcher(client *Client, connection sdkgo.ConnectionRef) *profileFetcher {
	return &profileFetcher{
		client: client, call: pollingtrigger.NewTriggerCredentialCall(connection, ProfileChangedTriggerDefinition.Trigger),
	}
}

// SeedCursor reads the current profile and keeps it as the baseline without delivering it.
func (fetcher *profileFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	profile, err := fetcher.readProfile(ctx)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	hash, err := profileHash(profile)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return encodeProfileCursor(profileCursor{Version: profileCursorVersion, Subject: profile.Subject, Hash: hash, Profile: profile})
}

// FetchChanges returns one change when the profile hash moved; another member's profile or an
// undecodable cursor reseeds.
func (fetcher *profileFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ProfileChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ProfileChangedEvent]]
	var position profileCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.Version != profileCursorVersion ||
		position.Subject == "" || position.Subject != position.Profile.Subject || position.Hash == "" {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("LinkedIn profile cursor is not decodable"))
	}
	profile, err := fetcher.readProfile(ctx)
	if err != nil {
		return page, err
	}
	if profile.Subject != position.Subject {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("LinkedIn connection now belongs to another member than the stored profile"))
	}
	hash, err := profileHash(profile)
	if err != nil {
		return page, err
	}
	next := position
	// An unchanged hash still stores the latest picture URL, whose signed query LinkedIn may have renewed.
	next.Profile = profile
	if hash != position.Hash {
		page.Changes = append(page.Changes, newProfileChangedTriggerEvent(
			position.Hash, hash, position.Sequence, position.Profile, profile, fetcher.client.now()))
		next.Hash, next.Sequence = hash, position.Sequence+1
	}
	page.Next, err = encodeProfileCursor(next)
	return page, err
}

// readProfile sends one UserInfo request and maps LinkedIn's statuses to the poll kit's signals.
func (fetcher *profileFetcher) readProfile(ctx context.Context) (ProfileClaims, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return ProfileClaims{}, err
	}
	credentials, err := sdkgo.ResolveCredential(ctx, fetcher.client.credentials, fetcher.call, fetcher.client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return ProfileClaims{}, errors.New("LinkedIn connection requires reauthorization; reconnect the connection")
	case err != nil || validateResolvedCredentials(credentials) != nil:
		return ProfileClaims{}, errors.New("LinkedIn Trigger credentials are unavailable")
	}
	response, err := fetcher.client.get(ctx, fetcher.call, &credentials)
	switch {
	case errors.Is(err, errResponseTooLarge):
		return ProfileClaims{}, errors.New("LinkedIn UserInfo response exceeds the configured size limit")
	case err != nil:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ProfileClaims{}, ctxErr
		}
		return ProfileClaims{}, errors.New("LinkedIn UserInfo request could not reach LinkedIn")
	}
	switch {
	case response.statusCode == http.StatusTooManyRequests:
		// LinkedIn limits are daily; without Retry-After the runner backs off exponentially.
		return ProfileClaims{}, pollingtrigger.MarkRateLimited(errors.New("LinkedIn rate limit was reached"),
			providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), fetcher.client.now()))
	case response.statusCode == http.StatusUnauthorized:
		return ProfileClaims{}, errors.New("LinkedIn authorization is invalid or revoked; reconnect the connection")
	case response.statusCode == http.StatusForbidden:
		return ProfileClaims{}, errors.New("LinkedIn authorization is forbidden or lacks the openid or profile scope")
	case response.statusCode < 200 || response.statusCode >= 300:
		return ProfileClaims{}, fmt.Errorf("LinkedIn UserInfo request returned HTTP %d", response.statusCode)
	}
	var claims userInfo
	if err := decodeJSON(response.body, &claims); err != nil {
		return ProfileClaims{}, errors.New("LinkedIn returned an invalid UserInfo response")
	}
	profile, isValid := profileClaimsFromUserInfo(claims)
	if !isValid {
		return ProfileClaims{}, errors.New("LinkedIn returned a UserInfo response without a valid sub claim")
	}
	return profile, nil
}

func encodeProfileCursor(position profileCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode LinkedIn profile cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
