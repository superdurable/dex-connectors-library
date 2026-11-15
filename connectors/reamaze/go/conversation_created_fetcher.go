// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	conversationCreatedOperation = "conversationCreated"

	// ConversationCreatedLateIndexWindow is how far before the newest creation time a poll still accepts a
	// conversation it has not seen, because Re:amaze lists a conversation some time after creating it.
	ConversationCreatedLateIndexWindow = 10 * time.Minute
	// MaxConversationCreatedPagesPerPoll bounds the GET /conversations pages one poll reads, 480
	// conversations, below pollingtrigger.MaxChangesPerPage.
	MaxConversationCreatedPagesPerPoll = 16
	// maximumSeenConversations keeps the cursor token well below the kit's 64 KiB bound.
	maximumSeenConversations = 800
)

// conversationCreatedFetcher polls one brand's, or one channel's, GET /conversations for the poll kit.
type conversationCreatedFetcher struct {
	client  *Client
	call    sdkgo.Call
	channel string
	logger  *slog.Logger
	binding string
}

// conversationCreatedCursor is the committed Trigger cursor. Re:amaze lists conversations by creation
// time with no change token, so the cursor holds the newest creation time, a floor below which nothing
// is delivered, and the conversations already seen at or above the floor.
type conversationCreatedCursor struct {
	Brand           string             `json:"brand"`
	Channel         string             `json:"channel,omitempty"`
	WatermarkMillis int64              `json:"watermark"`
	FloorMillis     int64              `json:"floor"`
	Seen            []seenConversation `json:"seen"`
}

// seenConversation is a slug digest, which bounds the entry size for any slug length.
type seenConversation struct {
	Digest          string `json:"d"`
	CreatedAtMillis int64  `json:"t"`
}

// conversationWalk is the newest end of the listing that one poll read.
type conversationWalk struct {
	conversations []Conversation
	// reachesFloor reports that the walk read every conversation at or above the floor it was given.
	reachesFloor bool
	pagesRead    int
}

func newConversationCreatedFetcher(client *Client, connection sdkgo.ConnectionRef, bindingName string, configuration ConversationCreatedTriggerConfiguration) *conversationCreatedFetcher {
	logger := client.logger
	if logger == nil {
		logger = slog.Default()
	}
	return &conversationCreatedFetcher{
		client:  client,
		call:    pollingtrigger.NewTriggerCredentialCall(connection, ConversationCreatedTriggerDefinition.Trigger),
		channel: configuration.Channel, logger: logger, binding: bindingName,
	}
}

// SeedCursor reads only the newest page, so existing conversations become the baseline and are never
// delivered.
func (fetcher *conversationCreatedFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	walk, err := fetcher.walkNewestConversations(ctx, 0, true)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	empty := conversationCreatedCursor{Brand: fetcher.client.brand, Channel: fetcher.channel}
	return fetcher.encodeCursor(advanceConversationCreatedCursor(empty, walk))
}

// FetchChanges delivers every listed conversation at or above the floor that the cursor has not seen,
// oldest first. An undecodable cursor, or one for another brand or channel, is marked invalid, so the kit
// records a new baseline.
func (fetcher *conversationCreatedFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationCreatedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationCreatedEvent]]
	var position conversationCreatedCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.Brand == "" {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Re:amaze conversationCreated cursor is not decodable"))
	}
	if position.Brand != fetcher.client.brand || position.Channel != fetcher.channel {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Re:amaze conversationCreated cursor belongs to another brand or channel"))
	}
	walk, err := fetcher.walkNewestConversations(ctx, position.FloorMillis, false)
	if err != nil {
		return page, err
	}
	if !walk.reachesFloor {
		fetcher.logger.Warn("poll backlog exceeds page bound; older new conversations skipped",
			"connector", ConnectorID, "trigger", ConversationCreatedTriggerDefinition.Trigger.TriggerName,
			"binding", fetcher.binding, "pages", walk.pagesRead)
	}
	seen := make(map[string]bool, len(position.Seen))
	for _, entry := range position.Seen {
		seen[entry.Digest] = true
	}
	var fresh []Conversation
	for _, conversation := range walk.conversations {
		if conversation.CreatedAt.UnixMilli() >= position.FloorMillis && !seen[conversationDigest(conversation.ID)] {
			fresh = append(fresh, conversation)
		}
	}
	sort.SliceStable(fresh, func(i, j int) bool {
		if !fresh[i].CreatedAt.Equal(fresh[j].CreatedAt) {
			return fresh[i].CreatedAt.Before(fresh[j].CreatedAt)
		}
		return fresh[i].ID < fresh[j].ID
	})
	for _, conversation := range fresh {
		page.Changes = append(page.Changes, newConversationCreatedTriggerEvent(fetcher.client.brand, fetcher.channel, conversation))
	}
	page.Next, err = fetcher.encodeCursor(advanceConversationCreatedCursor(position, walk))
	return page, err
}

