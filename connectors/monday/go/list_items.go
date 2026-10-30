// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listItemsOperationID = "listItems"
	// DefaultListItemsLimit is the page size when ListItemsInput.Limit is zero, monday.com's own default.
	DefaultListItemsLimit = 25
	// MaxListItemsLimit bounds one page; monday.com allows 500, and the connector keeps Step state smaller.
	MaxListItemsLimit = 100
	maxCursorBytes    = 4096

	listBoardItemsDocument = `query ListBoardItems($boardIds: [ID!], $limit: Int!, $queryParams: ItemsQuery, $columnIds: [String!]) {
  boards(ids: $boardIds) { id items_page(limit: $limit, query_params: $queryParams) { cursor items { ` + itemFields + ` } } }
}`
	listNextBoardItemsDocument = `query ListNextBoardItems($cursor: String!, $limit: Int!, $columnIds: [String!]) {
  next_items_page(cursor: $cursor, limit: $limit) { cursor items { ` + itemFields + ` } }
}`
)

// ListItemsInput selects one page of a board's active items. The first page sets BoardID with
// an optional Filter and Order; a later page sets BoardID and the previous page's NextCursor
// only, because monday.com's cursor carries the filter and rejects a new one.
type ListItemsInput struct {
	// BoardID is the numeric board ID, the number after /boards/ in the board's URL.
	BoardID string `json:"boardId"`
	// Filter limits the page to items whose column values match; the zero value lists every active item.
	Filter ItemFilter `json:"filter,omitzero"`
	// Order orders the listing by one column or timestamp; nil keeps monday.com's board order.
	Order *ItemOrder `json:"order,omitempty"`
	// Limit is the page size from 1 to 100; zero means 25.
	Limit int `json:"limit,omitempty"`
	// Cursor continues a listing with the previous page's NextCursor; monday.com expires it 60 minutes after the first page.
	Cursor string `json:"cursor,omitempty"`
	// ColumnIDs limits each item's column values to at most 50 columns; empty returns every column.
	ColumnIDs []string `json:"columnIds,omitempty"`
}

