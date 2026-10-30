// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"errors"
	"sort"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateItemColumnValuesOperationID = "updateItemColumnValues"

	updateItemColumnValuesDocument = `mutation UpdateItemColumnValues($boardId: ID!, $itemId: ID, $columnValues: JSON!, $createLabelsIfMissing: Boolean, $columnIds: [String!]) {
  change_multiple_column_values(board_id: $boardId, item_id: $itemId, column_values: $columnValues, create_labels_if_missing: $createLabelsIfMissing) { ` + itemFields + ` }
}`
)

// UpdateItemColumnValuesInput sets absolute values on columns of one item. Columns left out keep their values.
type UpdateItemColumnValuesInput struct {
	// BoardID is the numeric ID of the board that holds the item.
	BoardID string `json:"boardId"`
	// ItemID is the numeric item ID.
	ItemID string `json:"itemId"`
	// ColumnValues maps 1 to 50 column IDs, such as status or date4, to typed values.
	ColumnValues map[string]ColumnValue `json:"columnValues"`
	// CreatesLabelsIfMissing lets monday.com add a missing status or dropdown label, which needs
	// permission to change the board structure; false rejects an unknown label.
	CreatesLabelsIfMissing bool `json:"createsLabelsIfMissing,omitempty"`
}

// UpdateItemColumnValuesOutput is the item after the change. On every other branch Item is nil and
// the requested board and item are echoed.
type UpdateItemColumnValuesOutput struct {
	// BoardID echoes the requested board.
	BoardID string `json:"boardId"`
	// ItemID echoes the requested item.
	ItemID string `json:"itemId"`
	// Item is the changed item with the values of the columns this Step set.
	Item *Item `json:"item,omitempty"`
	// IsReplayed reports that monday.com answered an earlier attempt's cached result for this Step's idempotency key.
	IsReplayed bool `json:"replayed,omitempty"`
}

// UpdateItemColumnValuesOperation implements the updateItemColumnValues Mutation.
type UpdateItemColumnValuesOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (UpdateItemColumnValuesOperation) Definition() sdkgo.MutationDefinition {
	return UpdateItemColumnValuesDefinition
}

// IdempotencyKey is the stable Call ID, sent as monday.com's Idempotency-Key header.
// The values are absolute, so a repeat is safe even after the key's 30-minute cache expires.
func (UpdateItemColumnValuesOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateItemColumnValuesInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the column values. Every unconfirmed outcome is retried, because a repeat writes the same values.
func (operation UpdateItemColumnValuesOperation) Invoke(call sdkgo.Call, input UpdateItemColumnValuesInput) sdkgo.MutationAttempt[UpdateItemColumnValuesOutput] {
	client := operation.client
	request, requested, err := buildUpdateItemColumnValuesRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateItemColumnValuesBranchDefect, requested, mondayFailurePointer(sdkgo.FailureValidation, updateItemColumnValuesOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, updateItemColumnValuesOperationID, request)
	receipt := client.receipt(call, result.response, requested.ItemID)
	if item, isUpdated := decodeMutationItem(result.response.data, "change_multiple_column_values"); isUpdated && item.ID == requested.ItemID {
		output := requested
		output.Item, output.IsReplayed = &item, result.response.isReplayed
		return sdkgo.NewMutationBranch(UpdateItemColumnValuesBranchUpdated, output, nil, receipt)
	}
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		notFound: UpdateItemColumnValuesBranchNotFound, providerRejected: UpdateItemColumnValuesBranchProviderRejected,
		defect: UpdateItemColumnValuesBranchDefect, invalidResponse: UpdateItemColumnValuesBranchInvalidResponse,
	}); isTerminal {
		return attempt
	}
	return sdkgo.NewMutationBranch(UpdateItemColumnValuesBranchInvalidResponse, requested, mondayFailurePointer(sdkgo.FailureProtocol,
		updateItemColumnValuesOperationID, "monday.com returned an unusable item, so the values may have been set"), receipt)
}

// buildUpdateItemColumnValuesRequest validates input and returns the request plus the echo used by every branch.
func buildUpdateItemColumnValuesRequest(input UpdateItemColumnValuesInput) (graphQLRequest, UpdateItemColumnValuesOutput, error) {
	requested := UpdateItemColumnValuesOutput{BoardID: input.BoardID, ItemID: input.ItemID}
	boardID, err := validateNumericID(input.BoardID, "boardId")
	if err != nil {
		return graphQLRequest{}, requested, err
	}
	itemID, err := validateNumericID(input.ItemID, "itemId")
	if err != nil {
		return graphQLRequest{}, requested, err
	}
	requested.BoardID, requested.ItemID = boardID, itemID
	if len(input.ColumnValues) == 0 {
		return graphQLRequest{}, requested, errors.New("columnValues needs at least one column")
	}
	columnValues, err := encodeColumnValues(input.ColumnValues, "columnValues")
	if err != nil {
		return graphQLRequest{}, requested, err
	}
	columnIDs := make([]string, 0, len(input.ColumnValues))
	for columnID := range input.ColumnValues {
		columnIDs = append(columnIDs, columnID)
	}
	sort.Strings(columnIDs)
	return graphQLRequest{document: updateItemColumnValuesDocument, isMutation: true, variables: map[string]any{
		"boardId": boardID, "itemId": itemID, "columnValues": columnValues,
		"createLabelsIfMissing": input.CreatesLabelsIfMissing, "columnIds": columnIDs,
	}}, requested, nil
}
