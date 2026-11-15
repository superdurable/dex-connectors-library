// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	conversationReceivedOperation = "conversationReceived"

	// minimumPollPagesPerPoll leaves room for the head page plus one sweep page.
	minimumPollPagesPerPoll = 2
	// maximumPollPagesPerPoll keeps one poll within pollingtrigger.MaxChangesPerPage events.
	maximumPollPagesPerPoll = pollingtrigger.MaxChangesPerPage / MaximumPageSize

	// conversationReceivedCursorVersion versions the stored cursor token.
	conversationReceivedCursorVersion = 1

	// MaxTrackedConversations bounds the conversation IDs one conversationReceived binding remembers. An
	// inbox whose list holds more pauses its binding with a "cursor state exceeds bound" ERROR record.
	MaxTrackedConversations = 50_000

	// seedPagesPerFetch is how many pages one baseline step reads; it delivers nothing.
	seedPagesPerFetch = 10

	// conversationReceivedMaxBackoff caps the wait after failed polls and between bound-fault retries.
	conversationReceivedMaxBackoff = 30 * time.Minute
)

// ConversationReceivedTriggerConfiguration selects the shared inbox a conversationReceived binding polls.
// Dex Web saves it from the inboxPicker unit.
type ConversationReceivedTriggerConfiguration struct {
	// InboxID is the Hiver shared inbox ID, such as 105902, stored by the inboxPicker unit. Required.
	InboxID string `json:"inboxId"`
}

// ConversationReceivedEvent is one conversation that newly appeared in the polled shared inbox.
type ConversationReceivedEvent struct {
	// InboxID is the polled shared inbox ID.
	InboxID string `json:"inboxId"`
	// Conversation is the conversation as the inbox's conversation list reported it when the poll found
	// it: its status, assignee, tag IDs, Gmail thread ID, and private permalink. Hiver's list carries no
	// subject, requester, message, or timestamp; read the conversation with getConversation for its
	// message IDs.
	Conversation Conversation `json:"conversation"`
}

// conversationReceivedCursorToken is the JSON position stored in the poll cursor. The keyed snapshot
// beside it holds every conversation ID the binding has listed.
type conversationReceivedCursorToken struct {
	Version int    `json:"v"`
	InboxID string `json:"inboxId"`
	// IsBaselineComplete is false until the first full walk of the inbox list has recorded every
	// existing conversation without delivering it.
	IsBaselineComplete bool `json:"baselineComplete"`
	// SweepPageToken is Hiver's next_page token where the current sweep continues; empty starts a sweep.
	SweepPageToken string `json:"sweepPageToken,omitempty"`
	// SweepStartSeen is the lowest snapshot Seen counter a conversation refreshed by the current sweep
	// can have; entries below it at sweep end were not listed during the whole sweep.
	SweepStartSeen int64 `json:"sweepStartSeen,omitempty"`
	// PreviousSweepStartSeen is SweepStartSeen of the last sweep that completed. Hiver's next_page tokens
	// may be offsets, so a conversation removed from the list while a sweep runs can shift a listed one
	// past the sweep's page boundary. A tracked ID is dropped only when it is below this counter at the
	// end of the next complete sweep, that is, when two whole sweeps in a row never listed it.
	PreviousSweepStartSeen int64 `json:"previousSweepStartSeen,omitempty"`
}

// Validate checks that the binding names one Hiver shared inbox ID.
func (configuration ConversationReceivedTriggerConfiguration) Validate() error {
	if err := validateHiverID("inboxId", strings.TrimSpace(configuration.InboxID)); err != nil {
		return fmt.Errorf("Hiver conversationReceived %w", err)
	}
	return nil
}

