// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinterest

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetBoardInput identifies one board available to the authorized account.
type GetBoardInput struct {
	// BoardID is the numeric Pinterest board ID; blank is invalid.
	BoardID string `json:"boardId"`
}

// GetPinInput identifies one Pin available to the authorized account.
type GetPinInput struct {
	// PinID is the numeric Pinterest Pin ID; blank is invalid.
	PinID string `json:"pinId"`
}

// GetBoardOperation retrieves one board's identity and name.
type GetBoardOperation struct{ client *Client }

// GetBoard binds the board Query to this client.
func (client *Client) GetBoard() GetBoardOperation { return GetBoardOperation{client} }

// Definition returns the generated board lookup contract.
func (GetBoardOperation) Definition() sdkgo.QueryDefinition { return GetBoardDefinition }

// Invoke fetches one board without retaining its description or owner details.
func (operation GetBoardOperation) Invoke(call sdkgo.Call, input GetBoardInput) sdkgo.QueryAttempt[Board] {
	branches := pinterestReadBranches{found: GetBoardBranchFound, notFound: GetBoardBranchNotFound, rejected: GetBoardBranchProviderRejected, invalidResponse: GetBoardBranchInvalidResponse, defect: GetBoardBranchDefect}
	if !numericID.MatchString(input.BoardID) {
		return pinterestReadBranch[Board]("getBoard", branches.defect, sdkgo.FailureValidation, "board ID is invalid")
	}
	response := operation.client.get(call, "/v5/boards/"+input.BoardID)
	return pinterestReadAttempt("getBoard", response, branches, decodeBoard)
}

// GetPinOperation retrieves one Pin's identity, board ID, and title.
type GetPinOperation struct{ client *Client }

// GetPin binds the Pin Query to this client.
func (client *Client) GetPin() GetPinOperation { return GetPinOperation{client} }

// Definition returns the generated Pin lookup contract.
func (GetPinOperation) Definition() sdkgo.QueryDefinition { return GetPinDefinition }

// Invoke fetches one Pin without retaining media or other provider fields.
func (operation GetPinOperation) Invoke(call sdkgo.Call, input GetPinInput) sdkgo.QueryAttempt[Pin] {
	branches := pinterestReadBranches{found: GetPinBranchFound, notFound: GetPinBranchNotFound, rejected: GetPinBranchProviderRejected, invalidResponse: GetPinBranchInvalidResponse, defect: GetPinBranchDefect}
	if !numericID.MatchString(input.PinID) {
		return pinterestReadBranch[Pin]("getPin", branches.defect, sdkgo.FailureValidation, "Pin ID is invalid")
	}
	response := operation.client.get(call, "/v5/pins/"+input.PinID)
	return pinterestReadAttempt("getPin", response, branches, decodePin)
}

type pinterestReadBranches struct {
	found           sdkgo.BranchID
	notFound        sdkgo.BranchID
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

func pinterestReadAttempt[T any](operationName string, response providerResponse, branches pinterestReadBranches, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	if errors.Is(response.err, errCredential) {
		return pinterestReadBranch[T](operationName, branches.defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return pinterestReadRetry[T](operationName, sdkgo.FailureTransport, "Pinterest request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return pinterestReadRetry[T](operationName, sdkgo.FailureRateLimit, "Pinterest rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return pinterestReadRetry[T](operationName, sdkgo.FailureAvailability, "Pinterest is temporarily unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return pinterestReadBranch[T](operationName, branches.notFound, sdkgo.FailureNotFound, "Pinterest did not find the resource")
	}
	if response.status != http.StatusOK {
		return pinterestReadBranch[T](operationName, branches.rejected, sdkgo.FailureProviderRejection, "Pinterest rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return pinterestReadBranch[T](operationName, branches.invalidResponse, sdkgo.FailureResponseTooLarge, "Pinterest response exceeded the configured limit")
	}
	if response.err != nil {
		return pinterestReadRetry[T](operationName, sdkgo.FailureTransport, "Pinterest response was interrupted", 0)
	}
	value, err := decode(response.body)
	if err != nil {
		return pinterestReadBranch[T](operationName, branches.invalidResponse, sdkgo.FailureProtocol, "Pinterest returned invalid resource JSON")
	}
	return sdkgo.NewQueryBranch(branches.found, value, nil, sdkgo.Receipt{})
}

func decodeBoard(body []byte) (Board, error) {
	var board Board
	if err := json.Unmarshal(body, &board); err != nil {
		return Board{}, err
	}
	if !numericID.MatchString(board.ID) || board.Name == "" {
		return Board{}, errors.New("Pinterest board identity is invalid")
	}
	return board, nil
}

func decodePin(body []byte) (Pin, error) {
	var payload struct {
		ID      string `json:"id"`
		BoardID string `json:"board_id"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Pin{}, err
	}
	if !numericID.MatchString(payload.ID) || !numericID.MatchString(payload.BoardID) {
		return Pin{}, errors.New("Pinterest Pin identity is invalid")
	}
	return Pin{ID: payload.ID, BoardID: payload.BoardID, Title: payload.Title}, nil
}

func pinterestReadBranch[T any](operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "pinterest", Operation: operationName, Message: message}
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func pinterestReadRetry[T any](operationName string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "pinterest", Operation: operationName, Message: message}, delay)
}