// walkNewestConversations reads pages from the newest end of the listing until one holds a conversation
// created before floorMillis, so every conversation at the floor, ties across a page edge included, is
// read; or until the listing ends, or MaxConversationCreatedPagesPerPoll pages were read; a
// seed reads only the newest page. Re:amaze documents its default order as creation time without a
// direction, so the walk detects it from the first page and starts from the last page when it ascends.
func (fetcher *conversationCreatedFetcher) walkNewestConversations(ctx context.Context, floorMillis int64, isSeed bool) (conversationWalk, error) {
	first, err := fetcher.listConversationPage(ctx, 1)
	if err != nil {
		return conversationWalk{}, err
	}
	walk := conversationWalk{pagesRead: 1}
	isAscending := len(first.Conversations) > 1 && first.Conversations[0].CreatedAt.Before(first.Conversations[len(first.Conversations)-1].CreatedAt)
	pageNumber, step, current := 1, 1, first
	if isAscending && first.PageCount > 1 {
		if first.PageCount > MaxSearchPage {
			return conversationWalk{}, fmt.Errorf("Re:amaze lists %d conversation pages oldest first, more than the %d a poll can reach", first.PageCount, MaxSearchPage)
		}
		pageNumber, step = first.PageCount, -1
		if current, err = fetcher.listConversationPage(ctx, pageNumber); err != nil {
			return conversationWalk{}, err
		}
		walk.pagesRead++
	}
	listed := map[string]bool{}
	for {
		isBelowFloor := false
		for _, conversation := range current.Conversations {
			if conversation.CreatedAt.UnixMilli() < floorMillis {
				isBelowFloor = true
			}
			if !listed[conversation.ID] {
				listed[conversation.ID] = true
				walk.conversations = append(walk.conversations, conversation)
			}
		}
		nextPageNumber := pageNumber + step
		isLastPage := len(current.Conversations) == 0 || nextPageNumber < 1 || nextPageNumber > min(current.PageCount, MaxSearchPage)
		if isBelowFloor || isLastPage {
			walk.reachesFloor = true
			return walk, nil
		}
		if isSeed || walk.pagesRead >= MaxConversationCreatedPagesPerPoll {
			return walk, nil
		}
		pageNumber = nextPageNumber
		if current, err = fetcher.listConversationPage(ctx, pageNumber); err != nil {
			return conversationWalk{}, err
		}
		walk.pagesRead++
	}
}

// listConversationPage sends one GET /conversations request for every view, so archived and spam
// conversations are listed too, narrowed to the configured channel.
func (fetcher *conversationCreatedFetcher) listConversationPage(ctx context.Context, pageNumber int) (SearchConversationsOutput, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return SearchConversationsOutput{}, err
	}
	credentials, err := fetcher.resolveCredentials(ctx)
	if err != nil {
		return SearchConversationsOutput{}, err
	}
	query, err := BuildConversationSearchQuery(SearchConversationsInput{Filter: ConversationFilterAll, Channel: fetcher.channel, Page: pageNumber})
	if err != nil {
		return SearchConversationsOutput{}, conversationCreatedTriggerConfigurationError(err)
	}
	result := fetcher.client.exchange(fetcher.call, credentials, conversationCreatedOperation, reamazeRequest{
		method: http.MethodGet, path: "/conversations", query: query, requestContext: ctx,
	})
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited:
		// Re:amaze limits requests per minute per API token; the limit is shared with operation Steps.
		return SearchConversationsOutput{}, pollingtrigger.MarkRateLimited(errors.New("Re:amaze conversationCreated poll: "+result.failure.Message), result.retryAfter)
	default:
		return SearchConversationsOutput{}, errors.New("Re:amaze conversationCreated poll: " + result.failure.Message)
	}
	listed, err := decodeConversationPage(result.response.body, pageNumber, true)
	if err != nil {
		return SearchConversationsOutput{}, fmt.Errorf("Re:amaze conversationCreated poll: invalid conversation page: %w", err)
	}
	return listed, nil
}

