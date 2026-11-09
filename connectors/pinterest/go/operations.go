// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinterest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListPinsInput selects one cursor-based page of owned Pins.
type ListPinsInput struct {
	// PageSize is the maximum count from 1 to 250; zero uses the provider default of 25.
	PageSize int `json:"pageSize,omitempty"`
	// Bookmark is the cursor returned by a previous PinsPage; blank starts at the first page.
	Bookmark string `json:"bookmark,omitempty"`
}

// Pin contains a stable Pinterest identity and its display title.
type Pin struct {
	// ID is the provider's stable numeric Pin ID.
	ID string `json:"id"`
	// BoardID is the numeric ID of the containing board.
	BoardID string `json:"boardId"`
	// Title is the Pin title, if supplied by Pinterest.
	Title string `json:"title"`
}

// PinsPage contains the selected Pins and the cursor for the next page.
type PinsPage struct {
	// Pins are the identities returned by this page request.
	Pins []Pin `json:"pins"`
	// NextBookmark is blank when no further page exists.
	NextBookmark string `json:"nextBookmark"`
}

// ListPinsOperation retrieves one page of owned Pins.
type ListPinsOperation struct{ client *Client }

// ListPins binds the Pin Query to this client.
func (client *Client) ListPins() ListPinsOperation { return ListPinsOperation{client} }

// Definition returns the manifest-generated Pin page contract.
func (ListPinsOperation) Definition() sdkgo.QueryDefinition { return ListPinsDefinition }

// Invoke reads a bounded page and projects only Pin IDs, board IDs, and titles.
func (operation ListPinsOperation) Invoke(call sdkgo.Call, input ListPinsInput) sdkgo.QueryAttempt[PinsPage] {
	if input.PageSize < 0 || input.PageSize > 250 || !validBookmark(input.Bookmark) {
		return pinBranch(ListPinsBranchDefect, sdkgo.FailureValidation, "page size or bookmark is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 25
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.Bookmark != "" {
		query.Set("bookmark", input.Bookmark)
	}
	response := operation.client.get(call, "/v5/pins?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return pinBranch(ListPinsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return pinRetry(sdkgo.FailureTransport, "Pinterest request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return pinRetry(sdkgo.FailureRateLimit, "Pinterest rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return pinRetry(sdkgo.FailureAvailability, "Pinterest is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return pinBranch(ListPinsBranchProviderRejected, sdkgo.FailureProviderRejection, "Pinterest rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return pinBranch(ListPinsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Pinterest response exceeded the configured limit")
	}
	if response.err != nil {
		return pinRetry(sdkgo.FailureTransport, "Pinterest response was interrupted", 0)
	}
	page, err := decodePinsPage(response.body, input.PageSize)
	if err != nil {
		return pinBranch(ListPinsBranchInvalidResponse, sdkgo.FailureProtocol, "Pinterest returned invalid Pin JSON")
	}
	return sdkgo.NewQueryBranch(ListPinsBranchListed, page, nil, sdkgo.Receipt{})
}

var numericID = regexp.MustCompile(`^[0-9]{1,32}$`)

func validBookmark(bookmark string) bool {
	return len(bookmark) <= 4096 && !strings.ContainsAny(bookmark, "\r\n")
}

func decodePinsPage(body []byte, pageSize int) (PinsPage, error) {
	var payload struct {
		Items []struct {
			ID      string `json:"id"`
			BoardID string `json:"board_id"`
			Title   string `json:"title"`
		} `json:"items"`
		Bookmark *string `json:"bookmark"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return PinsPage{}, err
	}
	if payload.Items == nil || len(payload.Items) > pageSize {
		return PinsPage{}, fmt.Errorf("Pinterest Pin page is invalid")
	}
	page := PinsPage{Pins: make([]Pin, 0, len(payload.Items))}
	if payload.Bookmark != nil {
		if !validBookmark(*payload.Bookmark) {
			return PinsPage{}, fmt.Errorf("Pinterest bookmark is invalid")
		}
		page.NextBookmark = *payload.Bookmark
	}
	for _, item := range payload.Items {
		if !numericID.MatchString(item.ID) || !numericID.MatchString(item.BoardID) {
			return PinsPage{}, fmt.Errorf("Pinterest Pin ID is invalid")
		}
		page.Pins = append(page.Pins, Pin{ID: item.ID, BoardID: item.BoardID, Title: item.Title})
	}
	return page, nil
}

func pinBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PinsPage] {
	failure := pinFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PinsPage{}, &failure, sdkgo.Receipt{})
}

func pinRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[PinsPage] {
	return sdkgo.NewQueryRetry[PinsPage](pinFailure(kind, message), delay)
}

func pinFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "pinterest", Operation: "listPins", Message: message}
}
