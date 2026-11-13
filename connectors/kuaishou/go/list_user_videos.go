// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaishou

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListUserVideosInput selects a page; empty cursor starts at the first page.
type ListUserVideosInput struct {
	// Cursor is the photo ID returned as nextCursor by the previous page.
	Cursor string `json:"cursor"`
}

// UserVideo is bounded metadata for one authorized video.
type UserVideo struct {
	// PhotoID is Kuaishou's stable video identifier.
	PhotoID string `json:"photoID"`
	// Caption is the video's title or caption.
	Caption string `json:"caption"`
	// CreatedAtMilliseconds is the provider's creation time in Unix milliseconds.
	CreatedAtMilliseconds int64 `json:"createdAtMilliseconds"`
	// LikeCount is the provider's reported like total.
	LikeCount int64 `json:"likeCount"`
	// CommentCount is the provider's reported comment total.
	CommentCount int64 `json:"commentCount"`
	// ViewCount is the provider's reported view total.
	ViewCount int64 `json:"viewCount"`
	// Pending reports whether the video is still processing.
	Pending bool `json:"pending"`
}

// UserVideosPage is one bounded page and a cursor for another request.
type UserVideosPage struct {
	// Videos are the records returned by the provider.
	Videos []UserVideo `json:"videos"`
	// NextCursor is the final photo ID, or empty when this page has no videos.
	NextCursor string `json:"nextCursor"`
}

// ListUserVideosOperation reads one page of the authorized user's videos.
type ListUserVideosOperation struct{ client *Client }

// ListUserVideos binds the Query to this client.
func (client *Client) ListUserVideos() ListUserVideosOperation {
	return ListUserVideosOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (ListUserVideosOperation) Definition() sdkgo.QueryDefinition { return ListUserVideosDefinition }

// Invoke queries Kuaishou and classifies the provider outcome.
func (operation ListUserVideosOperation) Invoke(call sdkgo.Call, input ListUserVideosInput) sdkgo.QueryAttempt[UserVideosPage] {
	response := operation.client.listUserVideos(call, input)
	if errors.Is(response.err, errInput) {
		return videosBranch(ListUserVideosBranchDefect, sdkgo.FailureValidation, "Kuaishou cursor is invalid")
	}
	if errors.Is(response.err, errCredential) {
		return videosBranch(ListUserVideosBranchDefect, sdkgo.FailureAuthentication, "Kuaishou credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[UserVideosPage](videosFailure(sdkgo.FailureTransport, "Kuaishou request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UserVideosPage](videosFailure(sdkgo.FailureRateLimit, "Kuaishou rate limited the list query"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[UserVideosPage](videosFailure(sdkgo.FailureAvailability, "Kuaishou is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return videosBranch(ListUserVideosBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Kuaishou response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[UserVideosPage](videosFailure(sdkgo.FailureTransport, "Kuaishou response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return videosBranch(ListUserVideosBranchProviderRejected, sdkgo.FailureProviderRejection, "Kuaishou rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return videosBranch(ListUserVideosBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaishou returned invalid UTF-8")
	}
	var wire struct {
		Result *int `json:"result"`
		Videos *[]struct {
			PhotoID               string `json:"photo_id"`
			Caption               string `json:"caption"`
			CreatedAtMilliseconds int64  `json:"create_time"`
			LikeCount             int64  `json:"like_count"`
			CommentCount          int64  `json:"comment_count"`
			ViewCount             int64  `json:"view_count"`
			Pending               bool   `json:"pending"`
		} `json:"video_list"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return videosBranch(ListUserVideosBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaishou returned invalid video JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return videosBranch(ListUserVideosBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaishou returned trailing JSON")
	}
	if wire.Result == nil {
		return videosBranch(ListUserVideosBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaishou returned no result code")
	}
	if *wire.Result != 1 {
		return videosBranch(ListUserVideosBranchProviderRejected, sdkgo.FailureProviderRejection, "Kuaishou rejected code "+strconv.Itoa(*wire.Result))
	}
	if wire.Videos == nil || len(*wire.Videos) > operation.client.pageSize {
		return videosBranch(ListUserVideosBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaishou returned incomplete or oversized video list")
	}
	page := UserVideosPage{Videos: make([]UserVideo, 0, len(*wire.Videos))}
	for _, video := range *wire.Videos {
		if !isOpaqueValue(video.PhotoID, 128) || video.CreatedAtMilliseconds < 0 || video.LikeCount < 0 || video.CommentCount < 0 || video.ViewCount < 0 {
			return videosBranch(ListUserVideosBranchInvalidResponse, sdkgo.FailureProtocol, "Kuaishou returned invalid video metadata")
		}
		page.Videos = append(page.Videos, UserVideo{PhotoID: video.PhotoID, Caption: video.Caption, CreatedAtMilliseconds: video.CreatedAtMilliseconds, LikeCount: video.LikeCount, CommentCount: video.CommentCount, ViewCount: video.ViewCount, Pending: video.Pending})
		page.NextCursor = video.PhotoID
	}
	return sdkgo.NewQueryBranch(ListUserVideosBranchListed, page, nil, sdkgo.Receipt{})
}
func videosFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "kuaishou", Operation: "listUserVideos", Message: message}
}
func videosBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserVideosPage] {
	failure := videosFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserVideosPage{}, &failure, sdkgo.Receipt{})
}