// resolveCredentials reads the connection credentials under the poll's ctx, since a Trigger call has no
// Dex Step context.
func (fetcher *conversationCreatedFetcher) resolveCredentials(ctx context.Context) (Credentials, error) {
	var credentials Credentials
	var err error
	if contextProvider, ok := fetcher.client.credentials.(sdkgo.ContextCredentialProvider[Credentials]); ok {
		credentials, err = contextProvider.ResolveContext(ctx, fetcher.call)
	} else {
		credentials, err = fetcher.client.credentials.Resolve(fetcher.call)
	}
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, errors.New("Re:amaze conversationCreated poll: " + errReamazeCredentialsUnusable.Error())
	}
	return credentials, nil
}

// advanceConversationCreatedCursor moves the watermark to the newest walked conversation and keeps every
// conversation at or above the new floor as seen. A walk that stopped at the page bound, such as a seed,
// raises the floor just past its oldest conversation, and trimming the seen set raises it just past the
// newest entry dropped. Both raises exclude a whole creation millisecond, so a conversation that shares it
// and was not read, or whose seen entry was dropped, is never delivered later as new.
func advanceConversationCreatedCursor(position conversationCreatedCursor, walk conversationWalk) conversationCreatedCursor {
	next := conversationCreatedCursor{Brand: position.Brand, Channel: position.Channel, WatermarkMillis: position.WatermarkMillis}
	oldestWalkedMillis := int64(0)
	for index, conversation := range walk.conversations {
		createdAtMillis := conversation.CreatedAt.UnixMilli()
		next.WatermarkMillis = max(next.WatermarkMillis, createdAtMillis)
		if index == 0 || createdAtMillis < oldestWalkedMillis {
			oldestWalkedMillis = createdAtMillis
		}
	}
	next.FloorMillis = max(position.FloorMillis, next.WatermarkMillis-ConversationCreatedLateIndexWindow.Milliseconds())
	if !walk.reachesFloor && len(walk.conversations) != 0 {
		next.FloorMillis = max(next.FloorMillis, oldestWalkedMillis+1)
	}
	newestByDigest := map[string]int64{}
	for _, entry := range position.Seen {
		newestByDigest[entry.Digest] = max(newestByDigest[entry.Digest], entry.CreatedAtMillis)
	}
	for _, conversation := range walk.conversations {
		digest := conversationDigest(conversation.ID)
		newestByDigest[digest] = max(newestByDigest[digest], conversation.CreatedAt.UnixMilli())
	}
	for digest, createdAtMillis := range newestByDigest {
		if createdAtMillis >= next.FloorMillis {
			next.Seen = append(next.Seen, seenConversation{Digest: digest, CreatedAtMillis: createdAtMillis})
		}
	}
	sort.Slice(next.Seen, func(i, j int) bool {
		if next.Seen[i].CreatedAtMillis != next.Seen[j].CreatedAtMillis {
			return next.Seen[i].CreatedAtMillis > next.Seen[j].CreatedAtMillis
		}
		return next.Seen[i].Digest < next.Seen[j].Digest
	})
	if len(next.Seen) > maximumSeenConversations {
		next.FloorMillis = max(next.FloorMillis, next.Seen[maximumSeenConversations].CreatedAtMillis+1)
		kept := next.Seen[:0]
		for _, entry := range next.Seen[:maximumSeenConversations] {
			if entry.CreatedAtMillis >= next.FloorMillis {
				kept = append(kept, entry)
			}
		}
		next.Seen = kept
	}
	if next.Seen == nil {
		next.Seen = []seenConversation{}
	}
	return next
}

func conversationDigest(slug string) string {
	digest := sha256.Sum256([]byte(slug))
	return hex.EncodeToString(digest[:8])
}

func (fetcher *conversationCreatedFetcher) encodeCursor(position conversationCreatedCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Re:amaze conversationCreated cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