// conversationReceivedTriggerSource serves the generated NewConversationReceivedTrigger factory. Its
// cursor lives in process memory, so a restart records a new baseline; project applications use
// NewProjectConversationReceivedTriggerRunner instead.
func (client *Client) conversationReceivedTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration ConversationReceivedTriggerConfiguration,
) sdkgo.TriggerSource[ConversationReceivedEvent] {
	if err := configuration.Validate(); err != nil {
		return failedConversationReceivedTriggerSource{err: err}
	}
	// The generated factory does not pass its binding name, so log records name the Trigger instead.
	trigger := ConversationReceivedTriggerDefinition.Trigger
	binding := sdkgo.TriggerBindingRef{Connection: connection, Trigger: trigger, Name: trigger.TriggerName}
	source, err := pollingtrigger.NewBindingTriggerSource(pollingtrigger.BindingPollerConfig[ConversationReceivedEvent]{
		Definition: ConversationReceivedTriggerDefinition, Binding: binding, Cursor: pollingtrigger.NewMemoryCursorStore(),
		Fetcher: client.newConversationReceivedFetcher(binding, configuration), Snapshot: conversationSnapshotOptions(),
	}, client.conversationReceivedPollSchedule())
	if err != nil {
		return failedConversationReceivedTriggerSource{err: err}
	}
	return source
}

// failedConversationReceivedTriggerSource reports an invalid binding when its Trigger runs.
type failedConversationReceivedTriggerSource struct{ err error }

// Run returns the configuration error without polling.
func (source failedConversationReceivedTriggerSource) Run(context.Context, sdkgo.TriggerTarget[ConversationReceivedEvent]) error {
	return source.err
}

// conversationReceivedPollSchedule polls at the configured interval; a baseline walk reads at most
// seedPagesPerFetch pages per fetch and continues at once.
func (client *Client) conversationReceivedPollSchedule() pollingtrigger.PollSchedule {
	return pollingtrigger.PollSchedule{
		Interval: client.pollInterval, MaxBackoff: max(conversationReceivedMaxBackoff, client.pollInterval),
	}
}

// conversationSnapshotOptions makes the kit refuse, as a bound fault, a snapshot over MaxTrackedConversations.
func conversationSnapshotOptions() *pollingtrigger.KeyedSnapshotOptions {
	return &pollingtrigger.KeyedSnapshotOptions{MaxEntries: MaxTrackedConversations}
}

// conversationReceivedFetcher reads one shared inbox's conversation list. Hiver's list documents no
// timestamp, filter, or order, so every poll reads the first page and continues a sweep of the whole list.
type conversationReceivedFetcher struct {
	client  *Client
	binding sdkgo.TriggerBindingRef
	call    sdkgo.Call
	inboxID string
}

// conversationPage is one decoded page of the inbox's conversation list.
type conversationPage struct {
	conversations []Conversation
	nextPageToken string
}

// conversationSnapshotItem tracks one conversation ID; its hash ignores status, assignee, and tags.
type conversationSnapshotItem struct{ conversationID string }

func (client *Client) newConversationReceivedFetcher(
	binding sdkgo.TriggerBindingRef,
	configuration ConversationReceivedTriggerConfiguration,
) *conversationReceivedFetcher {
	return &conversationReceivedFetcher{
		client: client, binding: binding, call: pollingtrigger.NewTriggerCredentialCall(binding.Connection, binding.Trigger),
		inboxID: strings.TrimSpace(configuration.InboxID),
	}
}

// SeedCursor starts the baseline walk without a request; FetchChanges records existing conversations.
func (fetcher *conversationReceivedFetcher) SeedCursor(context.Context) (pollingtrigger.Cursor, error) {
	return fetcher.encodeCursor(conversationReceivedCursorToken{Version: conversationReceivedCursorVersion, InboxID: fetcher.inboxID}, &projectconfig.KeyedSnapshot{})
}

// FetchChanges continues the baseline walk until it reaches the end of the list, and afterwards reads the
// first page plus up to pollPagesPerPoll-1 sweep pages, delivering each conversation ID not yet tracked.
func (fetcher *conversationReceivedFetcher) FetchChanges(
	ctx context.Context,
	cursor pollingtrigger.Cursor,
) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationReceivedEvent]], error) {
	token, err := fetcher.decodeCursorToken(cursor.Token)
	if err != nil {
		return pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationReceivedEvent]]{}, pollingtrigger.MarkCursorInvalid(err)
	}
	if !token.IsBaselineComplete {
		return fetcher.continueBaseline(ctx, token, cursor.Snapshot)
	}
	return fetcher.pollForNewConversations(ctx, token, cursor.Snapshot)
}

