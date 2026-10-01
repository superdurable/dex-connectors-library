// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	addUpdateOperationID = "addUpdate"
	// MaxUpdateBodyCharacters bounds the plain-text body one addUpdate sends; monday.com documents no limit.
	MaxUpdateBodyCharacters = 10000

	addUpdateDocument = `mutation AddUpdate($itemId: ID, $body: String!) {
  create_update(item_id: $itemId, body: $body) { id item_id created_at creator_id }
}`
)

// AddUpdateInput is one update, monday.com's comment on an item.
type AddUpdateInput struct {
	// ItemID is the numeric ID of the item to comment on.
	ItemID string `json:"itemId"`
	// Body is plain text of at most 10,000 characters. The connector escapes it into monday.com's
	// HTML update body, so markup is shown literally and line breaks are kept.
	Body string `json:"body"`
}

// AddUpdateOutput identifies the added update. On every other branch UpdateID is empty.
type AddUpdateOutput struct {
	// ItemID echoes the requested item.
	ItemID string `json:"itemId"`
	// UpdateID is the new update's numeric ID.
	UpdateID string `json:"updateId,omitempty"`
	// CreatorID is the numeric ID of the user the update is posted as.
	CreatorID string `json:"creatorId,omitempty"`
	// CreatedAt is when monday.com recorded the update, or zero when its timestamp is unreadable.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// IsReplayed reports that monday.com answered an earlier attempt's cached result for this Step's idempotency key.
	IsReplayed bool `json:"replayed,omitempty"`
}

// AddUpdateOperation implements the addUpdate Mutation.
type AddUpdateOperation struct{ client *Client }

type createdUpdateResource struct {
	ID        string  `json:"id"`
	ItemID    *string `json:"item_id"`
	CreatedAt string  `json:"created_at"`
	CreatorID *string `json:"creator_id"`
}

// Definition returns the immutable connector operation definition.
func (AddUpdateOperation) Definition() sdkgo.MutationDefinition { return AddUpdateDefinition }

// IdempotencyKey is the stable Call ID, sent as monday.com's Idempotency-Key header. Every attempt of
// one Step execution, including one on a replacement Worker, sends the same key.
func (AddUpdateOperation) IdempotencyKey(callID sdkgo.CallID, _ AddUpdateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one update. Rate limits, an idempotency conflict, 5xx, and transport failures are
// retried under the same key, so monday.com replays the first result instead of posting a second update.
func (operation AddUpdateOperation) Invoke(call sdkgo.Call, input AddUpdateInput) sdkgo.MutationAttempt[AddUpdateOutput] {
	client := operation.client
	requested := AddUpdateOutput{ItemID: strings.TrimSpace(input.ItemID)}
	itemID, err := validateNumericID(input.ItemID, "itemId")
	if err != nil {
		return sdkgo.NewMutationBranch(AddUpdateBranchDefect, requested, mondayFailurePointer(sdkgo.FailureValidation, addUpdateOperationID, err.Error()), sdkgo.Receipt{})
	}
	body, err := convertPlainTextToUpdateBody(input.Body)
	if err != nil {
		return sdkgo.NewMutationBranch(AddUpdateBranchDefect, requested, mondayFailurePointer(sdkgo.FailureValidation, addUpdateOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, addUpdateOperationID, graphQLRequest{
		document: addUpdateDocument, variables: map[string]any{"itemId": itemID, "body": body}, isMutation: true,
	})
	receipt := client.receipt(call, result.response, "")
	if update, isAdded := decodeCreatedUpdate(result.response.data, itemID); isAdded {
		output := requested
		output.UpdateID, output.CreatorID, output.CreatedAt, output.IsReplayed = update.UpdateID, update.CreatorID, update.CreatedAt, result.response.isReplayed
		receipt.ProviderObjectID = update.UpdateID
		return sdkgo.NewMutationBranch(AddUpdateBranchAdded, output, nil, receipt)
	}
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		notFound: AddUpdateBranchNotFound, providerRejected: AddUpdateBranchProviderRejected, defect: AddUpdateBranchDefect,
	}); isTerminal {
		return attempt
	}
	return sdkgo.NewMutationUncertain(requested, mondayFailure(sdkgo.FailureProtocol, addUpdateOperationID,
		"monday.com accepted the update but returned an unusable update"), receipt)
}

// decodeCreatedUpdate reads create_update; an item_id that differs from the request is unusable.
func decodeCreatedUpdate(data json.RawMessage, itemID string) (AddUpdateOutput, bool) {
	if data == nil {
		return AddUpdateOutput{}, false
	}
	var document struct {
		CreateUpdate *createdUpdateResource `json:"create_update"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.CreateUpdate == nil || !numericIDPattern.MatchString(document.CreateUpdate.ID) {
		return AddUpdateOutput{}, false
	}
	update := document.CreateUpdate
	if update.ItemID != nil && *update.ItemID != "" && *update.ItemID != itemID {
		return AddUpdateOutput{}, false
	}
	output := AddUpdateOutput{ItemID: itemID, UpdateID: update.ID, CreatedAt: parseMondayTime(update.CreatedAt)}
	if update.CreatorID != nil && numericIDPattern.MatchString(*update.CreatorID) {
		output.CreatorID = *update.CreatorID
	}
	return output, true
}

// convertPlainTextToUpdateBody escapes text into monday.com's HTML update body, keeping line breaks.
func convertPlainTextToUpdateBody(text string) (string, error) {
	switch {
	case strings.TrimSpace(text) == "":
		return "", errors.New("body is required")
	case !utf8.ValidString(text):
		return "", errors.New("body must be valid UTF-8")
	case utf8.RuneCountInString(text) > MaxUpdateBodyCharacters:
		return "", fmt.Errorf("body is at most %d characters", MaxUpdateBodyCharacters)
	}
	for _, character := range text {
		if (character < ' ' && character != '\n' && character != '\r' && character != '\t') || character == 0x7f {
			return "", errors.New("body cannot contain control characters other than line breaks and tabs")
		}
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	for index, line := range lines {
		lines[index] = html.EscapeString(line)
	}
	return strings.Join(lines, "<br>"), nil
}
