// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package trelloboardfake serves one Trello board's action history and cards over HTTP for cardChanged
// tests: GET /boards/{board}/actions with filter, limit, since, and before, newest first, and GET
// /cards/{card}. It requires Trello's OAuth Authorization header with the test key and token.
package trelloboardfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// APIKey and Token are the only credentials the fake accepts.
	APIKey = "fake-trello-key"
	Token  = "fake-trello-token"
	// MemberID is the member every action is recorded for.
	MemberID = "5a1e4c7b2f0d3e6a9b8c7d6e"
	// TodoListID and DoneListID are the board's two lists.
	TodoListID = "5b6893f01cb3228998cf62a1"
	DoneListID = "5b6893f01cb3228998cf62a2"
)

// Board is one fake Trello board. It is safe for concurrent use.
type Board struct {
	// URL is the API base URL to configure as the connection endpoint.
	URL string
	// BoardID is the board's 24-character ID.
	BoardID string

	mutex             sync.Mutex
	actions           []action
	cards             map[string]*card
	nextSequence      uint64
	pendingRateLimits int
	rejectNextCursor  bool
	actionRequests    int
	rateLimited       int
}

type action struct {
	ID              string         `json:"id"`
	IDMemberCreator string         `json:"idMemberCreator"`
	Type            string         `json:"type"`
	Date            string         `json:"date"`
	Data            map[string]any `json:"data"`
	MemberCreator   map[string]any `json:"memberCreator"`
	occurredAt      time.Time
}

type card struct {
	ID        string  `json:"id"`
	ShortLink string  `json:"shortLink"`
	IDShort   int64   `json:"idShort"`
	Name      string  `json:"name"`
	Desc      string  `json:"desc"`
	IDBoard   string  `json:"idBoard"`
	IDList    string  `json:"idList"`
	Closed    bool    `json:"closed"`
	Pos       float64 `json:"pos"`
}

// New starts a fake board with a unique ID and stops it when the test ends.
func New(t testing.TB) *Board {
	t.Helper()
	// A time-based counter keeps action IDs, and so Flow IDs, unique across test runs on one Dex server.
	board := &Board{cards: map[string]*card{}, nextSequence: uint64(time.Now().UnixNano())}
	board.BoardID = board.newObjectID(time.Now())
	server := httptest.NewServer(http.HandlerFunc(board.serveHTTP))
	t.Cleanup(server.Close)
	board.URL = server.URL + "/1"
	return board
}

// CreateCard adds a card to the To do list and records createCard; it returns the card ID.
func (board *Board) CreateCard(name string) string {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	now := time.Now()
	created := &card{ID: board.newObjectID(now), Name: name, IDBoard: board.BoardID, IDList: TodoListID, Pos: 16384}
	created.IDShort, created.ShortLink = int64(len(board.cards)+1), fmt.Sprintf("Sh%06d", len(board.cards)+1)
	board.cards[created.ID] = created
	board.recordAction("createCard", now, created, map[string]any{"list": board.list(TodoListID)})
	return created.ID
}

// MoveCard moves a card to the Done list and records updateCard with listBefore and listAfter.
func (board *Board) MoveCard(cardID string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	moved := board.cards[cardID]
	before := moved.IDList
	moved.IDList = DoneListID
	board.recordAction("updateCard", time.Now(), moved, map[string]any{
		"listBefore": board.list(before), "listAfter": board.list(DoneListID), "old": map[string]any{"idList": before},
	})
}

// CommentOnCard records commentCard with text.
func (board *Board) CommentOnCard(cardID string, text string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	board.recordAction("commentCard", time.Now(), board.cards[cardID], map[string]any{"text": text})
}

// DeleteCard removes a card and records deleteCard, which keeps only the card's ID, short ID, and list.
func (board *Board) DeleteCard(cardID string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	deleted := board.cards[cardID]
	delete(board.cards, cardID)
	board.recordAction("deleteCard", time.Now(), &card{ID: deleted.ID, IDShort: deleted.IDShort, IDList: deleted.IDList}, nil)
}

// RenameCardRecordedEarlier renames a card with an updateCard action dated age ago that only now becomes
// visible, as an action recorded in the same moment as one already read can surface late.
func (board *Board) RenameCardRecordedEarlier(cardID string, name string, age time.Duration) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	renamed := board.cards[cardID]
	old := renamed.Name
	renamed.Name = name
	board.recordAction("updateCard", time.Now().Add(-age), renamed, map[string]any{"old": map[string]any{"name": old}})
}

// RecordUnwatchedAction records a board action that is not a card action, which cardChanged never reads.
func (board *Board) RecordUnwatchedAction() {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	now := time.Now()
	board.actions = append(board.actions, action{
		ID: board.newObjectID(now), IDMemberCreator: MemberID, Type: "updateBoard", Date: now.UTC().Format("2006-01-02T15:04:05.000Z"),
		Data: map[string]any{"board": map[string]any{"id": board.BoardID, "name": "Support queue"}}, occurredAt: now,
	})
}

