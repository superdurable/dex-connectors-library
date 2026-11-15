// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ItemChangeKind is the derived kind of one itemChanged delivery.
type ItemChangeKind string

const (
	// ItemChangeCreated is an item the Trigger has not delivered before whose creation time falls after the
	// previous poll's position, minus the lookback window. An item created and then edited between two
	// polls is still created.
	ItemChangeCreated ItemChangeKind = "created"
	// ItemChangeUpdated is any other change monday.com reports by moving the item's last-updated time, such
	// as a column value, name, or group change.
	ItemChangeUpdated ItemChangeKind = "updated"
)

// ItemChangedTriggerConfiguration is the itemChanged binding configuration that the boardIdInput unit
// saves in Dex Web. Changing the board of a binding that already polled makes its next poll record a new
// baseline for the new board.
type ItemChangedTriggerConfiguration struct {
	// BoardID is the numeric ID of the watched board, the digits after /boards/ in the board's address,
	// such as 1234567890. It is required.
	BoardID string `json:"boardId"`
}

// Validate requires a numeric monday.com board ID without surrounding whitespace.
func (configuration ItemChangedTriggerConfiguration) Validate() error {
	boardID, err := validateNumericID(configuration.BoardID, "boardId")
	if err != nil {
		return err
	}
	if boardID != configuration.BoardID {
		return fmt.Errorf("boardId must not have surrounding whitespace")
	}
	return nil
}

// ItemChangedEvent is one active item created or changed on the watched board, as monday.com returned it
// in the poll's items page with every column value.
type ItemChangedEvent struct {
	// BoardID is the watched board.
	BoardID string `json:"boardId"`
	// ChangeKind is the derived change kind; see ItemChangeKind.
	ChangeKind ItemChangeKind `json:"changeKind"`
	// Item is the item with its group, timestamps, and every column value.
	Item Item `json:"item"`
}

// polledItem is one item read by a poll, with the content hash its snapshot entry and event ID use.
type polledItem struct {
	item Item
	hash string
	// encodedBytes is the size of the item's JSON, which bounds a batch.
	encodedBytes int
}

// newPolledItem hashes the item's canonical JSON, which includes its last-updated time and every column.
func newPolledItem(item Item) (polledItem, error) {
	encoded, err := json.Marshal(item)
	if err != nil {
		return polledItem{}, fmt.Errorf("encode monday.com item %s: %w", item.ID, err)
	}
	digest := sha256.Sum256(encoded)
	return polledItem{item: item, hash: hex.EncodeToString(digest[:16]), encodedBytes: len(encoded)}, nil
}

// SnapshotKey implements pollingtrigger.KeyedItem with the item ID.
func (polled polledItem) SnapshotKey() string { return polled.item.ID }

// SnapshotHash implements pollingtrigger.KeyedItem with the item content hash.
func (polled polledItem) SnapshotHash() string { return polled.hash }

// SnapshotValue keeps the item's last-updated time, so entries leave the snapshot once they fall behind
// the lookback window.
func (polled polledItem) SnapshotValue() json.RawMessage {
	encoded, _ := json.Marshal(snapshotValue{UpdatedAt: polled.item.UpdatedAt.UTC()})
	return encoded
}

type snapshotValue struct {
	UpdatedAt time.Time `json:"updatedAt"`
}

// newItemChangedTriggerEvent converts one changed item. Its ID is a pure function of the board, item ID,
// and item content, so a refetch after a crash yields the same ID.
func newItemChangedTriggerEvent(boardID string, polled polledItem, kind ItemChangeKind) sdkgo.TriggerEvent[ItemChangedEvent] {
	digest := sha256.Sum256([]byte(boardID + "\x00" + polled.item.ID + "\x00" + polled.hash))
	return sdkgo.TriggerEvent[ItemChangedEvent]{
		ID:         "monday-item-" + hex.EncodeToString(digest[:16]),
		OccurredAt: polled.item.UpdatedAt.UTC(),
		Payload:    ItemChangedEvent{BoardID: boardID, ChangeKind: kind, Item: polled.item},
	}
}

func itemChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("monday itemChanged configuration: %w", err)
}
