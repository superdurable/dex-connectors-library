// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var regionCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)
var categoryIDPattern = regexp.MustCompile(`^[0-9]{1,12}$`)

// ListVideosInput selects one page of the most popular videos in a region.
type ListVideosInput struct {
	// RegionCode is the ISO 3166-1 alpha-2 country code, such as US.
	RegionCode string `json:"regionCode"`
	// MaxResults bounds one page from 1 to 50; zero uses YouTube's default.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is the opaque cursor from a previous page.
	PageToken string `json:"pageToken,omitempty"`
}

// VideosPage contains one page of popular video metadata.
type VideosPage struct {
	// Videos are projected video details.
	Videos []Video `json:"videos"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListVideoCategoriesInput selects the assignable categories for a country.
type ListVideoCategoriesInput struct {
	// RegionCode is the ISO 3166-1 alpha-2 country code, such as US.
	RegionCode string `json:"regionCode"`
}

// VideoCategory is an available YouTube video category.
type VideoCategory struct {
	// ID is the category identifier used by updateVideo.
	ID string `json:"id"`
	// Title is the localized category label.
	Title string `json:"title"`
	// Assignable reports whether uploads can select this category.
	Assignable bool `json:"assignable"`
}

// VideoCategoriesPage contains the categories YouTube returned for one region.
type VideoCategoriesPage struct {
	// Categories are the region's categories.
	Categories []VideoCategory `json:"categories"`
}

// RateVideoInput replaces the connected account's rating of one video.
type RateVideoInput struct {
	// VideoID is the 11-character video ID.
	VideoID string `json:"videoId"`
	// Rating is like, dislike, or none.
	Rating string `json:"rating"`
}

// RatedVideo records the requested rating.
type RatedVideo struct {
	// VideoID is the rated video ID.
	VideoID string `json:"videoId"`
	// Rating is the applied rating.
	Rating string `json:"rating"`
}

// DeletedVideo identifies a video confirmed deleted.
type DeletedVideo struct {
	// VideoID is the deleted video ID.
	VideoID string `json:"videoId"`
}

// UpdateVideoInput replaces one owned video's editable metadata.
type UpdateVideoInput struct {
	// VideoID is the 11-character video ID.
	VideoID string `json:"videoId"`
	// Title is the complete replacement title.
	Title string `json:"title"`
	// Description is the complete replacement description; blank clears it.
	Description string `json:"description"`
	// CategoryID is an assignable ID from listVideoCategories.
	CategoryID string `json:"categoryId"`
	// PrivacyStatus is public, unlisted, or private.
	PrivacyStatus string `json:"privacyStatus"`
}

// UpdatedVideo contains the editable metadata confirmed by YouTube.
type UpdatedVideo struct {
	// VideoID is the updated video ID.
	VideoID string `json:"videoId"`
	// Title is the confirmed title.
	Title string `json:"title"`
	// Description is the confirmed description.
	Description string `json:"description"`
	// CategoryID is the confirmed category ID.
	CategoryID string `json:"categoryId"`
	// PrivacyStatus is the confirmed visibility setting.
	PrivacyStatus string `json:"privacyStatus"`
}

// ListVideosOperation reads popular videos in one region.
type ListVideosOperation struct{ client *Client }

// ListVideoCategoriesOperation reads assignable video categories.
type ListVideoCategoriesOperation struct{ client *Client }

// RateVideoOperation replaces a rating.
type RateVideoOperation struct{ client *Client }

// DeleteVideoOperation deletes one video.
type DeleteVideoOperation struct{ client *Client }

// UpdateVideoOperation replaces one video's metadata.
type UpdateVideoOperation struct{ client *Client }

// ListVideos binds the popular videos Query.
func (client *Client) ListVideos() ListVideosOperation { return ListVideosOperation{client} }

// ListVideoCategories binds the category Query.
func (client *Client) ListVideoCategories() ListVideoCategoriesOperation {
	return ListVideoCategoriesOperation{client}
}

// RateVideo binds the rating Mutation.
func (client *Client) RateVideo() RateVideoOperation { return RateVideoOperation{client} }

// DeleteVideo binds the video deletion Mutation.
func (client *Client) DeleteVideo() DeleteVideoOperation { return DeleteVideoOperation{client} }

// UpdateVideo binds the metadata replacement Mutation.
func (client *Client) UpdateVideo() UpdateVideoOperation { return UpdateVideoOperation{client} }

var listVideosBranches = operationBranches{notFound: ListVideosBranchNotFound, quotaExhausted: ListVideosBranchQuotaExhausted, rejected: ListVideosBranchProviderRejected, invalidResponse: ListVideosBranchInvalidResponse, defect: ListVideosBranchDefect}
var listVideoCategoriesBranches = operationBranches{notFound: ListVideoCategoriesBranchNotFound, quotaExhausted: ListVideoCategoriesBranchQuotaExhausted, rejected: ListVideoCategoriesBranchProviderRejected, invalidResponse: ListVideoCategoriesBranchInvalidResponse, defect: ListVideoCategoriesBranchDefect}
var rateVideoBranches = operationBranches{notFound: RateVideoBranchNotFound, quotaExhausted: RateVideoBranchQuotaExhausted, rejected: RateVideoBranchProviderRejected, invalidResponse: RateVideoBranchInvalidResponse, defect: RateVideoBranchDefect}
var deleteVideoBranches = operationBranches{notFound: DeleteVideoBranchNotFound, quotaExhausted: DeleteVideoBranchQuotaExhausted, rejected: DeleteVideoBranchProviderRejected, invalidResponse: DeleteVideoBranchInvalidResponse, defect: DeleteVideoBranchDefect}
var updateVideoBranches = operationBranches{notFound: UpdateVideoBranchNotFound, quotaExhausted: UpdateVideoBranchQuotaExhausted, rejected: UpdateVideoBranchProviderRejected, invalidResponse: UpdateVideoBranchInvalidResponse, defect: UpdateVideoBranchDefect}

// Definition returns the generated popular videos contract.
func (ListVideosOperation) Definition() sdkgo.QueryDefinition { return ListVideosDefinition }

// Definition returns the generated category contract.
func (ListVideoCategoriesOperation) Definition() sdkgo.QueryDefinition {
	return ListVideoCategoriesDefinition
}

// Definition returns the generated rating contract.
func (RateVideoOperation) Definition() sdkgo.MutationDefinition { return RateVideoDefinition }

// Definition returns the generated deletion contract.
func (DeleteVideoOperation) Definition() sdkgo.MutationDefinition { return DeleteVideoDefinition }

// Definition returns the generated video replacement contract.
func (UpdateVideoOperation) Definition() sdkgo.MutationDefinition { return UpdateVideoDefinition }

// IdempotencyKey uses the stable Step call ID for an absolute rating.
func (RateVideoOperation) IdempotencyKey(callID sdkgo.CallID, _ RateVideoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the stable Step call ID for deletion.
func (DeleteVideoOperation) IdempotencyKey(callID sdkgo.CallID, _ GetVideoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey uses the stable Step call ID for an absolute replacement.
func (UpdateVideoOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateVideoInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one page of popular videos.
func (operation ListVideosOperation) Invoke(call sdkgo.Call, input ListVideosInput) sdkgo.QueryAttempt[VideosPage] {
	query := url.Values{"part": {"snippet,contentDetails,status,statistics"}, "chart": {"mostPopular"}}
	if !regionCodePattern.MatchString(input.RegionCode) || validatePageRequest(query, input.MaxResults, input.PageToken) != nil {
		return sdkgo.NewQueryBranch(ListVideosBranchDefect, VideosPage{}, providerFailurePointer("listVideos", sdkgo.FailureValidation, "regionCode or page is invalid"), sdkgo.Receipt{})
	}
	query.Set("regionCode", input.RegionCode)
	response, outcome := operation.client.exchange(call, "listVideos", providerRequest{method: http.MethodGet, path: "/videos", query: query}, "")
	if outcome != nil {
		return queryAttemptForOutcome[VideosPage](*outcome, listVideosBranches)
	}
	receipt := operation.client.receipt(call, "", response.header)
	var page struct {
		Items         []providerVideo `json:"items"`
		NextPageToken string          `json:"nextPageToken"`
	}
	if decodeResponse(response.body, &page) != nil || len(page.Items) > MaximumPageSize || validateReturnedPageToken(page.NextPageToken) != nil {
		return sdkgo.NewQueryBranch(ListVideosBranchInvalidResponse, VideosPage{}, providerFailurePointer("listVideos", sdkgo.FailureProtocol, "YouTube returned an invalid video page"), receipt)
	}
	result := VideosPage{Videos: make([]Video, 0, len(page.Items)), NextPageToken: page.NextPageToken}
	for _, item := range page.Items {
		video, err := convertProviderVideo(item, item.ID)
		if err != nil {
			return sdkgo.NewQueryBranch(ListVideosBranchInvalidResponse, VideosPage{}, providerFailurePointer("listVideos", sdkgo.FailureProtocol, "YouTube returned an invalid video"), receipt)
		}
		result.Videos = append(result.Videos, video)
	}
	return sdkgo.NewQueryBranch(ListVideosBranchListed, result, nil, receipt)
}

// Invoke lists category IDs accepted for videos in one region.
func (operation ListVideoCategoriesOperation) Invoke(call sdkgo.Call, input ListVideoCategoriesInput) sdkgo.QueryAttempt[VideoCategoriesPage] {
	if !regionCodePattern.MatchString(input.RegionCode) {
		return sdkgo.NewQueryBranch(ListVideoCategoriesBranchDefect, VideoCategoriesPage{}, providerFailurePointer("listVideoCategories", sdkgo.FailureValidation, "regionCode must be a two-letter uppercase country code"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "listVideoCategories", providerRequest{method: http.MethodGet, path: "/videoCategories", query: url.Values{"part": {"snippet"}, "regionCode": {input.RegionCode}}}, "")
	if outcome != nil {
		return queryAttemptForOutcome[VideoCategoriesPage](*outcome, listVideoCategoriesBranches)
	}
	receipt := operation.client.receipt(call, "", response.header)
	var decoded struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title      string `json:"title"`
				Assignable bool   `json:"assignable"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if decodeResponse(response.body, &decoded) != nil || len(decoded.Items) > 100 {
		return sdkgo.NewQueryBranch(ListVideoCategoriesBranchInvalidResponse, VideoCategoriesPage{}, providerFailurePointer("listVideoCategories", sdkgo.FailureProtocol, "YouTube returned an invalid category list"), receipt)
	}
	result := VideoCategoriesPage{Categories: make([]VideoCategory, 0, len(decoded.Items))}
	for _, item := range decoded.Items {
		if !categoryIDPattern.MatchString(item.ID) || item.Snippet.Title == "" {
			return sdkgo.NewQueryBranch(ListVideoCategoriesBranchInvalidResponse, VideoCategoriesPage{}, providerFailurePointer("listVideoCategories", sdkgo.FailureProtocol, "YouTube returned an invalid category"), receipt)
		}
		result.Categories = append(result.Categories, VideoCategory{ID: item.ID, Title: item.Snippet.Title, Assignable: item.Snippet.Assignable})
	}
	return sdkgo.NewQueryBranch(ListVideoCategoriesBranchListed, result, nil, receipt)
}

