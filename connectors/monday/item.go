// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxColumnTextBytes bounds the display text kept for one column value; longer text is cut on a UTF-8 boundary.
	MaxColumnTextBytes = 4096
	// MaxColumnValueBytes bounds the raw JSON kept for one column value; a larger value is omitted.
	MaxColumnValueBytes = 16384
	// MaxRequestedColumns bounds the column IDs one read requests or one write sets.
	MaxRequestedColumns = 50

	// itemFields is the selection every item read returns; $columnIds limits column_values, null means all columns.
	itemFields = `id name state created_at updated_at url creator_id board { id } group { id title } column_values(ids: $columnIds) { id type text value }`
	// createdItemFields omits column values, keeping a create response far below monday.com's 1 MB replay-cache limit.
	createdItemFields = `id name state created_at updated_at url creator_id board { id } group { id title }`
)

var (
	numericIDPattern  = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	columnIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	groupIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	columnTypeToken   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	mondayTimeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05Z0700", "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05"}
)

// ItemState is monday.com's state of an item.
type ItemState string

const (
	// ItemStateActive is an item shown on its board.
	ItemStateActive ItemState = "active"
	// ItemStateArchived is an archived item.
	ItemStateArchived ItemState = "archived"
	// ItemStateDeleted is a deleted item that monday.com still keeps in its trash.
	ItemStateDeleted ItemState = "deleted"
)

// Item is the connector's view of one monday.com item. Column types, status labels, and
// other column text are the board's own values, never mapped to another vocabulary.
type Item struct {
	// ID is monday.com's numeric item ID, as a decimal string.
	ID string `json:"id"`
	// Name is the item name, the board's first column.
	Name string `json:"name"`
	// State is active, archived, or deleted; blank when monday.com omits it.
	State ItemState `json:"state,omitempty"`
	// BoardID is the numeric ID of the board that holds the item.
	BoardID string `json:"boardId,omitempty"`
	// Group is the board group that holds the item, or nil when monday.com omits it.
	Group *ItemGroup `json:"group,omitempty"`
	// CreatorID is the numeric ID of the user who created the item; blank when monday.com created it.
	CreatorID string `json:"creatorId,omitempty"`
	// URL is the item's page on the account's monday.com host.
	URL string `json:"url,omitempty"`
	// CreatedAt is when the item was created, or zero when monday.com's timestamp is absent or unreadable.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// UpdatedAt is when the item last changed, or zero when monday.com's timestamp is absent or unreadable.
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	// ColumnValues lists the requested columns in monday.com's board order; the name column is Name instead.
	ColumnValues []ItemColumnValue `json:"columnValues,omitempty"`
}

// ItemGroup identifies a board group, monday.com's container for items.
type ItemGroup struct {
	// ID is the group ID, such as topics or new_group29179.
	ID string `json:"id"`
	// Title is the group's display title.
	Title string `json:"title,omitempty"`
}

// ItemColumnValue is one column value read from an item.
type ItemColumnValue struct {
	// ID is the column ID, such as status or date4.
	ID string `json:"id"`
	// Type is monday.com's column type, such as status, date, people, or numbers.
	Type ColumnType `json:"type"`
	// Text is monday.com's display text, such as Working on it for a status; blank when the column is empty.
	Text string `json:"text,omitempty"`
	// Value is the column's raw JSON value, such as {"index":1}; nil when the column is empty or omitted.
	Value json.RawMessage `json:"value,omitempty"`
	// IsTextTruncated reports that Text stopped at MaxColumnTextBytes.
	IsTextTruncated bool `json:"textTruncated,omitempty"`
	// IsValueOmitted reports that the raw value exceeded MaxColumnValueBytes and was left out.
	IsValueOmitted bool `json:"valueOmitted,omitempty"`
}

// ColumnValueByID returns the item's value for one column, or false when the item has none.
func (item Item) ColumnValueByID(columnID string) (ItemColumnValue, bool) {
	for _, columnValue := range item.ColumnValues {
		if columnValue.ID == columnID {
			return columnValue, true
		}
	}
	return ItemColumnValue{}, false
}

type itemResource struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	State        string                `json:"state"`
	CreatedAt    string                `json:"created_at"`
	UpdatedAt    string                `json:"updated_at"`
	URL          string                `json:"url"`
	CreatorID    *string               `json:"creator_id"`
	Board        *boardReference       `json:"board"`
	Group        *groupResource        `json:"group"`
	ColumnValues []columnValueResource `json:"column_values"`
}

type boardReference struct {
	ID string `json:"id"`
}

type groupResource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type columnValueResource struct {
	ID    string          `json:"id"`
	Type  string          `json:"type"`
	Text  *string         `json:"text"`
	Value json.RawMessage `json:"value"`
}