// continueBaseline records up to seedPagesPerFetch pages of existing conversations without events.
func (fetcher *conversationReceivedFetcher) continueBaseline(
	ctx context.Context,
	token conversationReceivedCursorToken,
	snapshot *projectconfig.KeyedSnapshot,
) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationReceivedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationReceivedEvent]]
	var listed []Conversation
	pageToken := token.SweepPageToken
	for pages := 0; pages < seedPagesPerFetch; pages++ {
		read, err := fetcher.readPage(ctx, pageToken)
		if pageToken != "" && isRejectedPageToken(err) {
			fetcher.logSweepRestart(ctx, "baseline")
			pageToken = ""
			break
		}
		if err != nil {
			return page, err
		}
		listed = append(listed, read.conversations...)
		pageToken = read.nextPageToken
		if pageToken == "" {
			token.IsBaselineComplete = true
			break
		}
	}
	next, err := fetcher.mergeSnapshot(snapshot, listed, nil)
	if err != nil {
		return page, err
	}
	token.SweepPageToken = pageToken
	page.HasMorePages = !token.IsBaselineComplete
	page.Next, err = fetcher.encodeCursor(token, next)
	return page, err
}

// pollForNewConversations reads the first page and continues the sweep. At the end of a sweep it drops
// tracked IDs that neither it nor the previous complete sweep listed, such as deleted conversations.
func (fetcher *conversationReceivedFetcher) pollForNewConversations(
	ctx context.Context,
	token conversationReceivedCursorToken,
	snapshot *projectconfig.KeyedSnapshot,
) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationReceivedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationReceivedEvent]]
	isSweepStart := token.SweepPageToken == ""
	if isSweepStart {
		token.SweepStartSeen = highestSeen(snapshot) + 1
	}
	head, err := fetcher.readPage(ctx, "")
	if err != nil {
		return page, err
	}
	listed := head.conversations
	sweepPageToken := token.SweepPageToken
	if isSweepStart {
		sweepPageToken = head.nextPageToken
	}
	isSweepComplete, isSweepRestarted := sweepPageToken == "", false
	for pages := 1; !isSweepComplete && pages < fetcher.client.pollPagesPerPoll; pages++ {
		read, err := fetcher.readPage(ctx, sweepPageToken)
		if isRejectedPageToken(err) {
			fetcher.logSweepRestart(ctx, "sweep")
			sweepPageToken, isSweepRestarted = "", true
			break
		}
		if err != nil {
			return page, err
		}
		listed = append(listed, read.conversations...)
		sweepPageToken = read.nextPageToken
		isSweepComplete = sweepPageToken == ""
	}
	page.Changes = fetcher.newConversationEvents(snapshot, listed)
	var removedKeys []string
	if isSweepComplete && !isSweepRestarted {
		removedKeys = unlistedSinceSweepStart(snapshot, token.PreviousSweepStartSeen, listed)
		token.PreviousSweepStartSeen = token.SweepStartSeen
	}
	next, err := fetcher.mergeSnapshot(snapshot, listed, removedKeys)
	if err != nil {
		return page, err
	}
	token.SweepPageToken = sweepPageToken
	page.Next, err = fetcher.encodeCursor(token, next)
	return page, err
}

// newConversationEvents returns one event per listed conversation ID the snapshot lacks, in list order.
func (fetcher *conversationReceivedFetcher) newConversationEvents(
	snapshot *projectconfig.KeyedSnapshot,
	listed []Conversation,
) []sdkgo.TriggerEvent[ConversationReceivedEvent] {
	tracked := trackedConversationIDs(snapshot)
	occurredAt := fetcher.client.now().UTC()
	var events []sdkgo.TriggerEvent[ConversationReceivedEvent]
	for _, conversation := range listed {
		if _, isTracked := tracked[conversation.ID]; isTracked {
			continue
		}
		tracked[conversation.ID] = struct{}{}
		events = append(events, sdkgo.TriggerEvent[ConversationReceivedEvent]{
			ID: fetcher.eventID(conversation.ID), OccurredAt: occurredAt,
			Payload: ConversationReceivedEvent{InboxID: fetcher.inboxID, Conversation: conversation},
		})
	}
	return events
}

