// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package miro

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListBoardsInput selects one page of accessible boards.
type ListBoardsInput struct {
	// Query searches names and descriptions; blank lists all accessible boards.
	Query string `json:"query,omitempty"`
	// Offset is the zero-based starting index; zero starts at the first board.
	Offset int `json:"offset"`
	// Limit is the number of boards per page, from 1 to 50.
	Limit int `json:"limit"`
}

// BoardsPage contains one Miro board page.
type BoardsPage struct {
	// Boards contains the returned board identities and names.
	Boards []BoardMetadata `json:"boards"`
	// Offset is the zero-based starting index of this page.
	Offset int `json:"offset"`
	// Total is the provider-reported count of matching boards.
	Total int `json:"total"`
}

// ListBoardItemsInput selects one cursor page of board items.
type ListBoardItemsInput struct {
	// BoardID is the ID after /board/ in a Miro board URL.
	BoardID string `json:"boardId"`
	// Cursor is the previous page's cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit is the number of items per page, from 10 to 50.
	Limit int `json:"limit"`
}

// BoardItemMetadata identifies one board item and its visible content.
type BoardItemMetadata struct {
	// ID is the item's stable Miro ID.
	ID string `json:"id"`
	// Type is the provider item type, such as sticky_note or shape.
	Type string `json:"type"`
	// Content is text or HTML content when the item exposes it.
	Content string `json:"content,omitempty"`
}

// BoardItemsPage contains one cursor page of board items.
type BoardItemsPage struct {
	// Items contains the returned item identities and text content.
	Items []BoardItemMetadata `json:"items"`
	// Cursor is passed to ListBoardItems for the next page; blank ends pagination.
	Cursor string `json:"cursor"`
}

// GetBoardItemInput selects one item from a board.
type GetBoardItemInput struct {
	// BoardID is the ID after /board/ in a Miro board URL.
	BoardID string `json:"boardId"`
	// ItemID is the stable ID returned by ListBoardItems.
	ItemID string `json:"itemId"`
}

// ListBoardMembersInput selects one page of board members.
type ListBoardMembersInput struct {
	// BoardID is the ID after /board/ in a Miro board URL.
	BoardID string `json:"boardId"`
	// Offset is the zero-based starting index; zero starts at the first member.
	Offset int `json:"offset"`
	// Limit is the number of members per page, from 1 to 50.
	Limit int `json:"limit"`
}

// BoardMember identifies a board member and their role.
type BoardMember struct {
	// ID is the stable Miro member ID.
	ID string `json:"id"`
	// Role is the member's board role.
	Role string `json:"role"`
}

// BoardMembersPage contains one member page.
type BoardMembersPage struct {
	// Members contains the returned member identities and roles.
	Members []BoardMember `json:"members"`
	// Offset is the zero-based starting index of this page.
	Offset int `json:"offset"`
	// Total is the provider-reported count of board members.
	Total int `json:"total"`
}

// ListBoardsOperation reads one page of accessible boards.
type ListBoardsOperation struct{ client *Client }

// ListBoards binds the Query to this client.
func (client *Client) ListBoards() ListBoardsOperation { return ListBoardsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListBoardsOperation) Definition() sdkgo.QueryDefinition { return ListBoardsDefinition }

