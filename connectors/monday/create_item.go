// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createItemOperationID = "createItem"
	// MaxItemNameCharacters is monday.com's item name limit.
	MaxItemNameCharacters = 255

	createItemDocument = `mutation CreateItem($boardId: ID!, $groupId: String, $itemName: String!, $columnValues: JSON, $createLabelsIfMissing: Boolean) {
  create_item(board_id: $boardId, group_id: $groupId, item_name: $itemName, column_values: $columnValues, create_labels_if_missing: $createLabelsIfMissing) { ` + createdItemFields + ` }
}`
)

// CreateItemInput describes one new item.
type CreateItemInput struct {
	// BoardID is the numeric board ID, the number after /boards/ in the board's URL.
	BoardID string `json:"boardId"`
	// GroupID places the item in this group, such as topics; blank uses the board's first active group.
	GroupID string `json:"groupId,omitempty"`
	// ItemName is the item name, 1 to 255 characters on one line.
	ItemName string `json:"itemName"`
	// ColumnValues maps at most 50 column IDs, such as status or date4, to typed values.
	ColumnValues map[string]ColumnValue `json:"columnValues,omitempty"`
	// CreatesLabelsIfMissing lets monday.com add a missing status or dropdown label, which needs
	// permission to change the board structure; false rejects an unknown label.
	CreatesLabelsIfMissing bool `json:"createsLabelsIfMissing,omitempty"`
}

// CreateItemOutput identifies the created item. On every other branch ItemID is empty and the
// requested board, group, and name are echoed so the application can reconcile.
type CreateItemOutput struct {
	// BoardID echoes the requested board.
	BoardID string `json:"boardId"`
	// GroupID echoes the requested group; blank when none was requested.
	GroupID string `json:"groupId,omitempty"`
	// ItemName echoes the requested name.
	ItemName string `json:"itemName"`
	// ItemID is the created item's numeric ID.
	ItemID string `json:"itemId,omitempty"`
	// Item is the created item without column values, which keeps the response replayable.
	Item *Item `json:"item,omitempty"`
	// IsReplayed reports that monday.com answered an earlier attempt's cached result for this Step's idempotency key.
	IsReplayed bool `json:"replayed,omitempty"`
}

// CreateItemOperation implements the createItem Mutation.
type CreateItemOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CreateItemOperation) Definition() sdkgo.MutationDefinition { return CreateItemDefinition }

// IdempotencyKey is the stable Call ID, sent as monday.com's Idempotency-Key header. Every attempt of
// one Step execution, including one on a replacement Worker, sends the same key.
func (CreateItemOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one item. Rate limits, an idempotency conflict, 5xx, and transport failures are
// retried under the same key, so monday.com replays the first result instead of creating a second item.
func (operation CreateItemOperation) Invoke(call sdkgo.Call, input CreateItemInput) sdkgo.MutationAttempt[CreateItemOutput] {
	client := operation.client
	request, requested, err := buildCreateItemRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateItemBranchDefect, requested, mondayFailurePointer(sdkgo.FailureValidation, createItemOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, createItemOperationID, request)
	receipt := client.receipt(call, result.response, "")
	if item, isCreated := decodeMutationItem(result.response.data, "create_item"); isCreated {
		output := requested
		output.ItemID, output.Item, output.IsReplayed = item.ID, &item, result.response.isReplayed
		receipt.ProviderObjectID = item.ID
		return sdkgo.NewMutationBranch(CreateItemBranchCreated, output, nil, receipt)
	}
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		notFound: CreateItemBranchNotFound, providerRejected: CreateItemBranchProviderRejected, defect: CreateItemBranchDefect,
	}); isTerminal {
		return attempt
	}
	return sdkgo.NewMutationUncertain(requested, mondayFailure(sdkgo.FailureProtocol, createItemOperationID,
		"monday.com accepted the item but returned an unusable item"), receipt)
}

// buildCreateItemRequest validates input and returns the request plus the echo used by every branch.
func buildCreateItemRequest(input CreateItemInput) (graphQLRequest, CreateItemOutput, error) {
	requested := CreateItemOutput{BoardID: strings.TrimSpace(input.BoardID), GroupID: strings.TrimSpace(input.GroupID), ItemName: input.ItemName}
	boardID, err := validateNumericID(input.BoardID, "boardId")
	if err != nil {
		return graphQLRequest{}, requested, err
	}
	if requested.GroupID != "" && !groupIDPattern.MatchString(requested.GroupID) {
		return graphQLRequest{}, requested, errors.New("groupId must be a monday.com group ID such as topics")
	}
	itemName, err := validateItemName(input.ItemName)
	if err != nil {
		return graphQLRequest{}, requested, err
	}
	requested.ItemName = itemName
	variables := map[string]any{"boardId": boardID, "itemName": itemName, "createLabelsIfMissing": input.CreatesLabelsIfMissing}
	if requested.GroupID != "" {
		variables["groupId"] = requested.GroupID
	}
	if len(input.ColumnValues) != 0 {
		columnValues, err := encodeColumnValues(input.ColumnValues, "columnValues")
		if err != nil {
			return graphQLRequest{}, requested, err
		}
		variables["columnValues"] = columnValues
	}
	return graphQLRequest{document: createItemDocument, variables: variables, isMutation: true}, requested, nil
}

// validateItemName checks a one-line item name within monday.com's 255-character limit.
func validateItemName(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return "", errors.New("itemName is required")
	case !utf8.ValidString(trimmed):
		return "", errors.New("itemName must be valid UTF-8")
	case utf8.RuneCountInString(trimmed) > MaxItemNameCharacters:
		return "", fmt.Errorf("itemName cannot exceed %d characters", MaxItemNameCharacters)
	}
	for _, character := range trimmed {
		if character < ' ' || character == 0x7f {
			return "", errors.New("itemName must be one line without control characters")
		}
	}
	return trimmed, nil
}