// pollRequestError is a definite Hiver rejection of one poll request, kept with its HTTP status.
type pollRequestError struct {
	statusCode int
	message    string
}

// Error describes the failure without Hiver's response text.
func (err *pollRequestError) Error() string { return "Hiver conversationReceived poll: " + err.message }

// readPage waits for the runner's limiter and the connection's request slot, then reads one page of 100.
func (fetcher *conversationReceivedFetcher) readPage(ctx context.Context, pageToken string) (conversationPage, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return conversationPage{}, err
	}
	credentials, failure := fetcher.client.resolveCredentials(fetcher.call, conversationReceivedOperation)
	if failure != nil {
		return conversationPage{}, errors.New("Hiver conversationReceived poll: " + failure.Message)
	}
	if result, isAvailable := fetcher.client.awaitRequestSlotWithContext(ctx, conversationReceivedOperation); !isAvailable {
		if err := ctx.Err(); err != nil {
			return conversationPage{}, err
		}
		return conversationPage{}, errors.New("Hiver conversationReceived poll: " + result.failure.Message)
	}
	result := fetcher.client.sendWithContext(ctx, credentials, conversationReceivedOperation, hiverRequest{
		method: http.MethodGet, path: inboxPath(fetcher.inboxID) + "/conversations", query: pageQuery(pageToken, MaximumPageSize),
	}, false)
	switch result.outcome {
	case exchangeSucceeded:
		return fetcher.decodePage(result.response.body)
	case exchangeRateLimited:
		// Without Retry-After the runner backs off exponentially; the client pacer also holds its requests.
		return conversationPage{}, pollingtrigger.MarkRateLimited(errors.New("Hiver conversationReceived poll: "+result.failure.Message), result.retryAfter)
	case exchangeNotFound, exchangeRejected:
		return conversationPage{}, &pollRequestError{statusCode: result.response.statusCode, message: result.failure.Message}
	default:
		return conversationPage{}, errors.New("Hiver conversationReceived poll: " + result.failure.Message)
	}
}

// decodePage validates one list page; a page with more conversations than requested is invalid.
func (fetcher *conversationReceivedFetcher) decodePage(body []byte) (conversationPage, error) {
	results, nextPageToken, err := decodeListPage(body)
	if err != nil {
		return conversationPage{}, fmt.Errorf("Hiver conversationReceived poll: invalid conversation page: %w", err)
	}
	if len(results) > MaximumPageSize {
		return conversationPage{}, errors.New("Hiver conversationReceived poll: the page lists more conversations than requested")
	}
	page := conversationPage{conversations: make([]Conversation, 0, len(results)), nextPageToken: nextPageToken}
	for index, raw := range results {
		details, err := decodeConversation(raw, fetcher.inboxID)
		if err != nil {
			return conversationPage{}, fmt.Errorf("Hiver conversationReceived poll: invalid conversation at index %d: %w", index, err)
		}
		page.conversations = append(page.conversations, details.Conversation)
	}
	return page, nil
}

// mergeSnapshot refreshes every listed ID as seen by this commit and drops removedKeys. The kit, not this
// call, enforces MaxTrackedConversations, so an oversized inbox pauses as a bound fault.
func (fetcher *conversationReceivedFetcher) mergeSnapshot(
	snapshot *projectconfig.KeyedSnapshot,
	listed []Conversation,
	removedKeys []string,
) (*projectconfig.KeyedSnapshot, error) {
	items := make([]pollingtrigger.KeyedItem, 0, len(listed))
	isListed := make(map[string]struct{}, len(listed))
	for _, conversation := range listed {
		if _, isDuplicate := isListed[conversation.ID]; isDuplicate {
			continue
		}
		isListed[conversation.ID] = struct{}{}
		items = append(items, conversationSnapshotItem{conversationID: conversation.ID})
	}
	return pollingtrigger.ApplyKeyedSnapshotChanges(snapshot, items, removedKeys,
		pollingtrigger.KeyedSnapshotOptions{MaxEntries: projectconfig.MaxKeyedSnapshotEntries})
}

