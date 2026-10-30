// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetVideoInput identifies one video.
type GetVideoInput struct {
	// VideoID is the 11-character video ID, such as dQw4w9WgXcQ from youtube.com/watch?v=dQw4w9WgXcQ.
	VideoID string `json:"videoId"`
}

// Video is one video's details. Title and Description are untrusted provider
// text: escape them before rendering HTML.
type Video struct {
	// ID is the 11-character video ID.
	ID string `json:"id"`
	// ChannelID is the ID of the channel that published the video.
	ChannelID string `json:"channelId"`
	// ChannelTitle is that channel's display name.
	ChannelTitle string `json:"channelTitle"`
	// Title is the video title.
	Title string `json:"title"`
	// Description is the video description.
	Description string `json:"description"`
	// PublishedAt is when the video was published, in UTC.
	PublishedAt time.Time `json:"publishedAt"`
	// Duration is the ISO 8601 duration, such as PT4M13S; blank for an upcoming live stream.
	Duration string `json:"duration,omitempty"`
	// PrivacyStatus is public, unlisted, or private.
	PrivacyStatus string `json:"privacyStatus"`
	// ViewCount is the view count, or nil when YouTube does not report it.
	ViewCount *int64 `json:"viewCount,omitempty"`
	// LikeCount is the like count, or nil when the owner hides it.
	LikeCount *int64 `json:"likeCount,omitempty"`
	// CommentCount is the comment count, or nil when comments are disabled.
	CommentCount *int64 `json:"commentCount,omitempty"`
}

type providerVideoList struct {
	Items *[]providerVideo `json:"items"`
}

type providerVideo struct {
	ID      string `json:"id"`
	Snippet struct {
		ChannelID    string `json:"channelId"`
		ChannelTitle string `json:"channelTitle"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		PublishedAt  string `json:"publishedAt"`
	} `json:"snippet"`
	ContentDetails struct {
		Duration string `json:"duration"`
	} `json:"contentDetails"`
	Status struct {
		PrivacyStatus string `json:"privacyStatus"`
	} `json:"status"`
	Statistics struct {
		ViewCount    *string `json:"viewCount"`
		LikeCount    *string `json:"likeCount"`
		CommentCount *string `json:"commentCount"`
	} `json:"statistics"`
}

// GetVideoOperation implements the getVideo connector Query. Each call costs one quota unit.
type GetVideoOperation struct{ client *Client }

var getVideoBranches = operationBranches{
	notFound: GetVideoBranchNotFound, quotaExhausted: GetVideoBranchQuotaExhausted, rejected: GetVideoBranchProviderRejected,
	invalidResponse: GetVideoBranchInvalidResponse, defect: GetVideoBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetVideoOperation) Definition() sdkgo.QueryDefinition { return GetVideoDefinition }

// Invoke sends one videos.list read and classifies its attempt. YouTube
// answers an unknown or private video with an empty list, which selects notFound.
func (operation GetVideoOperation) Invoke(call sdkgo.Call, input GetVideoInput) sdkgo.QueryAttempt[Video] {
	if !videoIDPattern.MatchString(input.VideoID) {
		return sdkgo.NewQueryBranch(GetVideoBranchDefect, Video{}, providerFailurePointer("getVideo", sdkgo.FailureValidation, "videoId must be an 11-character YouTube video ID"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "getVideo", providerRequest{
		method: http.MethodGet, path: "/videos",
		query: url.Values{"id": {input.VideoID}, "part": {"snippet,contentDetails,status,statistics"}},
	}, input.VideoID)
	if outcome != nil {
		return queryAttemptForOutcome[Video](*outcome, getVideoBranches)
	}
	receipt := operation.client.receipt(call, input.VideoID, response.header)
	var decoded providerVideoList
	if err := decodeResponse(response.body, &decoded); err != nil || decoded.Items == nil {
		return sdkgo.NewQueryBranch(GetVideoBranchInvalidResponse, Video{}, providerFailurePointer("getVideo", sdkgo.FailureProtocol, "YouTube returned a malformed video list"), receipt)
	}
	if len(*decoded.Items) == 0 {
		return sdkgo.NewQueryBranch(GetVideoBranchNotFound, Video{}, providerFailurePointer("getVideo", sdkgo.FailureNotFound, "no visible YouTube video has the ID"), receipt)
	}
	video, err := convertProviderVideo((*decoded.Items)[0], input.VideoID)
	if err != nil || len(*decoded.Items) != 1 {
		return sdkgo.NewQueryBranch(GetVideoBranchInvalidResponse, Video{}, providerFailurePointer("getVideo", sdkgo.FailureProtocol, "YouTube returned a different or invalid video"), receipt)
	}
	return sdkgo.NewQueryBranch(GetVideoBranchFound, video, nil, receipt)
}

func convertProviderVideo(video providerVideo, videoID string) (Video, error) {
	if video.ID != videoID || !channelIDPattern.MatchString(video.Snippet.ChannelID) {
		return Video{}, errors.New("YouTube returned a different video or an invalid channel ID")
	}
	publishedAt, err := parseOptionalTime(video.Snippet.PublishedAt)
	if err != nil || publishedAt == nil {
		return Video{}, errors.New("YouTube returned a video without a publish time")
	}
	converted := Video{
		ID: video.ID, ChannelID: video.Snippet.ChannelID, ChannelTitle: video.Snippet.ChannelTitle, Title: video.Snippet.Title,
		Description: video.Snippet.Description, PublishedAt: *publishedAt, Duration: video.ContentDetails.Duration,
		PrivacyStatus: video.Status.PrivacyStatus,
	}
	for _, statistic := range []struct {
		value       *string
		destination **int64
	}{
		{video.Statistics.ViewCount, &converted.ViewCount},
		{video.Statistics.LikeCount, &converted.LikeCount},
		{video.Statistics.CommentCount, &converted.CommentCount},
	} {
		if *statistic.destination, err = parseOptionalCount(statistic.value); err != nil {
			return Video{}, err
		}
	}
	return converted, nil
}
