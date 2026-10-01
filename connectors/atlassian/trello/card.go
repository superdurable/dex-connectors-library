// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maximumTextCharacters is Trello's documented description limit; comments and names use the same bound.
	maximumTextCharacters = 16384
	maximumCardIDs        = 50
	maximumListedLabels   = 50
	maximumListedMembers  = 50
	positionTop           = "top"
	positionBottom        = "bottom"
	// trelloTimeLayout writes a UTC instant with the millisecond precision Trello stores, such as
	// 2026-10-15T17:00:00.000Z.
	trelloTimeLayout = "2006-01-02T15:04:05.000Z07:00"
)

var (
	// trelloIDPattern accepts Trello's documented object ID, 24 hexadecimal characters.
	trelloIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
	// shortLinkPattern accepts the 8-character short link Trello documents for boards and cards.
	shortLinkPattern = regexp.MustCompile(`^[0-9A-Za-z]{8}$`)

	// cardSummaryFields are the card fields listCards requests; the description stays out of list pages.
	cardSummaryFields = []string{
		"id", "shortLink", "name", "idBoard", "idList", "closed", "due", "dueComplete", "start",
		"idLabels", "labels", "idMembers", "pos", "url", "shortUrl", "dateLastActivity",
	}
	// cardDetailFields add the plain-text description for getCard and the updateCard read-back.
	cardDetailFields = append(append([]string(nil), cardSummaryFields...), "desc")

	errMismatchedCard = errors.New("response describes another card")
)

// Card is the connector's view of one Trello card. Trello has no status field: the card's list is its
// workflow state, so moving a card between lists is its status change.
type Card struct {
	// ID is the card's Trello ID, 24 hexadecimal characters.
	ID string `json:"id"`
	// ShortLink is the 8-character code in the card's web address, such as LrrmgFyd.
	ShortLink string `json:"shortLink,omitempty"`
	// Name is the card name.
	Name string `json:"name"`
	// Description is the plain-text description, returned by getCard and updateCard only, at most 16384
	// characters.
	Description string `json:"description,omitempty"`
	// IsDescriptionTruncated reports that Description stopped at 16384 characters.
	IsDescriptionTruncated bool `json:"isDescriptionTruncated,omitempty"`
	// BoardID is the ID of the board that holds the card.
	BoardID string `json:"boardId"`
	// ListID is the ID of the list that holds the card; the list is the card's workflow state.
	ListID string `json:"listId"`
	// ListName is the name of the card's list, returned by getCard only.
	ListName string `json:"listName,omitempty"`
	// IsClosed reports an archived card.
	IsClosed bool `json:"isClosed"`
	// Due is when the card is due, or nil without a due date.
	Due *time.Time `json:"due,omitempty"`
	// IsDueComplete reports that the card's due date is marked complete.
	IsDueComplete bool `json:"isDueComplete"`
	// Start is the card's start date, or nil.
	Start *time.Time `json:"start,omitempty"`
	// LabelIDs lists up to 50 IDs of the labels on the card.
	LabelIDs []string `json:"labelIds,omitempty"`
	// Labels lists up to 50 labels on the card with their names and colors.
	Labels []Label `json:"labels,omitempty"`
	// HasMoreLabels reports that the card carries more than 50 labels, so LabelIDs and Labels are incomplete.
	HasMoreLabels bool `json:"hasMoreLabels,omitempty"`
	// MemberIDs lists up to 50 IDs of the members on the card.
	MemberIDs []string `json:"memberIds,omitempty"`
	// Members lists up to 50 members on the card with their names, returned by getCard only. Email
	// addresses are never read.
	Members []MemberReference `json:"members,omitempty"`
	// Position is the card's position in its list; a smaller value is nearer the top.
	Position float64 `json:"position"`
	// URL is the card's full Trello web URL.
	URL string `json:"url,omitempty"`
	// ShortURL is the card's short Trello web URL.
	ShortURL string `json:"shortUrl,omitempty"`
	// LastActivityAt is when the card last changed.
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
}

// Label is one board label on a card.
type Label struct {
	// ID is the label's Trello ID.
	ID string `json:"id"`
	// Name is the label name, which may be empty for a color-only label.
	Name string `json:"name,omitempty"`
	// Color is the label color, such as green or sky, or empty for a colorless label.
	Color string `json:"color,omitempty"`
}

// MemberReference identifies a Trello member by ID, full name, and username.
type MemberReference struct {
	// ID is the member's Trello ID.
	ID string `json:"id"`
	// FullName is the member's display name.
	FullName string `json:"fullName,omitempty"`
	// Username is the member's Trello username.
	Username string `json:"username,omitempty"`
}

// HasLabel reports whether the card carries the label ID.
func (card Card) HasLabel(labelID string) bool {
	for _, cardLabelID := range card.LabelIDs {
		if strings.EqualFold(cardLabelID, labelID) {
			return true
		}
	}
	return false
}