// eventID is a pure function of the inbox and conversation, so every redelivery starts one Flow.
func (fetcher *conversationReceivedFetcher) eventID(conversationID string) string {
	return "conversation-" + fetcher.inboxID + "-" + conversationID
}

func (fetcher *conversationReceivedFetcher) encodeCursor(token conversationReceivedCursorToken, snapshot *projectconfig.KeyedSnapshot) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(token)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Hiver conversationReceived cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded), Snapshot: snapshot}, nil
}

// decodeCursorToken rejects a token of another version or another inbox, so a re-pointed binding rebaselines.
func (fetcher *conversationReceivedFetcher) decodeCursorToken(encoded string) (conversationReceivedCursorToken, error) {
	var token conversationReceivedCursorToken
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&token); err != nil || token.Version != conversationReceivedCursorVersion {
		return token, errors.New("Hiver conversationReceived cursor is not a version 1 cursor")
	}
	if token.InboxID != fetcher.inboxID {
		return token, errors.New("Hiver conversationReceived cursor belongs to another shared inbox")
	}
	if (token.SweepPageToken != "" && !isPageToken(token.SweepPageToken)) || token.SweepStartSeen < 0 ||
		token.PreviousSweepStartSeen < 0 || token.PreviousSweepStartSeen > token.SweepStartSeen {
		return token, errors.New("Hiver conversationReceived cursor holds an invalid sweep position")
	}
	return token, nil
}

func (fetcher *conversationReceivedFetcher) logSweepRestart(ctx context.Context, walk string) {
	logger := fetcher.client.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelWarn, "hiver page token rejected; restarting walk from the first page",
		slog.String("connector", ConnectorID), slog.String("connection", fetcher.binding.Connection.Name),
		slog.String("trigger", fetcher.binding.Trigger.TriggerName), slog.String("binding", fetcher.binding.Name),
		slog.String("walk", walk))
}

// SnapshotKey is the Hiver conversation ID.
func (item conversationSnapshotItem) SnapshotKey() string { return item.conversationID }

// SnapshotHash is the first 8 bytes of SHA-256 over the ID; only membership matters.
func (item conversationSnapshotItem) SnapshotHash() string {
	digest := sha256.Sum256([]byte(item.conversationID))
	return hex.EncodeToString(digest[:8])
}

// SnapshotValue stores nothing beside the key.
func (conversationSnapshotItem) SnapshotValue() json.RawMessage { return nil }

// isRejectedPageToken reports a status Hiver may use for an expired or unknown next_page token.
func isRejectedPageToken(err error) bool {
	var rejected *pollRequestError
	if !errors.As(err, &rejected) {
		return false
	}
	switch rejected.statusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusGone, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

func trackedConversationIDs(snapshot *projectconfig.KeyedSnapshot) map[string]struct{} {
	tracked := map[string]struct{}{}
	if snapshot != nil {
		for _, entry := range snapshot.Entries {
			tracked[entry.Key] = struct{}{}
		}
	}
	return tracked
}

// highestSeen is the latest commit that refreshed an entry; zero for an empty snapshot.
func highestSeen(snapshot *projectconfig.KeyedSnapshot) int64 {
	var highest int64
	if snapshot != nil {
		for _, entry := range snapshot.Entries {
			highest = max(highest, entry.Seen)
		}
	}
	return highest
}

// unlistedSinceSweepStart returns tracked IDs that no page listed since the commit counter sweepStartSeen.
func unlistedSinceSweepStart(snapshot *projectconfig.KeyedSnapshot, sweepStartSeen int64, listed []Conversation) []string {
	if snapshot == nil {
		return nil
	}
	listedNow := make(map[string]struct{}, len(listed))
	for _, conversation := range listed {
		listedNow[conversation.ID] = struct{}{}
	}
	var removed []string
	for _, entry := range snapshot.Entries {
		if _, isListed := listedNow[entry.Key]; !isListed && entry.Seen < sweepStartSeen {
			removed = append(removed, entry.Key)
		}
	}
	return removed
}