// ListItemsOutput is one page of items.
type ListItemsOutput struct {
	// BoardID echoes the requested board.
	BoardID string `json:"boardId"`
	// Items are the page's active items in monday.com's order.
	Items []Item `json:"items"`
	// NextCursor continues the listing; empty means this is the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListItemsOperation implements the listItems Query.
type ListItemsOperation struct{ client *Client }

type itemsPageResource struct {
	Cursor *string        `json:"cursor"`
	Items  []itemResource `json:"items"`
}

// Definition returns the immutable connector operation definition.
func (ListItemsOperation) Definition() sdkgo.QueryDefinition { return ListItemsDefinition }

// Invoke reads one page. Rate limits, 5xx, and transport failures are retried.
func (operation ListItemsOperation) Invoke(call sdkgo.Call, input ListItemsInput) sdkgo.QueryAttempt[ListItemsOutput] {
	client := operation.client
	request, output, err := buildListItemsRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListItemsBranchDefect, output, mondayFailurePointer(sdkgo.FailureValidation, listItemsOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, listItemsOperationID, request)
	receipt := client.receipt(call, result.response, output.BoardID)
	if attempt, isTerminal := queryAttemptForExchange(result, output, receipt, queryBranches{
		notFound: ListItemsBranchNotFound, providerRejected: ListItemsBranchProviderRejected,
		invalidResponse: ListItemsBranchInvalidResponse, defect: ListItemsBranchDefect,
	}); isTerminal {
		return attempt
	}
	page, isBoardFound, err := decodeItemsPage(result.response.data, input.Cursor != "")
	switch {
	case err != nil:
		return sdkgo.NewQueryBranch(ListItemsBranchInvalidResponse, output, mondayFailurePointer(sdkgo.FailureProtocol, listItemsOperationID, "monday.com returned an invalid page of items: "+err.Error()), receipt)
	case !isBoardFound:
		return sdkgo.NewQueryBranch(ListItemsBranchNotFound, output, mondayFailurePointer(sdkgo.FailureNotFound, listItemsOperationID, "monday.com found no board with this ID that the connection can see"), receipt)
	}
	output.Items, output.NextCursor = page.Items, page.NextCursor
	return sdkgo.NewQueryBranch(ListItemsBranchListed, output, nil, receipt)
}

// buildListItemsRequest validates input and returns the request plus the echo used by every branch.
func buildListItemsRequest(input ListItemsInput) (graphQLRequest, ListItemsOutput, error) {
	output := ListItemsOutput{BoardID: input.BoardID, Items: []Item{}}
	boardID, err := validateNumericID(input.BoardID, "boardId")
	if err != nil {
		return graphQLRequest{}, output, err
	}
	output.BoardID = boardID
	limit := input.Limit
	switch {
	case limit == 0:
		limit = DefaultListItemsLimit
	case limit < 1 || limit > MaxListItemsLimit:
		return graphQLRequest{}, output, fmt.Errorf("limit must be from 1 to %d", MaxListItemsLimit)
	}
	columnIDs, err := validateRequestedColumnIDs(input.ColumnIDs)
	if err != nil {
		return graphQLRequest{}, output, err
	}
	if input.Cursor != "" {
		if !input.Filter.isZero() || input.Order != nil {
			return graphQLRequest{}, output, errors.New("a cursor continues its first page's filter and order, so set neither with it")
		}
		if len(input.Cursor) > maxCursorBytes || !isPrintableASCIIWithoutSpaces(input.Cursor) {
			return graphQLRequest{}, output, errors.New("cursor must be the nextCursor value of a previous page")
		}
		return graphQLRequest{document: listNextBoardItemsDocument, variables: map[string]any{
			"cursor": input.Cursor, "limit": limit, "columnIds": optionalStringList(columnIDs),
		}}, output, nil
	}
	queryParams, err := encodeItemsQuery(input.Filter, input.Order)
	if err != nil {
		return graphQLRequest{}, output, err
	}
	variables := map[string]any{"boardIds": []string{boardID}, "limit": limit, "columnIds": optionalStringList(columnIDs)}
	if queryParams != nil {
		variables["queryParams"] = queryParams
	}
	return graphQLRequest{document: listBoardItemsDocument, variables: variables}, output, nil
}

type decodedItemsPage struct {
	Items      []Item
	NextCursor string
}

// decodeItemsPage reads boards[0].items_page, or next_items_page for a continued listing.
func decodeItemsPage(data json.RawMessage, isContinuation bool) (decodedItemsPage, bool, error) {
	var resource itemsPageResource
	if isContinuation {
		var document struct {
			NextItemsPage *itemsPageResource `json:"next_items_page"`
		}
		if err := json.Unmarshal(data, &document); err != nil || document.NextItemsPage == nil {
			return decodedItemsPage{}, true, errors.New("next_items_page is missing")
		}
		resource = *document.NextItemsPage
	} else {
		var document struct {
			Boards []*struct {
				ID        string             `json:"id"`
				ItemsPage *itemsPageResource `json:"items_page"`
			} `json:"boards"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			return decodedItemsPage{}, true, errors.New("boards is not a list")
		}
		if len(document.Boards) == 0 || document.Boards[0] == nil {
			return decodedItemsPage{}, false, nil
		}
		if len(document.Boards) != 1 || document.Boards[0].ItemsPage == nil {
			return decodedItemsPage{}, true, errors.New("expected one board with an items_page")
		}
		resource = *document.Boards[0].ItemsPage
	}
	page := decodedItemsPage{Items: make([]Item, 0, len(resource.Items))}
	if resource.Cursor != nil {
		if len(*resource.Cursor) > maxCursorBytes || !isPrintableASCIIWithoutSpaces(*resource.Cursor) {
			return decodedItemsPage{}, true, errors.New("cursor is not a printable token")
		}
		page.NextCursor = *resource.Cursor
	}
	if len(resource.Items) > MaxListItemsLimit {
		return decodedItemsPage{}, true, fmt.Errorf("page holds more than %d items", MaxListItemsLimit)
	}
	for _, itemResource := range resource.Items {
		item, err := decodeItemResource(itemResource)
		if err != nil {
			return decodedItemsPage{}, true, err
		}
		page.Items = append(page.Items, item)
	}
	return page, true, nil
}

func isPrintableASCIIWithoutSpaces(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