type cardResource struct {
	ID               string            `json:"id"`
	ShortLink        string            `json:"shortLink"`
	Name             string            `json:"name"`
	Desc             string            `json:"desc"`
	IDBoard          string            `json:"idBoard"`
	IDList           string            `json:"idList"`
	Closed           bool              `json:"closed"`
	Due              *string           `json:"due"`
	DueComplete      bool              `json:"dueComplete"`
	Start            *string           `json:"start"`
	IDLabels         []json.RawMessage `json:"idLabels"`
	Labels           []json.RawMessage `json:"labels"`
	IDMembers        []string          `json:"idMembers"`
	Members          []memberResource  `json:"members"`
	List             *listResource     `json:"list"`
	Pos              *float64          `json:"pos"`
	URL              string            `json:"url"`
	ShortURL         string            `json:"shortUrl"`
	DateLastActivity *string           `json:"dateLastActivity"`
}

type memberResource struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Username string `json:"username"`
}

type listResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type labelResource struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

// decodeCard converts one Trello card. includesDetail keeps the description, members, and list name.
func decodeCard(resource cardResource, includesDetail bool) (Card, error) {
	switch {
	case !trelloIDPattern.MatchString(resource.ID):
		return Card{}, errors.New("card ID is invalid")
	case !trelloIDPattern.MatchString(resource.IDBoard):
		return Card{}, errors.New("card board ID is invalid")
	case !trelloIDPattern.MatchString(resource.IDList):
		return Card{}, errors.New("card list ID is invalid")
	}
	card := Card{
		ID: resource.ID, Name: resource.Name, BoardID: resource.IDBoard, ListID: resource.IDList,
		IsClosed: resource.Closed, IsDueComplete: resource.DueComplete, URL: resource.URL, ShortURL: resource.ShortURL,
	}
	if shortLinkPattern.MatchString(resource.ShortLink) {
		card.ShortLink = resource.ShortLink
	}
	if resource.Pos != nil {
		card.Position = *resource.Pos
	}
	var err error
	if card.Due, err = parseOptionalTrelloTime(resource.Due); err != nil {
		return Card{}, fmt.Errorf("card due date: %w", err)
	}
	if card.Start, err = parseOptionalTrelloTime(resource.Start); err != nil {
		return Card{}, fmt.Errorf("card start date: %w", err)
	}
	if card.LastActivityAt, err = parseOptionalTrelloTime(resource.DateLastActivity); err != nil {
		return Card{}, fmt.Errorf("card last activity: %w", err)
	}
	card.LabelIDs, card.Labels, card.HasMoreLabels = decodeCardLabels(resource.IDLabels, resource.Labels)
	for _, memberID := range resource.IDMembers {
		if trelloIDPattern.MatchString(memberID) && len(card.MemberIDs) < maximumListedMembers {
			card.MemberIDs = append(card.MemberIDs, memberID)
		}
	}
	if !includesDetail {
		return card, nil
	}
	card.Description, card.IsDescriptionTruncated = truncateCharacters(resource.Desc, maximumTextCharacters)
	if resource.List != nil && resource.List.ID == resource.IDList {
		card.ListName = resource.List.Name
	}
	for _, member := range resource.Members {
		if trelloIDPattern.MatchString(member.ID) && len(card.Members) < maximumListedMembers {
			card.Members = append(card.Members, MemberReference{ID: member.ID, FullName: member.FullName, Username: member.Username})
		}
	}
	return card, nil
}

// decodeCardLabels accepts ID strings or label objects in both fields, which Trello's OpenAPI types inconsistently.
func decodeCardLabels(idLabels []json.RawMessage, labels []json.RawMessage) (labelIDs []string, labelViews []Label, hasMore bool) {
	isSeen := map[string]bool{}
	for _, entry := range append(append([]json.RawMessage(nil), idLabels...), labels...) {
		var view Label
		var labelID string
		if json.Unmarshal(entry, &labelID) != nil {
			var resource labelResource
			if json.Unmarshal(entry, &resource) != nil {
				continue
			}
			labelID, view = resource.ID, Label{ID: resource.ID, Name: resource.Name}
			if resource.Color != nil {
				view.Color = *resource.Color
			}
		}
		if !trelloIDPattern.MatchString(labelID) {
			continue
		}
		key := strings.ToLower(labelID)
		if view.ID != "" && len(labelViews) < maximumListedLabels && !hasLabelView(labelViews, labelID) {
			labelViews = append(labelViews, view)
		}
		if isSeen[key] {
			continue
		}
		isSeen[key] = true
		if len(labelIDs) == maximumListedLabels {
			hasMore = true
			continue
		}
		labelIDs = append(labelIDs, labelID)
	}
	return labelIDs, labelViews, hasMore
}