// Invoke replaces the connected account's rating and accepts the empty 204 response.
func (operation RateVideoOperation) Invoke(call sdkgo.Call, input RateVideoInput) sdkgo.MutationAttempt[RatedVideo] {
	if !videoIDPattern.MatchString(input.VideoID) || (input.Rating != "like" && input.Rating != "dislike" && input.Rating != "none") {
		return sdkgo.NewMutationBranch(RateVideoBranchDefect, RatedVideo{}, providerFailurePointer("rateVideo", sdkgo.FailureValidation, "videoId or rating is invalid"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "rateVideo", providerRequest{method: http.MethodPost, path: "/videos/rate", query: url.Values{"id": {input.VideoID}, "rating": {input.Rating}}, requiresManage: true}, input.VideoID)
	if outcome != nil {
		return mutationAttemptForOutcome[RatedVideo](*outcome, rateVideoBranches)
	}
	receipt := operation.client.receipt(call, input.VideoID, response.header)
	if response.statusCode != http.StatusNoContent {
		return sdkgo.NewMutationBranch(RateVideoBranchInvalidResponse, RatedVideo{}, providerFailurePointer("rateVideo", sdkgo.FailureProtocol, "YouTube returned an unexpected rating status"), receipt)
	}
	return sdkgo.NewMutationBranch(RateVideoBranchRated, RatedVideo{VideoID: input.VideoID, Rating: input.Rating}, nil, receipt)
}

// Invoke deletes one video owned by the connected channel.
func (operation DeleteVideoOperation) Invoke(call sdkgo.Call, input GetVideoInput) sdkgo.MutationAttempt[DeletedVideo] {
	if !videoIDPattern.MatchString(input.VideoID) {
		return sdkgo.NewMutationBranch(DeleteVideoBranchDefect, DeletedVideo{}, providerFailurePointer("deleteVideo", sdkgo.FailureValidation, "videoId is invalid"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "deleteVideo", providerRequest{method: http.MethodDelete, path: "/videos", query: url.Values{"id": {input.VideoID}}, requiresManage: true}, input.VideoID)
	if outcome != nil {
		return mutationAttemptForOutcome[DeletedVideo](*outcome, deleteVideoBranches)
	}
	receipt := operation.client.receipt(call, input.VideoID, response.header)
	if response.statusCode != http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeleteVideoBranchInvalidResponse, DeletedVideo{}, providerFailurePointer("deleteVideo", sdkgo.FailureProtocol, "YouTube returned an unexpected delete status"), receipt)
	}
	return sdkgo.NewMutationBranch(DeleteVideoBranchDeleted, DeletedVideo{VideoID: input.VideoID}, nil, receipt)
}

// Invoke replaces a video's snippet and privacy status with complete values.
func (operation UpdateVideoOperation) Invoke(call sdkgo.Call, input UpdateVideoInput) sdkgo.MutationAttempt[UpdatedVideo] {
	if !videoIDPattern.MatchString(input.VideoID) || strings.TrimSpace(input.Title) == "" || !categoryIDPattern.MatchString(input.CategoryID) || !validPlaylistPrivacy(input.PrivacyStatus) {
		return sdkgo.NewMutationBranch(UpdateVideoBranchDefect, UpdatedVideo{}, providerFailurePointer("updateVideo", sdkgo.FailureValidation, "video ID, title, category, or privacy is invalid"), sdkgo.Receipt{})
	}
	body, err := json.Marshal(map[string]any{"id": input.VideoID, "snippet": map[string]string{"title": input.Title, "description": input.Description, "categoryId": input.CategoryID}, "status": map[string]string{"privacyStatus": input.PrivacyStatus}})
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateVideoBranchDefect, UpdatedVideo{}, providerFailurePointer("updateVideo", sdkgo.FailureLocalDefect, "video replacement could not be encoded"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "updateVideo", providerRequest{method: http.MethodPut, path: "/videos", query: url.Values{"part": {"snippet,status"}}, body: body, requiresManage: true}, input.VideoID)
	if outcome != nil {
		return mutationAttemptForOutcome[UpdatedVideo](*outcome, updateVideoBranches)
	}
	receipt := operation.client.receipt(call, input.VideoID, response.header)
	var decoded struct {
		ID      string `json:"id"`
		Snippet struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			CategoryID  string `json:"categoryId"`
		} `json:"snippet"`
		Status struct {
			PrivacyStatus string `json:"privacyStatus"`
		} `json:"status"`
	}
	if decodeResponse(response.body, &decoded) != nil || decoded.ID != input.VideoID || decoded.Snippet.Title != input.Title || decoded.Snippet.Description != input.Description || decoded.Snippet.CategoryID != input.CategoryID || decoded.Status.PrivacyStatus != input.PrivacyStatus {
		return sdkgo.NewMutationBranch(UpdateVideoBranchInvalidResponse, UpdatedVideo{}, providerFailurePointer("updateVideo", sdkgo.FailureProtocol, "YouTube returned different video metadata"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateVideoBranchUpdated, UpdatedVideo{VideoID: input.VideoID, Title: input.Title, Description: input.Description, CategoryID: input.CategoryID, PrivacyStatus: input.PrivacyStatus}, nil, receipt)
}
