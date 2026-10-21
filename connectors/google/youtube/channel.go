// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetChannelInput selects one channel. Set exactly one field.
type GetChannelInput struct {
	// ChannelID is the channel ID, UC followed by 22 characters, from youtube.com/channel/<ID>.
	ChannelID string `json:"channelId,omitempty"`
	// Handle is the channel handle with or without @, such as @GoogleDevelopers.
	Handle string `json:"handle,omitempty"`
	// Mine reads the authorized account's own channel.
	Mine bool `json:"mine,omitempty"`
}

// Channel is one channel's details. Title and Description are untrusted provider text.
type Channel struct {
	// ID is the channel ID.
	ID string `json:"id"`
	// Title is the channel's display name.
	Title string `json:"title"`
	// Description is the channel description.
	Description string `json:"description"`
	// CustomURL is the channel's handle, such as @googledevelopers; blank when it has none.
	CustomURL string `json:"customUrl,omitempty"`
	// PublishedAt is when the channel was created, in UTC.
	PublishedAt time.Time `json:"publishedAt"`
	// UploadsPlaylistID lists the channel's public uploads, newest first, for ListPlaylistItems.
	UploadsPlaylistID string `json:"uploadsPlaylistId"`
	// SubscriberCount is the rounded subscriber count, or nil when the owner hides it.
	SubscriberCount *int64 `json:"subscriberCount,omitempty"`
	// VideoCount is the number of public videos, or nil when YouTube does not report it.
	VideoCount *int64 `json:"videoCount,omitempty"`
	// ViewCount is the channel's total views, or nil when YouTube does not report it.
	ViewCount *int64 `json:"viewCount,omitempty"`
}

type providerChannelList struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			CustomURL   string `json:"customUrl"`
			PublishedAt string `json:"publishedAt"`
		} `json:"snippet"`
		ContentDetails struct {
			RelatedPlaylists struct {
				Uploads string `json:"uploads"`
			} `json:"relatedPlaylists"`
		} `json:"contentDetails"`
		Statistics struct {
			SubscriberCount       *string `json:"subscriberCount"`
			HiddenSubscriberCount bool    `json:"hiddenSubscriberCount"`
			VideoCount            *string `json:"videoCount"`
			ViewCount             *string `json:"viewCount"`
		} `json:"statistics"`
	} `json:"items"`
}

// GetChannelOperation implements the getChannel connector Query. Each call costs one quota unit.
type GetChannelOperation struct{ client *Client }

var getChannelBranches = operationBranches{
	notFound: GetChannelBranchNotFound, quotaExhausted: GetChannelBranchQuotaExhausted, rejected: GetChannelBranchProviderRejected,
	invalidResponse: GetChannelBranchInvalidResponse, defect: GetChannelBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetChannelOperation) Definition() sdkgo.QueryDefinition { return GetChannelDefinition }

// Invoke sends one channels.list read and classifies its attempt. YouTube
// answers an unknown channel or handle with no items, which selects notFound.
func (operation GetChannelOperation) Invoke(call sdkgo.Call, input GetChannelInput) sdkgo.QueryAttempt[Channel] {
	query, objectID, err := encodeGetChannelQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetChannelBranchDefect, Channel{}, providerFailurePointer("getChannel", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "getChannel", providerRequest{method: http.MethodGet, path: "/channels", query: query}, objectID)
	if outcome != nil {
		return queryAttemptForOutcome[Channel](*outcome, getChannelBranches)
	}
	receipt := operation.client.receipt(call, objectID, response.header)
	var decoded providerChannelList
	if err := decodeResponse(response.body, &decoded); err != nil {
		return sdkgo.NewQueryBranch(GetChannelBranchInvalidResponse, Channel{}, providerFailurePointer("getChannel", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	if len(decoded.Items) == 0 {
		return sdkgo.NewQueryBranch(GetChannelBranchNotFound, Channel{}, providerFailurePointer("getChannel", sdkgo.FailureNotFound, "no YouTube channel matches the request"), receipt)
	}
	channel, err := convertProviderChannel(decoded, input.ChannelID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetChannelBranchInvalidResponse, Channel{}, providerFailurePointer("getChannel", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	receipt.ProviderObjectID = channel.ID
	return sdkgo.NewQueryBranch(GetChannelBranchFound, channel, nil, receipt)
}

func encodeGetChannelQuery(input GetChannelInput) (url.Values, string, error) {
	query := url.Values{"part": {"snippet,contentDetails,statistics"}}
	selectors := 0
	objectID := ""
	if input.ChannelID != "" {
		selectors++
		if !channelIDPattern.MatchString(input.ChannelID) {
			return nil, "", errors.New("channelId must be a YouTube channel ID: UC followed by 22 characters")
		}
		query.Set("id", input.ChannelID)
		objectID = input.ChannelID
	}
	if input.Handle != "" {
		selectors++
		if !handlePattern.MatchString(input.Handle) {
			return nil, "", errors.New("handle must be a YouTube handle of 3 to 30 letters, digits, periods, hyphens, or underscores")
		}
		handle := "@" + strings.TrimPrefix(input.Handle, "@")
		query.Set("forHandle", handle)
		objectID = handle
	}
	if input.Mine {
		selectors++
		query.Set("mine", "true")
	}
	if selectors != 1 {
		return nil, "", errors.New("set exactly one of channelId, handle, and mine")
	}
	return query, objectID, nil
}

func convertProviderChannel(decoded providerChannelList, requestedID string) (Channel, error) {
	if len(decoded.Items) != 1 {
		return Channel{}, errors.New("YouTube returned more than one channel")
	}
	item := decoded.Items[0]
	if !channelIDPattern.MatchString(item.ID) || (requestedID != "" && item.ID != requestedID) {
		return Channel{}, errors.New("YouTube returned a different channel or an invalid channel ID")
	}
	if !playlistIDPattern.MatchString(item.ContentDetails.RelatedPlaylists.Uploads) {
		return Channel{}, errors.New("YouTube returned a channel without an uploads playlist")
	}
	publishedAt, err := parseOptionalTime(item.Snippet.PublishedAt)
	if err != nil || publishedAt == nil {
		return Channel{}, errors.New("YouTube returned a channel without a creation time")
	}
	channel := Channel{
		ID: item.ID, Title: item.Snippet.Title, Description: item.Snippet.Description, CustomURL: item.Snippet.CustomURL,
		PublishedAt: *publishedAt, UploadsPlaylistID: item.ContentDetails.RelatedPlaylists.Uploads,
	}
	subscribers := item.Statistics.SubscriberCount
	if item.Statistics.HiddenSubscriberCount {
		subscribers = nil
	}
	if channel.SubscriberCount, err = parseOptionalCount(subscribers); err != nil {
		return Channel{}, err
	}
	if channel.VideoCount, err = parseOptionalCount(item.Statistics.VideoCount); err != nil {
		return Channel{}, err
	}
	if channel.ViewCount, err = parseOptionalCount(item.Statistics.ViewCount); err != nil {
		return Channel{}, err
	}
	return channel, nil
}