func hasLabelView(labels []Label, labelID string) bool {
	for _, label := range labels {
		if strings.EqualFold(label.ID, labelID) {
			return true
		}
	}
	return false
}

// parseTrelloTime accepts Trello's ISO 8601 timestamp, such as 2026-10-15T17:00:00.000Z.
func parseTrelloTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("timestamp is not an ISO 8601 date-time")
	}
	return parsed.UTC(), nil
}

func parseOptionalTrelloTime(value *string) (*time.Time, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	parsed, err := parseTrelloTime(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// formatTrelloTime writes an instant as UTC with millisecond precision.
func formatTrelloTime(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format(trelloTimeLayout)
}

// isSameTrelloInstant compares instants at the millisecond precision Trello stores.
func isSameTrelloInstant(first time.Time, second time.Time) bool {
	return first.UTC().Truncate(time.Millisecond).Equal(second.UTC().Truncate(time.Millisecond))
}

// truncateCharacters keeps at most maximumCharacters runes.
func truncateCharacters(value string, maximumCharacters int) (string, bool) {
	if utf8.RuneCountInString(value) <= maximumCharacters {
		return value, false
	}
	runes := []rune(value)
	return string(runes[:maximumCharacters]), true
}

// validateTrelloID checks one Trello ID used in a path or body.
func validateTrelloID(value string, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !trelloIDPattern.MatchString(trimmed) {
		return "", fmt.Errorf("%s must be a Trello ID, 24 hexadecimal characters such as 5b6893f01cb3228998cf629e", fieldName)
	}
	return trimmed, nil
}

// validateOptionalTrelloID checks an ID that may be blank.
func validateOptionalTrelloID(value string, fieldName string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return validateTrelloID(value, fieldName)
}

// validateTrelloIDs checks up to 50 distinct IDs, such as label or member IDs.
func validateTrelloIDs(values []string, fieldName string) ([]string, error) {
	if len(values) > maximumCardIDs {
		return nil, fmt.Errorf("%s accepts at most %d IDs", fieldName, maximumCardIDs)
	}
	validated := make([]string, 0, len(values))
	isSeen := map[string]bool{}
	for _, value := range values {
		trelloID, err := validateTrelloID(value, fieldName)
		if err != nil {
			return nil, err
		}
		if isSeen[strings.ToLower(trelloID)] {
			return nil, fmt.Errorf("%s lists %s twice", fieldName, trelloID)
		}
		isSeen[strings.ToLower(trelloID)] = true
		validated = append(validated, trelloID)
	}
	return validated, nil
}

// validateCardName checks a required one-line card name.
func validateCardName(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return "", errors.New("name is required")
	case !utf8.ValidString(trimmed):
		return "", errors.New("name must be valid UTF-8")
	case utf8.RuneCountInString(trimmed) > maximumTextCharacters:
		return "", fmt.Errorf("name cannot exceed %d characters", maximumTextCharacters)
	}
	for _, character := range trimmed {
		if character < ' ' || character == 0x7f {
			return "", errors.New("name must be one line without control characters")
		}
	}
	return trimmed, nil
}

// validatePlainText checks a description or a comment; line breaks and tabs are kept, other control
// characters are rejected.
func validatePlainText(value string, fieldName string, isRequired bool) error {
	switch {
	case isRequired && strings.TrimSpace(value) == "":
		return errors.New(fieldName + " is required")
	case !utf8.ValidString(value):
		return errors.New(fieldName + " must be valid UTF-8")
	case utf8.RuneCountInString(value) > maximumTextCharacters:
		return fmt.Errorf("%s cannot exceed %d characters", fieldName, maximumTextCharacters)
	}
	for _, character := range value {
		if (character < ' ' && character != '\n' && character != '\t' && character != '\r') || character == 0x7f {
			return errors.New(fieldName + " cannot contain control characters")
		}
	}
	return nil
}

// cardPositionWireValue converts top, bottom, or a positive number to the value Trello's pos accepts.
func cardPositionWireValue(value string) (any, error) {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "":
		return nil, nil
	case positionTop, positionBottom:
		return trimmed, nil
	}
	position, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(position) || math.IsInf(position, 0) || position <= 0 {
		return nil, errors.New("position must be top, bottom, or a positive number such as 16384")
	}
	return position, nil
}

// decodeJSONArray unmarshals a top-level JSON array, which Trello returns for card lists.
func decodeJSONArray[T any](body []byte) ([]T, error) {
	var values []T
	if err := json.Unmarshal(body, &values); err != nil || values == nil {
		return nil, errors.New("response is not a JSON array")
	}
	return values, nil
}

// decodeJSONObject unmarshals a JSON object response.
func decodeJSONObject[T any](body []byte) (T, error) {
	var value *T
	if err := json.Unmarshal(body, &value); err != nil || value == nil {
		var zero T
		return zero, errors.New("response is not a JSON object")
	}
	return *value, nil
}
