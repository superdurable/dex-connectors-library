// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListScheduledPostsInput selects one page of scheduled messages in a UTC interval.
type ListScheduledPostsInput struct {
	// StartTime is the inclusive UTC instant, formatted as RFC 3339 with a Z suffix.
	StartTime string `json:"startTime"`
	// EndTime is the exclusive UTC instant, no later than four weeks after StartTime.
	EndTime string `json:"endTime"`
	// SocialProfileID optionally filters to one ID returned by listSocialProfiles.
	SocialProfileID string `json:"socialProfileId,omitempty"`
	// Cursor is the opaque cursor from a prior page; empty selects the first page.
	Cursor string `json:"cursor,omitempty"`
}

// ScheduledPost identifies a scheduled outbound message without returning its text.
type ScheduledPost struct {
	// ID is Hootsuite's stable message ID.
	ID string `json:"id"`
	// State is the provider's outbound message state.
	State string `json:"state"`
	// SocialProfileID is the destination social profile.
	SocialProfileID string `json:"socialProfileId"`
	// ScheduledSendTime is the UTC send instant.
	ScheduledSendTime string `json:"scheduledSendTime"`
}

// ScheduledPostsPage contains at most 100 scheduled messages and a continuation cursor.
type ScheduledPostsPage struct {
	// Posts contains the scheduled messages in provider order.
	Posts []ScheduledPost `json:"posts"`
	// NextCursor is empty when there is no next page.
	NextCursor string `json:"nextCursor"`
}

// ListScheduledPostsOperation reads scheduled outbound messages.
type ListScheduledPostsOperation struct{ client *Client }

// ListScheduledPosts binds the scheduled message Query to this client.
func (client *Client) ListScheduledPosts() ListScheduledPostsOperation {
	return ListScheduledPostsOperation{client}
}

// Definition returns the manifest-generated scheduled message Query contract.
func (ListScheduledPostsOperation) Definition() sdkgo.QueryDefinition {
	return ListScheduledPostsDefinition
}

// Invoke reads one scheduled message page or classifies a definite provider outcome.
func (operation ListScheduledPostsOperation) Invoke(call sdkgo.Call, input ListScheduledPostsInput) sdkgo.QueryAttempt[ScheduledPostsPage] {
	start, startErr := time.Parse(time.RFC3339, input.StartTime)
	end, endErr := time.Parse(time.RFC3339, input.EndTime)
	if startErr != nil || endErr != nil || start.IsZero() || !end.After(start) || end.Sub(start) > 28*24*time.Hour ||
		start.Location() != time.UTC || end.Location() != time.UTC ||
		len(input.StartTime) == 0 || input.StartTime[len(input.StartTime)-1] != 'Z' ||
		len(input.EndTime) == 0 || input.EndTime[len(input.EndTime)-1] != 'Z' ||
		len(input.Cursor) > 4096 || !isSafeQueryValue(input.Cursor) ||
		len(input.SocialProfileID) > 40 || !isSafeQueryValue(input.SocialProfileID) {
		return scheduledBranch(ListScheduledPostsBranchDefect, sdkgo.FailureValidation, "scheduled post interval or filter is invalid")
	}
	query := url.Values{"startTime": {input.StartTime}, "endTime": {input.EndTime}, "state": {"SCHEDULED"}, "limit": {"100"}}
	if input.SocialProfileID != "" {
		query.Set("socialProfileIds", input.SocialProfileID)
	}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	response := operation.client.get(call, "/v1/messages?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return scheduledBranch(ListScheduledPostsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return scheduledRetry(sdkgo.FailureTransport, "Hootsuite request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return scheduledRetry(sdkgo.FailureRateLimit, "Hootsuite rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return scheduledRetry(sdkgo.FailureAvailability, "Hootsuite is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return scheduledBranch(ListScheduledPostsBranchProviderRejected, sdkgo.FailureProviderRejection, "Hootsuite rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return scheduledBranch(ListScheduledPostsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Hootsuite response exceeded the configured limit")
	}
	if response.err != nil {
		return scheduledRetry(sdkgo.FailureTransport, "Hootsuite response was interrupted", 0)
	}
	var wire struct {
		Data []struct {
			ID            string `json:"id"`
			State         string `json:"state"`
			SocialProfile struct {
				ID string `json:"id"`
			} `json:"socialProfile"`
			ScheduledSendTime string `json:"scheduledSendTime"`
		} `json:"data"`
		Metadata struct {
			Cursor struct {
				Next *string `json:"next"`
			} `json:"cursor"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data == nil || len(wire.Data) > 100 {
		return scheduledBranch(ListScheduledPostsBranchInvalidResponse, sdkgo.FailureProtocol, "Hootsuite returned invalid scheduled message JSON")
	}
	page := ScheduledPostsPage{Posts: make([]ScheduledPost, 0, len(wire.Data))}
	for _, post := range wire.Data {
		if post.ID == "" || post.State != "SCHEDULED" || post.SocialProfile.ID == "" || post.ScheduledSendTime == "" {
			return scheduledBranch(ListScheduledPostsBranchInvalidResponse, sdkgo.FailureProtocol, "Hootsuite returned incomplete scheduled message metadata")
		}
		page.Posts = append(page.Posts, ScheduledPost{ID: post.ID, State: post.State, SocialProfileID: post.SocialProfile.ID, ScheduledSendTime: post.ScheduledSendTime})
	}
	if wire.Metadata.Cursor.Next != nil {
		if len(*wire.Metadata.Cursor.Next) > 4096 || !isSafeQueryValue(*wire.Metadata.Cursor.Next) {
			return scheduledBranch(ListScheduledPostsBranchInvalidResponse, sdkgo.FailureProtocol, "Hootsuite returned an invalid page cursor")
		}
		page.NextCursor = *wire.Metadata.Cursor.Next
	}
	return sdkgo.NewQueryBranch(ListScheduledPostsBranchListed, page, nil, sdkgo.Receipt{})
}

func isSafeQueryValue(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func scheduledBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ScheduledPostsPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "listScheduledPosts", Message: message}
	return sdkgo.NewQueryBranch(branch, ScheduledPostsPage{}, &failure, sdkgo.Receipt{})
}

func scheduledRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ScheduledPostsPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "listScheduledPosts", Message: message}
	return sdkgo.NewQueryRetry[ScheduledPostsPage](failure, delay)
}
