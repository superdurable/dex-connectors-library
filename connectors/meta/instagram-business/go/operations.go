// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package instagrambusiness

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListMediaInput selects a professional Instagram account and an optional media cursor.
type ListMediaInput struct {
	// IGUserID is the numeric Instagram account ID linked to a Facebook Page.
	IGUserID string `json:"igUserId"`
	// After is Meta's opaque cursor from a prior page; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// InstagramMedia contains one media object's public identity and display fields.
type InstagramMedia struct {
	// ID is Meta's stable Instagram media ID.
	ID string `json:"id"`
	// Caption is the optional caption of the media object.
	Caption string `json:"caption"`
	// Timestamp is Meta's raw media timestamp.
	Timestamp string `json:"timestamp"`
	// Permalink is the optional public HTTPS URL of the media object.
	Permalink string `json:"permalink"`
	// MediaType is IMAGE, VIDEO, or CAROUSEL_ALBUM.
	MediaType string `json:"mediaType"`
}

// MediaListPage contains one bounded Instagram media page.
type MediaListPage struct {
	// Media are the account media objects in provider order.
	Media []InstagramMedia `json:"media"`
	// NextCursor is blank when Meta reported no later page.
	NextCursor string `json:"nextCursor"`
}

// ListMediaOperation reads one Instagram media page.
type ListMediaOperation struct{ client *Client }

// ListMedia binds the media Query to this client.
func (client *Client) ListMedia() ListMediaOperation { return ListMediaOperation{client} }

// Definition returns the generated media Query contract.
func (ListMediaOperation) Definition() sdkgo.QueryDefinition { return ListMediaDefinition }

var numericID = regexp.MustCompile(`^[0-9]{1,40}$`)

// Invoke lists media while projecting only ID, caption, media type, timestamp, and permalink.
func (operation ListMediaOperation) Invoke(call sdkgo.Call, input ListMediaInput) sdkgo.QueryAttempt[MediaListPage] {
	if !numericID.MatchString(input.IGUserID) || len(input.After) > 4096 || strings.ContainsAny(input.After, "\r\n\x00") {
		return queryBranch("listMedia", ListMediaBranchDefect, MediaListPage{}, sdkgo.FailureValidation, "Instagram account ID or cursor is invalid")
	}
	query := url.Values{"fields": {"id,caption,media_type,timestamp,permalink"}, "limit": {"100"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	response := operation.client.get(call, "/"+input.IGUserID+"/media?"+query.Encode())
	if decision, handled := classifyGraphResponse("listMedia", response, MediaListPage{}, ListMediaBranchNotFound, ListMediaBranchProviderRejected, ListMediaBranchInvalidResponse, ListMediaBranchDefect); handled {
		return decision
	}
	var wire struct {
		Data   []mediaWire `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data == nil || len(wire.Data) > 100 || len(wire.Paging.Cursors.After) > 4096 {
		return queryBranch("listMedia", ListMediaBranchInvalidResponse, MediaListPage{}, sdkgo.FailureProtocol, "Meta returned invalid Instagram media JSON")
	}
	page := MediaListPage{Media: make([]InstagramMedia, 0, len(wire.Data)), NextCursor: wire.Paging.Cursors.After}
	for _, media := range wire.Data {
		metadata, valid := projectMedia(media)
		if !valid {
			return queryBranch("listMedia", ListMediaBranchInvalidResponse, MediaListPage{}, sdkgo.FailureProtocol, "Meta returned incomplete media metadata")
		}
		page.Media = append(page.Media, metadata)
	}
	return sdkgo.NewQueryBranch(ListMediaBranchListed, page, nil, sdkgo.Receipt{})
}

type mediaWire struct {
	ID        string `json:"id"`
	Caption   string `json:"caption"`
	MediaType string `json:"media_type"`
	Timestamp string `json:"timestamp"`
	Permalink string `json:"permalink"`
}

func projectMedia(wire mediaWire) (InstagramMedia, bool) {
	if !numericID.MatchString(wire.ID) || wire.Timestamp == "" || wire.MediaType == "" || (wire.Permalink != "" && !strings.HasPrefix(wire.Permalink, "https://")) {
		return InstagramMedia{}, false
	}
	return InstagramMedia{ID: wire.ID, Caption: wire.Caption, MediaType: wire.MediaType, Timestamp: wire.Timestamp, Permalink: wire.Permalink}, true
}

func classifyGraphResponse[Output any](operation string, response graphResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureAvailability, "Meta is temporarily unavailable"), response.retryAfter), true
	}
	if graphErr, ok := decodeGraphError(response.body); response.status >= 400 && response.err == nil && ok {
		if graphErr.isThrottled() {
			return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter), true
		}
		if graphErr.isTransient {
			return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureAvailability, "Meta reported a transient error"), response.retryAfter), true
		}
	}
	if response.status == http.StatusNotFound {
		return queryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "Meta did not find the resource"), true
	}
	if response.status != http.StatusOK {
		return queryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "Meta rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "Meta response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func queryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "instagram-business", Operation: operation, Message: message}
}
