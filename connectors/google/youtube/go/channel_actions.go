// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListChannelsInput selects one page of channels owned by the connected account.
type ListChannelsInput struct {
	// MaxResults bounds one page from 1 to 50; zero uses YouTube's default.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is an opaque cursor from a previous page.
	PageToken string `json:"pageToken,omitempty"`
}

// ChannelIdentity projects a channel without account-private settings.
type ChannelIdentity struct {
	// ID is the stable YouTube channel ID.
	ID string `json:"id"`
	// Title is untrusted provider text.
	Title string `json:"title"`
	// Description is untrusted provider text.
	Description string `json:"description"`
}

// ChannelsPage contains one bounded page of owned channels.
type ChannelsPage struct {
	// Channels are channel identities and display fields.
	Channels []ChannelIdentity `json:"channels"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// UpdateChannelInput replaces one channel's description.
type UpdateChannelInput struct {
	// ChannelID is the owned channel's UC-prefixed ID.
	ChannelID string `json:"channelId"`
	// Description is the complete replacement description; blank clears it.
	Description string `json:"description"`
}

// UpdatedChannelDescription confirms the channel's new description.
type UpdatedChannelDescription struct {
	// ID is the updated channel ID.
	ID string `json:"id"`
	// Description is the confirmed description.
	Description string `json:"description"`
}

// ListChannelsOperation lists owned channel identities.
type ListChannelsOperation struct{ client *Client }

// UpdateChannelOperation replaces a channel snippet.
type UpdateChannelOperation struct{ client *Client }

// ListChannels binds the owned channels Query.
func (client *Client) ListChannels() ListChannelsOperation { return ListChannelsOperation{client} }

// UpdateChannel binds the channel replacement Mutation.
func (client *Client) UpdateChannel() UpdateChannelOperation { return UpdateChannelOperation{client} }

var listChannelsBranches = operationBranches{notFound: ListChannelsBranchNotFound, quotaExhausted: ListChannelsBranchQuotaExhausted, rejected: ListChannelsBranchProviderRejected, invalidResponse: ListChannelsBranchInvalidResponse, defect: ListChannelsBranchDefect}
var updateChannelBranches = operationBranches{notFound: UpdateChannelBranchNotFound, quotaExhausted: UpdateChannelBranchQuotaExhausted, rejected: UpdateChannelBranchProviderRejected, invalidResponse: UpdateChannelBranchInvalidResponse, defect: UpdateChannelBranchDefect}

// Definition returns the generated channel list contract.
func (ListChannelsOperation) Definition() sdkgo.QueryDefinition { return ListChannelsDefinition }

// Definition returns the generated channel replacement contract.
func (UpdateChannelOperation) Definition() sdkgo.MutationDefinition { return UpdateChannelDefinition }

// IdempotencyKey uses the stable Step call ID for an absolute replacement.
func (UpdateChannelOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateChannelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists channels owned by the connected account.
func (operation ListChannelsOperation) Invoke(call sdkgo.Call, input ListChannelsInput) sdkgo.QueryAttempt[ChannelsPage] {
	query := url.Values{"part": {"snippet"}, "mine": {"true"}}
	if validatePageRequest(query, input.MaxResults, input.PageToken) != nil {
		return sdkgo.NewQueryBranch(ListChannelsBranchDefect, ChannelsPage{}, providerFailurePointer("listChannels", sdkgo.FailureValidation, "channel page is invalid"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "listChannels", providerRequest{method: http.MethodGet, path: "/channels", query: query}, "")
	if outcome != nil {
		return queryAttemptForOutcome[ChannelsPage](*outcome, listChannelsBranches)
	}
	receipt := operation.client.receipt(call, "", response.header)
	var decoded struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"snippet"`
		} `json:"items"`
		NextPageToken string `json:"nextPageToken"`
	}
	if decodeResponse(response.body, &decoded) != nil || len(decoded.Items) > MaximumPageSize || validateReturnedPageToken(decoded.NextPageToken) != nil {
		return sdkgo.NewQueryBranch(ListChannelsBranchInvalidResponse, ChannelsPage{}, providerFailurePointer("listChannels", sdkgo.FailureProtocol, "YouTube returned an invalid channel page"), receipt)
	}
	result := ChannelsPage{Channels: make([]ChannelIdentity, 0, len(decoded.Items)), NextPageToken: decoded.NextPageToken}
	for _, item := range decoded.Items {
		if !channelIDPattern.MatchString(item.ID) || item.Snippet.Title == "" {
			return sdkgo.NewQueryBranch(ListChannelsBranchInvalidResponse, ChannelsPage{}, providerFailurePointer("listChannels", sdkgo.FailureProtocol, "YouTube returned an invalid channel"), receipt)
		}
		result.Channels = append(result.Channels, ChannelIdentity{ID: item.ID, Title: item.Snippet.Title, Description: item.Snippet.Description})
	}
	return sdkgo.NewQueryBranch(ListChannelsBranchListed, result, nil, receipt)
}