// RateLimitNextActionRequests answers the next count action requests with 429 API_TOKEN_LIMIT_EXCEEDED.
func (board *Board) RateLimitNextActionRequests(count int) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	board.pendingRateLimits = count
}

// RejectNextCursor answers the next action request that sends since with 400 invalid value for since.
func (board *Board) RejectNextCursor() {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	board.rejectNextCursor = true
}

// RateLimitedRequests returns how many action requests were answered with 429.
func (board *Board) RateLimitedRequests() int {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	return board.rateLimited
}

func (board *Board) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	expected := `OAuth oauth_consumer_key="` + APIKey + `", oauth_token="` + Token + `"`
	if request.Header.Get("Authorization") != expected {
		http.Error(writer, "invalid key", http.StatusUnauthorized)
		return
	}
	segments := strings.Split(strings.TrimPrefix(request.URL.Path, "/1/"), "/")
	switch {
	case request.Method == http.MethodGet && len(segments) == 3 && segments[0] == "boards" && segments[2] == "actions":
		board.serveActions(writer, request, segments[1])
	case request.Method == http.MethodGet && len(segments) == 2 && segments[0] == "cards":
		board.serveCard(writer, segments[1])
	default:
		http.NotFound(writer, request)
	}
}

func (board *Board) serveActions(writer http.ResponseWriter, request *http.Request, boardID string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	board.actionRequests++
	query := request.URL.Query()
	switch {
	case boardID != board.BoardID:
		http.Error(writer, "The requested resource was not found.", http.StatusNotFound)
		return
	case board.pendingRateLimits > 0:
		board.pendingRateLimits--
		board.rateLimited++
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "API_TOKEN_LIMIT_EXCEEDED", "message": "Rate limit exceeded"})
		return
	case board.rejectNextCursor && query.Has("since"):
		board.rejectNextCursor = false
		http.Error(writer, "invalid value for since", http.StatusBadRequest)
		return
	}
	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil || limit < 0 || limit > 1000 {
		http.Error(writer, "invalid value for limit", http.StatusBadRequest)
		return
	}
	filters := strings.Split(query.Get("filter"), ",")
	since, hasSince := parseBound(query.Get("since"))
	before, hasBefore := query.Get("before"), query.Has("before")
	listed := make([]action, 0)
	for _, candidate := range board.actions {
		switch {
		case !slices.Contains(filters, candidate.Type):
		case hasSince && !candidate.occurredAt.After(since):
		case hasBefore && candidate.ID >= before:
		default:
			listed = append(listed, candidate)
		}
	}
	slices.SortFunc(listed, func(left, right action) int {
		if order := right.occurredAt.Compare(left.occurredAt); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
	writeJSON(writer, http.StatusOK, listed[:min(limit, len(listed))])
}

func (board *Board) serveCard(writer http.ResponseWriter, cardID string) {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	found, ok := board.cards[cardID]
	if !ok {
		http.Error(writer, "The requested resource was not found.", http.StatusNotFound)
		return
	}
	writeJSON(writer, http.StatusOK, found)
}

func (board *Board) recordAction(actionType string, occurredAt time.Time, subject *card, extra map[string]any) {
	data := map[string]any{
		"board": map[string]any{"id": board.BoardID, "name": "Support queue", "shortLink": "Supp0rtQ"},
		"card":  map[string]any{"id": subject.ID, "name": subject.Name, "idShort": subject.IDShort, "shortLink": subject.ShortLink, "idList": subject.IDList},
	}
	for key, value := range extra {
		data[key] = value
	}
	board.actions = append(board.actions, action{
		ID: board.newObjectID(occurredAt), IDMemberCreator: MemberID, Type: actionType,
		Date: occurredAt.UTC().Format("2006-01-02T15:04:05.000Z"), Data: data, occurredAt: occurredAt.UTC().Truncate(time.Millisecond),
		MemberCreator: map[string]any{"id": MemberID, "fullName": "Grace Hopper", "username": "gracehopper"},
	})
}

func (board *Board) list(listID string) map[string]any {
	names := map[string]string{TodoListID: "To do", DoneListID: "Done"}
	return map[string]any{"id": listID, "name": names[listID]}
}

// newObjectID mimics Trello's Mongo IDs: four bytes of creation seconds, then a counter.
func (board *Board) newObjectID(createdAt time.Time) string {
	board.nextSequence++
	return fmt.Sprintf("%08x%016x", uint32(createdAt.Unix()), board.nextSequence)
}

func parseBound(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	// The test server owns the connection; a failed write surfaces as a client error in the test.
	_ = json.NewEncoder(writer).Encode(value)
}

// ActionRequests returns how many action requests the fake received.
func (board *Board) ActionRequests() int {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	return board.actionRequests
}

// LastActionID returns the ID of the newest recorded action, or "" before the first.
func (board *Board) LastActionID() string {
	board.mutex.Lock()
	defer board.mutex.Unlock()
	if len(board.actions) == 0 {
		return ""
	}
	return board.actions[len(board.actions)-1].ID
}
