// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinterest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListBoardsInput selects one page of boards owned by the authorized account.
type ListBoardsInput struct {
	// PageSize limits the page to 1 through 250 boards; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Bookmark continues from a prior page; blank starts at the first page.
	Bookmark string `json:"bookmark,omitempty"`
}

// Board is an owned Pinterest board's stable identity and display name.
type Board struct {
	// ID is the provider's numeric board ID.
	ID string `json:"id"`
	// Name is the board's display name.
	Name string `json:"name"`
}

// BoardsPage holds one bounded page of boards and its continuation cursor.
type BoardsPage struct {
	// Boards are the identities on this page.
	Boards []Board `json:"boards"`
	// NextBookmark is blank after the last page.
	NextBookmark string `json:"nextBookmark"`
}

// ListBoardsOperation retrieves one page of owned boards.
type ListBoardsOperation struct{ client *Client }

// ListBoards binds the board Query to this client.
func (client *Client) ListBoards() ListBoardsOperation { return ListBoardsOperation{client} }

// Definition returns the generated board page contract.
func (ListBoardsOperation) Definition() sdkgo.QueryDefinition { return ListBoardsDefinition }

// Invoke fetches one board page and projects its identities and names.
func (operation ListBoardsOperation) Invoke(call sdkgo.Call, input ListBoardsInput) sdkgo.QueryAttempt[BoardsPage] {
	if input.PageSize < 0 || input.PageSize > 250 || !validBookmark(input.Bookmark) {
		return boardBranch(ListBoardsBranchDefect, sdkgo.FailureValidation, "page size or bookmark is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 25
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.Bookmark != "" {
		query.Set("bookmark", input.Bookmark)
	}
	response := operation.client.get(call, "/v5/boards?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return boardBranch(ListBoardsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return boardRetry(sdkgo.FailureTransport, "Pinterest request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return boardRetry(sdkgo.FailureRateLimit, "Pinterest rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return boardRetry(sdkgo.FailureAvailability, "Pinterest is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return boardBranch(ListBoardsBranchProviderRejected, sdkgo.FailureProviderRejection, "Pinterest rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return boardBranch(ListBoardsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Pinterest response exceeded the configured limit")
	}
	if response.err != nil {
		return boardRetry(sdkgo.FailureTransport, "Pinterest response was interrupted", 0)
	}
	page, err := decodeBoardsPage(response.body, input.PageSize)
	if err != nil {
		return boardBranch(ListBoardsBranchInvalidResponse, sdkgo.FailureProtocol, "Pinterest returned invalid board JSON")
	}
	return sdkgo.NewQueryBranch(ListBoardsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeBoardsPage(body []byte, pageSize int) (BoardsPage, error) {
	var payload struct {
		Items []struct {
			ID string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
		Bookmark *string `json:"bookmark"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return BoardsPage{}, err
	}
	if payload.Items == nil || len(payload.Items) > pageSize {
		return BoardsPage{}, fmt.Errorf("Pinterest board page is invalid")
	}
	page := BoardsPage{Boards: make([]Board, 0, len(payload.Items))}
	if payload.Bookmark != nil {
		if !validBookmark(*payload.Bookmark) {
			return BoardsPage{}, fmt.Errorf("Pinterest bookmark is invalid")
		}
		page.NextBookmark = *payload.Bookmark
	}
	for _, item := range payload.Items {
		if !numericID.MatchString(item.ID) || item.Name == "" {
			return BoardsPage{}, fmt.Errorf("Pinterest board identity is invalid")
		}
		page.Boards = append(page.Boards, Board{ID: item.ID, Name: item.Name})
	}
	return page, nil
}

func boardBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[BoardsPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pinterest", Operation: "listBoards", Message: message}
	return sdkgo.NewQueryBranch(branch, BoardsPage{}, &failure, sdkgo.Receipt{})
}

func boardRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[BoardsPage] {
	return sdkgo.NewQueryRetry[BoardsPage](sdkgo.Failure{Kind: kind, Provider: "pinterest", Operation: "listBoards", Message: message}, delay)
}