// Invoke reads one board page and classifies the response.
func (operation ListBoardsOperation) Invoke(call sdkgo.Call, input ListBoardsInput) sdkgo.QueryAttempt[BoardsPage] {
	if input.Offset < 0 || input.Limit < 1 || input.Limit > 50 || len(input.Query) > 500 || strings.ContainsAny(input.Query, "\r\n") {
		return miroReadFailure[BoardsPage]("listBoards", ListBoardsBranchDefect, sdkgo.FailureValidation, "Miro board query, offset, or limit is invalid", 0)
	}
	query := url.Values{"offset": {stringInt(input.Offset)}, "limit": {stringInt(input.Limit)}}
	if input.Query != "" {
		query.Set("query", input.Query)
	}
	response := operation.client.get(call, "/boards?"+query.Encode())
	if attempt, failed := classifyMiroReadResponse[BoardsPage]("listBoards", response, ListBoardsBranchProviderRejected, ListBoardsBranchInvalidResponse, ListBoardsBranchDefect, ""); failed {
		return attempt
	}
	var wire struct {
		Data   []json.RawMessage `json:"data"`
		Offset *int              `json:"offset"`
		Total  *int              `json:"total"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || wire.Offset == nil || wire.Total == nil || *wire.Offset != input.Offset || *wire.Total < 0 {
		return miroReadFailure[BoardsPage]("listBoards", ListBoardsBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid board page JSON", 0)
	}
	output := BoardsPage{Boards: make([]BoardMetadata, 0, len(wire.Data)), Offset: *wire.Offset, Total: *wire.Total}
	for _, record := range wire.Data {
		var board struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(record, &board) != nil || board.ID == "" || strings.TrimSpace(board.Name) == "" {
			return miroReadFailure[BoardsPage]("listBoards", ListBoardsBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid board metadata", 0)
		}
		output.Boards = append(output.Boards, BoardMetadata{ID: board.ID, Name: board.Name})
	}
	return sdkgo.NewQueryBranch(ListBoardsBranchListed, output, nil, sdkgo.Receipt{})
}

// ListBoardItemsOperation reads one page of board items.
type ListBoardItemsOperation struct{ client *Client }

// ListBoardItems binds the Query to this client.
func (client *Client) ListBoardItems() ListBoardItemsOperation {
	return ListBoardItemsOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListBoardItemsOperation) Definition() sdkgo.QueryDefinition { return ListBoardItemsDefinition }

// Invoke reads one item page and classifies the response.
func (operation ListBoardItemsOperation) Invoke(call sdkgo.Call, input ListBoardItemsInput) sdkgo.QueryAttempt[BoardItemsPage] {
	if !validBoardID(input.BoardID) || input.Limit < 10 || input.Limit > 50 || len(input.Cursor) > 1024 || strings.ContainsAny(input.Cursor, "\r\n") {
		return miroReadFailure[BoardItemsPage]("listBoardItems", ListBoardItemsBranchDefect, sdkgo.FailureValidation, "Miro board ID, cursor, or limit is invalid", 0)
	}
	query := url.Values{"limit": {stringInt(input.Limit)}}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	response := operation.client.get(call, "/boards/"+url.PathEscape(input.BoardID)+"/items?"+query.Encode())
	if attempt, failed := classifyMiroReadResponse[BoardItemsPage]("listBoardItems", response, ListBoardItemsBranchProviderRejected, ListBoardItemsBranchInvalidResponse, ListBoardItemsBranchDefect, ListBoardItemsBranchNotFound); failed {
		return attempt
	}
	var wire struct {
		Data   []json.RawMessage `json:"data"`
		Cursor string            `json:"cursor"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Cursor) > 1024 {
		return miroReadFailure[BoardItemsPage]("listBoardItems", ListBoardItemsBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid item page JSON", 0)
	}
	output := BoardItemsPage{Items: make([]BoardItemMetadata, 0, len(wire.Data)), Cursor: wire.Cursor}
	for _, record := range wire.Data {
		item, valid := decodeBoardItem(record)
		if !valid {
			return miroReadFailure[BoardItemsPage]("listBoardItems", ListBoardItemsBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid board item", 0)
		}
		output.Items = append(output.Items, item)
	}
	return sdkgo.NewQueryBranch(ListBoardItemsBranchListed, output, nil, sdkgo.Receipt{})
}

// GetBoardItemOperation reads one board item.
type GetBoardItemOperation struct{ client *Client }

// GetBoardItem binds the Query to this client.
func (client *Client) GetBoardItem() GetBoardItemOperation { return GetBoardItemOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetBoardItemOperation) Definition() sdkgo.QueryDefinition { return GetBoardItemDefinition }

// Invoke reads one item and classifies the response.
func (operation GetBoardItemOperation) Invoke(call sdkgo.Call, input GetBoardItemInput) sdkgo.QueryAttempt[BoardItemMetadata] {
	if !validBoardID(input.BoardID) || !validBoardID(input.ItemID) {
		return miroReadFailure[BoardItemMetadata]("getBoardItem", GetBoardItemBranchDefect, sdkgo.FailureValidation, "Miro board and item IDs are required", 0)
	}
	response := operation.client.get(call, "/boards/"+url.PathEscape(input.BoardID)+"/items/"+url.PathEscape(input.ItemID))
	if attempt, failed := classifyMiroReadResponse[BoardItemMetadata]("getBoardItem", response, GetBoardItemBranchProviderRejected, GetBoardItemBranchInvalidResponse, GetBoardItemBranchDefect, GetBoardItemBranchNotFound); failed {
		return attempt
	}
	item, valid := decodeBoardItem(response.body)
	if !valid || item.ID != input.ItemID {
		return miroReadFailure[BoardItemMetadata]("getBoardItem", GetBoardItemBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid board item", 0)
	}
	return sdkgo.NewQueryBranch(GetBoardItemBranchFound, item, nil, sdkgo.Receipt{})
}

// ListBoardMembersOperation reads one page of board members.
type ListBoardMembersOperation struct{ client *Client }

// ListBoardMembers binds the Query to this client.
func (client *Client) ListBoardMembers() ListBoardMembersOperation {
	return ListBoardMembersOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListBoardMembersOperation) Definition() sdkgo.QueryDefinition {
	return ListBoardMembersDefinition
}

// Invoke reads one member page and classifies the response.
func (operation ListBoardMembersOperation) Invoke(call sdkgo.Call, input ListBoardMembersInput) sdkgo.QueryAttempt[BoardMembersPage] {
	if !validBoardID(input.BoardID) || input.Offset < 0 || input.Limit < 1 || input.Limit > 50 {
		return miroReadFailure[BoardMembersPage]("listBoardMembers", ListBoardMembersBranchDefect, sdkgo.FailureValidation, "Miro board ID, offset, or limit is invalid", 0)
	}
	query := url.Values{"offset": {stringInt(input.Offset)}, "limit": {stringInt(input.Limit)}}
	response := operation.client.get(call, "/boards/"+url.PathEscape(input.BoardID)+"/members?"+query.Encode())
	if attempt, failed := classifyMiroReadResponse[BoardMembersPage]("listBoardMembers", response, ListBoardMembersBranchProviderRejected, ListBoardMembersBranchInvalidResponse, ListBoardMembersBranchDefect, ListBoardMembersBranchNotFound); failed {
		return attempt
	}
	var wire struct {
		Data   []BoardMember `json:"data"`
		Offset *int          `json:"offset"`
		Total  *int          `json:"total"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || wire.Offset == nil || wire.Total == nil || *wire.Offset != input.Offset || *wire.Total < 0 {
		return miroReadFailure[BoardMembersPage]("listBoardMembers", ListBoardMembersBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid member page JSON", 0)
	}
	for _, member := range wire.Data {
		if member.ID == "" || member.Role == "" {
			return miroReadFailure[BoardMembersPage]("listBoardMembers", ListBoardMembersBranchInvalidResponse, sdkgo.FailureProtocol, "Miro returned invalid board member", 0)
		}
	}
	return sdkgo.NewQueryBranch(ListBoardMembersBranchListed, BoardMembersPage{Members: wire.Data, Offset: *wire.Offset, Total: *wire.Total}, nil, sdkgo.Receipt{})
}

func stringInt(value int) string { return strconv.Itoa(value) }

func decodeBoardItem(body []byte) (BoardItemMetadata, bool) {
	var wire struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.ID == "" || wire.Type == "" {
		return BoardItemMetadata{}, false
	}
	return BoardItemMetadata{ID: wire.ID, Type: wire.Type, Content: wire.Data.Content}, true
}

func classifyMiroReadResponse[T any](operation string, response providerResponse, rejected, invalid, defect, notFound sdkgo.BranchID) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return miroReadFailure[T](operation, defect, sdkgo.FailureAuthentication, "Miro credentials are unavailable or invalid", 0), true
	}
	if errors.Is(response.err, errRequest) {
		return miroReadRetry[T](operation, sdkgo.FailureTransport, "Miro request failed", 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return miroReadRetry[T](operation, sdkgo.FailureRateLimit, "Miro rate limited the request", response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return miroReadRetry[T](operation, sdkgo.FailureAvailability, "Miro is temporarily unavailable", response.retryAfter), true
	}
	if response.status == http.StatusNotFound && notFound != "" {
		return miroReadFailure[T](operation, notFound, sdkgo.FailureNotFound, "Miro did not expose the resource", 0), true
	}
	if response.status != http.StatusOK {
		return miroReadFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "Miro rejected the request", 0), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return miroReadFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "Miro response exceeded the configured limit", 0), true
	}
	if response.err != nil {
		return miroReadRetry[T](operation, sdkgo.FailureTransport, "Miro response was interrupted", 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func miroReadFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, _ time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "miro", Operation: operation, Message: message}
	var zero T
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func miroReadRetry[T any](operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "miro", Operation: operation, Message: message}, retryAfter)
}