// decodeItemResource converts one monday.com item and rejects identifiers the connector cannot trust.
func decodeItemResource(resource itemResource) (Item, error) {
	if !numericIDPattern.MatchString(resource.ID) {
		return Item{}, errors.New("item ID is not a monday.com numeric ID")
	}
	if !utf8.ValidString(resource.Name) {
		return Item{}, errors.New("item name is not valid UTF-8")
	}
	item := Item{ID: resource.ID, Name: resource.Name, URL: safeItemURL(resource.URL)}
	switch state := ItemState(resource.State); state {
	case "", ItemStateActive, ItemStateArchived, ItemStateDeleted:
		item.State = state
	default:
		return Item{}, errors.New("item state is not a monday.com state")
	}
	if resource.Board != nil {
		if !numericIDPattern.MatchString(resource.Board.ID) {
			return Item{}, errors.New("item board ID is not a monday.com numeric ID")
		}
		item.BoardID = resource.Board.ID
	}
	if resource.Group != nil {
		if !groupIDPattern.MatchString(resource.Group.ID) {
			return Item{}, errors.New("item group ID is not a monday.com group ID")
		}
		item.Group = &ItemGroup{ID: resource.Group.ID, Title: resource.Group.Title}
	}
	if resource.CreatorID != nil && *resource.CreatorID != "" {
		if !numericIDPattern.MatchString(*resource.CreatorID) {
			return Item{}, errors.New("item creator ID is not a monday.com numeric ID")
		}
		item.CreatorID = *resource.CreatorID
	}
	item.CreatedAt, item.UpdatedAt = parseMondayTime(resource.CreatedAt), parseMondayTime(resource.UpdatedAt)
	for _, columnValue := range resource.ColumnValues {
		decoded, err := decodeColumnValueResource(columnValue)
		if err != nil {
			return Item{}, err
		}
		item.ColumnValues = append(item.ColumnValues, decoded)
	}
	return item, nil
}

// decodeColumnValueResource bounds one column value; monday.com returns the JSON value as a JSON-encoded string.
func decodeColumnValueResource(resource columnValueResource) (ItemColumnValue, error) {
	if !columnIDPattern.MatchString(resource.ID) {
		return ItemColumnValue{}, errors.New("column ID is not a monday.com column ID")
	}
	if !columnTypeToken.MatchString(resource.Type) {
		return ItemColumnValue{}, fmt.Errorf("column %s type is not a monday.com column type", resource.ID)
	}
	value := ItemColumnValue{ID: resource.ID, Type: ColumnType(resource.Type)}
	if resource.Text != nil {
		value.Text, value.IsTextTruncated = truncateUTF8(*resource.Text, MaxColumnTextBytes)
	}
	raw := bytes.TrimSpace(resource.Value)
	if isJSONNull(raw) {
		return value, nil
	}
	if raw[0] == '"' {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return ItemColumnValue{}, fmt.Errorf("column %s value is not JSON", resource.ID)
		}
		raw = bytes.TrimSpace([]byte(encoded))
		if isJSONNull(raw) {
			return value, nil
		}
	}
	if !json.Valid(raw) {
		return ItemColumnValue{}, fmt.Errorf("column %s value is not JSON", resource.ID)
	}
	if len(raw) > MaxColumnValueBytes {
		value.IsValueOmitted = true
		return value, nil
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return ItemColumnValue{}, fmt.Errorf("column %s value is not JSON", resource.ID)
	}
	value.Value = compacted.Bytes()
	return value, nil
}

// parseMondayTime accepts the timestamp shapes monday.com's Date scalar is seen in; anything else is zero.
func parseMondayTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range mondayTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

// safeItemURL keeps only an absolute HTTPS URL without user information.
func safeItemURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return value
}

// truncateUTF8 cuts value to at most maximumBytes on a rune boundary and reports whether it cut.
func truncateUTF8(value string, maximumBytes int) (string, bool) {
	if !utf8.ValidString(value) {
		value = strings.ToValidUTF8(value, "�")
	}
	if len(value) <= maximumBytes {
		return value, false
	}
	cut := maximumBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}

// validateNumericID accepts a monday.com board, item, or user ID written as a decimal string.
func validateNumericID(value string, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !numericIDPattern.MatchString(trimmed) {
		return "", fmt.Errorf("%s must be a numeric monday.com ID such as 1234567890", fieldName)
	}
	return trimmed, nil
}

// validateRequestedColumnIDs checks a column selection; nil means every column.
func validateRequestedColumnIDs(columnIDs []string) ([]string, error) {
	if len(columnIDs) == 0 {
		return nil, nil
	}
	if len(columnIDs) > MaxRequestedColumns {
		return nil, fmt.Errorf("columnIds accepts at most %d column IDs", MaxRequestedColumns)
	}
	seen := map[string]bool{}
	validated := make([]string, 0, len(columnIDs))
	for _, columnID := range columnIDs {
		if !columnIDPattern.MatchString(columnID) {
			return nil, errors.New("columnIds must be monday.com column IDs such as status or date4")
		}
		if !seen[columnID] {
			seen[columnID] = true
			validated = append(validated, columnID)
		}
	}
	return validated, nil
}

// optionalStringList passes a column selection as a GraphQL variable; nil sends null, which selects every column.
func optionalStringList(values []string) any {
	if len(values) == 0 {
		return nil
	}
	return values
}
