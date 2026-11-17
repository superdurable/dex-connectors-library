// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package instagrambusiness

import (
	"encoding/json"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetAccountInput identifies the professional account linked to a Facebook Page.
type GetAccountInput struct {
	// IGUserID is the numeric Instagram account ID from the linked Page.
	IGUserID string `json:"igUserId"`
}

// InstagramAccount contains public account metadata.
type InstagramAccount struct {
	// ID is the account's stable numeric identifier.
	ID string `json:"id"`
	// Username is the account's public handle.
	Username string `json:"username"`
	// MediaCount is Meta's published media count.
	MediaCount int64 `json:"mediaCount"`
}

// GetAccountOperation reads account metadata.
type GetAccountOperation struct{ client *Client }

// GetAccount binds the account Query to this client.
func (client *Client) GetAccount() GetAccountOperation { return GetAccountOperation{client} }

// Definition returns the generated account Query contract.
func (GetAccountOperation) Definition() sdkgo.QueryDefinition { return GetAccountDefinition }

// Invoke reads only public account identity and count fields.
func (operation GetAccountOperation) Invoke(call sdkgo.Call, input GetAccountInput) sdkgo.QueryAttempt[InstagramAccount] {
	if !numericID.MatchString(input.IGUserID) {
		return queryBranch("getAccount", GetAccountBranchDefect, InstagramAccount{}, sdkgo.FailureValidation, "Instagram account ID is invalid")
	}
	query := url.Values{"fields": {"id,username,media_count"}}
	response := operation.client.get(call, "/"+input.IGUserID+"?"+query.Encode())
	if decision, handled := classifyGraphResponse("getAccount", response, InstagramAccount{}, GetAccountBranchNotFound, GetAccountBranchProviderRejected, GetAccountBranchInvalidResponse, GetAccountBranchDefect); handled {
		return decision
	}
	var wire struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		MediaCount *int64 `json:"media_count"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !numericID.MatchString(wire.ID) || wire.Username == "" || wire.MediaCount == nil || *wire.MediaCount < 0 {
		return queryBranch("getAccount", GetAccountBranchInvalidResponse, InstagramAccount{}, sdkgo.FailureProtocol, "Meta returned invalid Instagram account JSON")
	}
	return sdkgo.NewQueryBranch(GetAccountBranchFound, InstagramAccount{ID: wire.ID, Username: wire.Username, MediaCount: *wire.MediaCount}, nil, sdkgo.Receipt{})
}

// GetMediaInput identifies one media object owned by an accessible account.
type GetMediaInput struct {
	// MediaID is the numeric Instagram media ID.
	MediaID string `json:"mediaId"`
}

// GetMediaOperation reads one media object's metadata.
type GetMediaOperation struct{ client *Client }

// GetMedia binds the media Query to this client.
func (client *Client) GetMedia() GetMediaOperation { return GetMediaOperation{client} }

// Definition returns the generated media Query contract.
func (GetMediaOperation) Definition() sdkgo.QueryDefinition { return GetMediaDefinition }

// Invoke reads identity, caption, type, timestamp, and permalink.
func (operation GetMediaOperation) Invoke(call sdkgo.Call, input GetMediaInput) sdkgo.QueryAttempt[InstagramMedia] {
	if !numericID.MatchString(input.MediaID) {
		return queryBranch("getMedia", GetMediaBranchDefect, InstagramMedia{}, sdkgo.FailureValidation, "Instagram media ID is invalid")
	}
	query := url.Values{"fields": {"id,caption,media_type,timestamp,permalink"}}
	response := operation.client.get(call, "/"+input.MediaID+"?"+query.Encode())
	if decision, handled := classifyGraphResponse("getMedia", response, InstagramMedia{}, GetMediaBranchNotFound, GetMediaBranchProviderRejected, GetMediaBranchInvalidResponse, GetMediaBranchDefect); handled {
		return decision
	}
	var wire mediaWire
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return queryBranch("getMedia", GetMediaBranchInvalidResponse, InstagramMedia{}, sdkgo.FailureProtocol, "Meta returned invalid Instagram media JSON")
	}
	media, valid := projectMedia(wire)
	if !valid {
		return queryBranch("getMedia", GetMediaBranchInvalidResponse, InstagramMedia{}, sdkgo.FailureProtocol, "Meta returned incomplete media metadata")
	}
	return sdkgo.NewQueryBranch(GetMediaBranchFound, media, nil, sdkgo.Receipt{})
}
