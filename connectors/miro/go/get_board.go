// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package miro

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetBoardInput selects a board by its non-secret ID.
type GetBoardInput struct {
	// BoardID is the ID after /board/ in a Miro board URL.
	BoardID string `json:"boardId"`
}

// BoardMetadata excludes the board description, policy, owner, and members.
type BoardMetadata struct {
	// ID is the Miro board ID.
	ID string `json:"id"`
	// Name is the board title.
	Name string `json:"name"`
}

// GetBoardOperation reads one board's identity and name.
type GetBoardOperation struct{ client *Client }

// GetBoard binds the Query to this client.
func (client *Client) GetBoard() GetBoardOperation { return GetBoardOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetBoardOperation) Definition() sdkgo.QueryDefinition { return GetBoardDefinition }

// Invoke reads one board and classifies retryable and definitive outcomes.
func (operation GetBoardOperation) Invoke(call sdkgo.Call, input GetBoardInput) sdkgo.QueryAttempt[BoardMetadata] {
	if !validBoardID(input.BoardID) {
		return boardBranch(GetBoardBranchDefect, sdkgo.FailureValidation, "board ID must be 1–256 printable characters")
	}
	response := operation.client.get(call, "/boards/"+url.PathEscape(input.BoardID))
	if errors.Is(response.err, errCredential) {
		return boardBranch(GetBoardBranchDefect, sdkgo.FailureAuthentication, "Miro credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[BoardMetadata](boardFailure(sdkgo.FailureTransport, "Miro request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[BoardMetadata](boardFailure(sdkgo.FailureRateLimit, "Miro rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[BoardMetadata](boardFailure(sdkgo.FailureAvailability, "Miro is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return boardBranch(GetBoardBranchNotFound, sdkgo.FailureNotFound, "Miro did not expose the board")
	}
	if response.status != http.StatusOK {
		return boardBranch(GetBoardBranchProviderRejected, sdkgo.FailureProviderRejection, "Miro rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return boardBranch(GetBoardBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Miro response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[BoardMetadata](boardFailure(sdkgo.FailureTransport, "Miro response was interrupted"), 0)
	}
	board, err := decodeBoard(response.body, input.BoardID)
	if err != nil {
		return boardBranch(GetBoardBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid board JSON")
	}
	return sdkgo.NewQueryBranch(GetBoardBranchFound, board, nil, sdkgo.Receipt{})
}

func validBoardID(boardID string) bool {
	if len(boardID) == 0 || len(boardID) > 256 || !utf8.ValidString(boardID) {
		return false
	}
	for _, character := range boardID {
		if unicode.IsControl(character) || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func decodeBoard(body []byte, requestedBoardID string) (BoardMetadata, error) {
	var wire struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return BoardMetadata{}, err
	}
	if wire.ID != requestedBoardID || strings.TrimSpace(wire.Name) == "" || wire.Type != "board" {
		return BoardMetadata{}, fmt.Errorf("board identity or name missing")
	}
	return BoardMetadata{ID: wire.ID, Name: wire.Name}, nil
}

func boardFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "miro", Operation: "getBoard", Message: message}
}

func boardBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[BoardMetadata] {
	failure := boardFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, BoardMetadata{}, &failure, sdkgo.Receipt{})
}
