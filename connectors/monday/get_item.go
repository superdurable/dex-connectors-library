// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"encoding/json"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getItemOperationID = "getItem"

	getItemDocument = `query GetItem($itemIds: [ID!], $excludeNonactive: Boolean, $columnIds: [String!]) {
  items(ids: $itemIds, exclude_nonactive: $excludeNonactive) { ` + itemFields + ` }
}`
)

// GetItemInput identifies one item.
type GetItemInput struct {
	// ItemID is the numeric item ID, such as the ID listItems returns.
	ItemID string `json:"itemId"`
	// ColumnIDs limits the column values to at most 50 columns; empty returns every column.
	ColumnIDs []string `json:"columnIds,omitempty"`
	// IncludesInactive also returns an archived or deleted item, with its State, instead of notFound.
	IncludesInactive bool `json:"includesInactive,omitempty"`
}

// GetItemOperation implements the getItem Query.
type GetItemOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetItemOperation) Definition() sdkgo.QueryDefinition { return GetItemDefinition }

// Invoke reads one item. Rate limits, 5xx, and transport failures are retried.
func (operation GetItemOperation) Invoke(call sdkgo.Call, input GetItemInput) sdkgo.QueryAttempt[Item] {
	client := operation.client
	itemID, err := validateNumericID(input.ItemID, "itemId")
	if err != nil {
		return sdkgo.NewQueryBranch(GetItemBranchDefect, Item{}, mondayFailurePointer(sdkgo.FailureValidation, getItemOperationID, err.Error()), sdkgo.Receipt{})
	}
	columnIDs, err := validateRequestedColumnIDs(input.ColumnIDs)
	if err != nil {
		return sdkgo.NewQueryBranch(GetItemBranchDefect, Item{}, mondayFailurePointer(sdkgo.FailureValidation, getItemOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, getItemOperationID, graphQLRequest{document: getItemDocument, variables: map[string]any{
		"itemIds": []string{itemID}, "excludeNonactive": !input.IncludesInactive, "columnIds": optionalStringList(columnIDs),
	}})
	receipt := client.receipt(call, result.response, itemID)
	if attempt, isTerminal := queryAttemptForExchange(result, Item{}, receipt, queryBranches{
		notFound: GetItemBranchNotFound, providerRejected: GetItemBranchProviderRejected,
		invalidResponse: GetItemBranchInvalidResponse, defect: GetItemBranchDefect,
	}); isTerminal {
		return attempt
	}
	var document struct {
		Items []*itemResource `json:"items"`
	}
	if err := json.Unmarshal(result.response.data, &document); err != nil || len(document.Items) > 1 {
		return sdkgo.NewQueryBranch(GetItemBranchInvalidResponse, Item{}, mondayFailurePointer(sdkgo.FailureProtocol, getItemOperationID, "monday.com returned an invalid item list"), receipt)
	}
	if len(document.Items) == 0 || document.Items[0] == nil {
		return sdkgo.NewQueryBranch(GetItemBranchNotFound, Item{}, mondayFailurePointer(sdkgo.FailureNotFound, getItemOperationID, "monday.com found no item with this ID that the connection can see"), receipt)
	}
	item, err := decodeItemResource(*document.Items[0])
	if err != nil || item.ID != itemID {
		return sdkgo.NewQueryBranch(GetItemBranchInvalidResponse, Item{}, mondayFailurePointer(sdkgo.FailureProtocol, getItemOperationID, "monday.com returned an invalid item"), receipt)
	}
	return sdkgo.NewQueryBranch(GetItemBranchFound, item, nil, receipt)
}