// Invoke replaces one owned channel's title and description.
func (operation UpdateChannelOperation) Invoke(call sdkgo.Call, input UpdateChannelInput) sdkgo.MutationAttempt[UpdatedChannelDescription] {
	if !channelIDPattern.MatchString(input.ChannelID) {
		return sdkgo.NewMutationBranch(UpdateChannelBranchDefect, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureValidation, "channel ID is invalid"), sdkgo.Receipt{})
	}
	readResponse, outcome := operation.client.exchange(call, "updateChannel", providerRequest{method: http.MethodGet, path: "/channels", query: url.Values{"part": {"brandingSettings"}, "id": {input.ChannelID}}, requiresManage: true}, input.ChannelID)
	if outcome != nil {
		return mutationAttemptForOutcome[UpdatedChannelDescription](*outcome, updateChannelBranches)
	}
	readReceipt := operation.client.receipt(call, input.ChannelID, readResponse.header)
	var current struct {
		Items []struct {
			ID               string                     `json:"id"`
			BrandingSettings map[string]json.RawMessage `json:"brandingSettings"`
		} `json:"items"`
	}
	if decodeResponse(readResponse.body, &current) != nil || len(current.Items) > 1 {
		return sdkgo.NewMutationBranch(UpdateChannelBranchInvalidResponse, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureProtocol, "YouTube returned invalid channel branding"), readReceipt)
	}
	if len(current.Items) == 0 {
		return sdkgo.NewMutationBranch(UpdateChannelBranchNotFound, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureNotFound, "channel is not visible"), readReceipt)
	}
	if current.Items[0].ID != input.ChannelID {
		return sdkgo.NewMutationBranch(UpdateChannelBranchInvalidResponse, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureProtocol, "YouTube returned a different channel"), readReceipt)
	}
	branding := current.Items[0].BrandingSettings
	var channelSettings map[string]json.RawMessage
	if json.Unmarshal(branding["channel"], &channelSettings) != nil || channelSettings == nil {
		return sdkgo.NewMutationBranch(UpdateChannelBranchInvalidResponse, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureProtocol, "YouTube omitted channel branding settings"), readReceipt)
	}
	description, err := json.Marshal(input.Description)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateChannelBranchDefect, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureLocalDefect, "channel description could not be encoded"), sdkgo.Receipt{})
	}
	channelSettings["description"] = description
	branding["channel"], err = json.Marshal(channelSettings)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateChannelBranchDefect, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureLocalDefect, "channel branding could not be encoded"), sdkgo.Receipt{})
	}
	body, err := json.Marshal(map[string]any{"id": input.ChannelID, "brandingSettings": branding})
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateChannelBranchDefect, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureLocalDefect, "channel replacement could not be encoded"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "updateChannel", providerRequest{method: http.MethodPut, path: "/channels", query: url.Values{"part": {"brandingSettings"}}, body: body, requiresManage: true}, input.ChannelID)
	if outcome != nil {
		return mutationAttemptForOutcome[UpdatedChannelDescription](*outcome, updateChannelBranches)
	}
	receipt := operation.client.receipt(call, input.ChannelID, response.header)
	var updated struct {
		ID               string `json:"id"`
		BrandingSettings struct {
			Channel struct {
				Description string `json:"description"`
			} `json:"channel"`
		} `json:"brandingSettings"`
	}
	if decodeResponse(response.body, &updated) != nil || updated.ID != input.ChannelID || updated.BrandingSettings.Channel.Description != input.Description {
		return sdkgo.NewMutationBranch(UpdateChannelBranchInvalidResponse, UpdatedChannelDescription{}, providerFailurePointer("updateChannel", sdkgo.FailureProtocol, "YouTube returned a different channel description"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateChannelBranchUpdated, UpdatedChannelDescription{ID: input.ChannelID, Description: input.Description}, nil, receipt)
}
