// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// inboundListPageSize keeps a page of full message resources well under the default response limit;
	// Twilio allows 1 to 1000 and defaults to 50.
	inboundListPageSize = 200
	// maximumWindowPages bounds one listing at 2,000 messages; a larger window is split by DateSent.
	maximumWindowPages = 10
	// maximumWindowNarrowings halves a window down to one second from more than 500 years.
	maximumWindowNarrowings = 34
	// inboundLookback relists recent seconds, so a message Twilio dates slightly late is still found.
	inboundLookback = 2 * time.Minute
	// receivingHoldLimit bounds how long a message still in receiving holds the watermark back.
	receivingHoldLimit = 10 * time.Minute
)

// inboundSnapshotOptions bounds the keys of messages delivered within the lookback.
var inboundSnapshotOptions = pollingtrigger.KeyedSnapshotOptions{}

// inboundMessageFetcher reads one account's Messages list for the poll Trigger kit. Twilio has no change
// feed, so the cursor is a DateSent watermark plus a snapshot of the message SIDs delivered in the lookback.
type inboundMessageFetcher struct {
	client *Client
	call   sdkgo.Call
	to     string
}

// inboundMessageCursor is the committed cursor token. Watermark is a Unix second of Twilio's date_sent.
type inboundMessageCursor struct {
	AccountSID string `json:"accountSid"`
	To         string `json:"to,omitempty"`
	Watermark  int64  `json:"watermark"`
}

type twilioMessagePage struct {
	Messages    *[]twilioMessageResource `json:"messages"`
	NextPageURI *string                  `json:"next_page_uri"`
}

// listedMessage is one decoded list entry; sentAtSecond is zero while Twilio reports no date_sent.
type listedMessage struct {
	message      Message
	body         string
	sentAtSecond int64
}

// inboundMessageWindow is a complete listing of [since, upperBound]; a zero upperBound is unbounded.
type inboundMessageWindow struct {
	messages   []listedMessage
	upperBound int64
}

func newInboundMessageFetcher(client *Client, connection sdkgo.ConnectionRef, configuration MessageReceivedTriggerConfiguration) *inboundMessageFetcher {
	return &inboundMessageFetcher{
		client: client, to: configuration.To,
		call: pollingtrigger.NewTriggerCredentialCall(connection, MessageReceivedTriggerDefinition.Trigger),
	}
}

// SeedCursor lists the lookback before now and records every inbound message in it as already delivered,
// so existing messages become the baseline and are never delivered.
func (fetcher *inboundMessageFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	now := fetcher.client.now().UTC().Unix()
	since := now - int64(inboundLookback/time.Second)
	window, err := fetcher.listWindow(ctx, since, 0)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	if window.upperBound != 0 {
		return pollingtrigger.Cursor{}, fmt.Errorf("Twilio listed more than %d messages within the baseline lookback", inboundListPageSize*maximumWindowPages)
	}
	watermark := max(now, latestSentAtSecond(window.messages))
	var delivered []pollingtrigger.KeyedItem
	for _, listed := range window.messages {
		if fetcher.isReceivedHere(listed) && listed.sentAtSecond >= since {
			delivered = append(delivered, newInboundSnapshotKey(listed))
		}
	}
	return fetcher.encodeCursor(watermark, nil, delivered)
}

// FetchChanges lists messages dated from the watermark minus the lookback and returns, oldest first, up to
// pollingtrigger.MaxChangesPerPage inbound messages the snapshot has not recorded. A message still in
// receiving waits, and holds the watermark back, until received or receivingHoldLimit. An undecodable
// cursor, or one for another account or receiving address, is marked invalid so the kit records a new baseline.
func (fetcher *inboundMessageFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[MessageReceivedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[MessageReceivedEvent]]
	var position inboundMessageCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.Watermark <= 0 {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Twilio messageReceived cursor is not decodable"))
	}
	if position.AccountSID != fetcher.client.accountSID || position.To != fetcher.to {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Twilio messageReceived cursor belongs to another account or receiving address"))
	}
	since := position.Watermark - int64(inboundLookback/time.Second)
	// A narrowed window must still pass the watermark, or the poll would make no progress.
	window, err := fetcher.listWindow(ctx, since, position.Watermark+1)
	if err != nil {
		return page, err
	}
	now := fetcher.client.now()
	delivered := map[string]bool{}
	if cursor.Snapshot != nil {
		for _, entry := range cursor.Snapshot.Entries {
			delivered[entry.Key] = true
		}
	}
	holdBound := int64(0)
	holdAt := func(second int64) {
		if holdBound == 0 || second < holdBound {
			holdBound = second
		}
	}
	var pending []listedMessage
	for _, listed := range window.messages {
		switch {
		case !fetcher.isReceivedHere(listed):
		case listed.sentAtSecond == 0:
			holdAt(position.Watermark)
		case listed.sentAtSecond < since || delivered[inboundSnapshotKeyFor(listed)]:
		case listed.message.Status == MessageStatusReceiving && now.Sub(listed.message.SentAt) < receivingHoldLimit:
			holdAt(listed.sentAtSecond)
		default:
			pending = append(pending, listed)
		}
	}
	slices.SortFunc(pending, func(left, right listedMessage) int {
		if left.sentAtSecond != right.sentAtSecond {
			return cmp.Compare(left.sentAtSecond, right.sentAtSecond)
		}
		return strings.Compare(left.message.SID, right.message.SID)
	})
	var watermark int64
	isPageFull := len(pending) > pollingtrigger.MaxChangesPerPage
	switch {
	case isPageFull:
		pending = pending[:pollingtrigger.MaxChangesPerPage]
		watermark, page.HasMorePages = pending[len(pending)-1].sentAtSecond, true
	case window.upperBound != 0:
		watermark, page.HasMorePages = window.upperBound, true
	default:
		watermark = latestSentAtSecond(window.messages)
	}
	if holdBound != 0 && watermark > holdBound {
		watermark = holdBound
		// Relisting a held window returns the same messages, so only a full change page is worth refetching.
		page.HasMorePages = isPageFull
	}
	watermark = max(watermark, position.Watermark)
	upserted := make([]pollingtrigger.KeyedItem, 0, len(pending))
	for _, listed := range pending {
		page.Changes = append(page.Changes, newMessageReceivedTriggerEvent(listed.message, listed.body, now))
		upserted = append(upserted, newInboundSnapshotKey(listed))
	}
	page.Next, err = fetcher.encodeCursor(watermark, cursor.Snapshot, upserted)
	return page, err
}

// isReceivedHere reports an inbound message to the watched address, or to any address when none is set.
func (fetcher *inboundMessageFetcher) isReceivedHere(listed listedMessage) bool {
	return listed.message.Direction == InboundMessageDirection && (fetcher.to == "" || listed.message.To == fetcher.to)
}

// listWindow lists every message dated from since. Over maximumWindowPages pages it halves the window's
// upper bound toward minimumUpperBound (since when zero) until the listing is complete.
func (fetcher *inboundMessageFetcher) listWindow(ctx context.Context, since int64, minimumUpperBound int64) (inboundMessageWindow, error) {
	floor := since
	if minimumUpperBound != 0 {
		floor = max(minimumUpperBound, since)
	}
	span := max(fetcher.client.now().UTC().Unix()-floor, 0)
	upperBound := int64(0)
	for narrowing := 0; narrowing <= maximumWindowNarrowings; narrowing++ {
		messages, isTruncated, err := fetcher.listMessages(ctx, since, upperBound)
		if err != nil {
			return inboundMessageWindow{}, err
		}
		if !isTruncated {
			return inboundMessageWindow{messages: messages, upperBound: upperBound}, nil
		}
		if upperBound == floor {
			break
		}
		span /= 2
		upperBound = floor + span
	}
	return inboundMessageWindow{}, fmt.Errorf("Twilio listed more than %d messages within %s of the watermark; the messageReceived Trigger cannot page through them",
		inboundListPageSize*maximumWindowPages, inboundLookback)
}

// listMessages follows next_page_uri for at most maximumWindowPages pages and reports whether more remain.
// Twilio lists newest first by DateSent, so a page reaching a message dated before since ends the listing.
// DateSent bounds are inclusive and also applied locally: Twilio documents them as dates, so it may list
// the whole day before since.
func (fetcher *inboundMessageFetcher) listMessages(ctx context.Context, since int64, upperBound int64) ([]listedMessage, bool, error) {
	query := url.Values{"PageSize": {strconv.Itoa(inboundListPageSize)}, "DateSent>": {time.Unix(since, 0).UTC().Format(time.RFC3339)}}
	if upperBound != 0 {
		query.Set("DateSent<", time.Unix(upperBound, 0).UTC().Format(time.RFC3339))
	}
	if fetcher.to != "" {
		query.Set("To", fetcher.to)
	}
	target := fetcher.client.messagesURL + ".json?" + query.Encode()
	var messages []listedMessage
	for pageNumber := 0; pageNumber < maximumWindowPages; pageNumber++ {
		listed, nextPageURI, err := fetcher.listPage(ctx, target)
		if err != nil {
			return nil, false, err
		}
		hasReachedSince := false
		for _, entry := range listed {
			if entry.sentAtSecond != 0 && entry.sentAtSecond < since {
				hasReachedSince = true
				continue
			}
			if entry.sentAtSecond != 0 && upperBound != 0 && entry.sentAtSecond > upperBound {
				continue
			}
			messages = append(messages, entry)
		}
		if nextPageURI == "" || hasReachedSince {
			return messages, false, nil
		}
		if target, err = fetcher.resolveNextPage(nextPageURI); err != nil {
			return nil, false, err
		}
	}
	return messages, true, nil
}

// resolveNextPage accepts only a next page of this account's Messages list on the configured host.
func (fetcher *inboundMessageFetcher) resolveNextPage(nextPageURI string) (string, error) {
	base, err := url.Parse(fetcher.client.messagesURL + ".json")
	if err != nil {
		return "", errRequestNotBuilt
	}
	next, err := url.Parse(nextPageURI)
	if err != nil || next.IsAbs() || next.Host != "" || next.Path != base.Path || next.RawQuery == "" {
		return "", errors.New("Twilio Messages list returned an unexpected next_page_uri")
	}
	return base.Scheme + "://" + base.Host + next.Path + "?" + next.RawQuery, nil
}

// listPage sends one Messages list request with the connection's Basic credential. A 429 pauses the
// connection for Retry-After, or backs off exponentially without it; Twilio did not process the request.
func (fetcher *inboundMessageFetcher) listPage(ctx context.Context, target string) ([]listedMessage, string, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return nil, "", err
	}
	credentials, err := sdkgo.ResolveCredential(ctx, fetcher.client.credentials, fetcher.call, twilioCredentialRefreshDriver{})
	if errors.Is(err, sdkgo.ErrReauthorizationRequired) {
		return nil, "", errors.New("Twilio connection credentials require reauthorization; reconnect the connection")
	}
	if err != nil {
		return nil, "", errors.New("Twilio messageReceived credentials are unavailable")
	}
	authorization, failure, _ := fetcher.client.validateAuthorization(credentials, MessageReceivedTriggerDefinition.Trigger.TriggerName)
	if failure != nil {
		return nil, "", errors.New(failure.Message)
	}
	requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	response, _, err := fetcher.client.exchangeWithContext(requestContext, authorization, http.MethodGet, target, nil, false)
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, "", ctx.Err()
	case errors.Is(err, errRequestNotBuilt):
		return nil, "", err
	case err != nil:
		return nil, "", errors.New("Twilio Messages list request failed before a response")
	case response.statusCode == http.StatusTooManyRequests:
		failure := fmt.Errorf("Twilio Messages list returned HTTP 429 (error %d)", readErrorCode(response.body))
		return nil, "", pollingtrigger.MarkRateLimited(failure, providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), fetcher.client.now()))
	case response.statusCode == http.StatusUnauthorized || response.statusCode == http.StatusForbidden:
		return nil, "", fmt.Errorf("Twilio rejected the Messages list credentials (HTTP %d, error %d); update the connection or grant the API key Messages read access",
			response.statusCode, readErrorCode(response.body))
	case response.statusCode < 200 || response.statusCode >= 300:
		return nil, "", fmt.Errorf("Twilio Messages list returned HTTP %d (error %d)", response.statusCode, readErrorCode(response.body))
	case response.hasBodyReadFailed:
		return nil, "", errors.New("Twilio Messages list response was interrupted")
	case response.isBodyTooLarge:
		return nil, "", errors.New("Twilio Messages list page exceeds maxResponseBytes; raise it on the connection")
	}
	return fetcher.decodePage(response.body, authorization)
}

func (fetcher *inboundMessageFetcher) decodePage(contents []byte, authorization basicAuthorization) ([]listedMessage, string, error) {
	var page twilioMessagePage
	if err := json.Unmarshal(contents, &page); err != nil || page.Messages == nil {
		return nil, "", errors.New("Twilio Messages list page is not valid JSON")
	}
	listed := make([]listedMessage, 0, len(*page.Messages))
	for _, resource := range *page.Messages {
		message, err := fetcher.client.convertMessageResource(resource, authorization)
		if err != nil {
			return nil, "", errors.New("Twilio Messages list page has an invalid message")
		}
		body := valueOrEmpty(resource.Body)
		// A body that is invalid or quotes the credential is dropped rather than wedging the poll.
		if !utf8.ValidString(body) || (authorization.password != "" && strings.Contains(body, authorization.password)) {
			body = ""
		}
		entry := listedMessage{message: message, body: body}
		if !message.SentAt.IsZero() {
			entry.sentAtSecond = message.SentAt.Unix()
		}
		listed = append(listed, entry)
	}
	return listed, valueOrEmpty(page.NextPageURI), nil
}

// encodeCursor adds upserted to the snapshot and drops keys dated before the new lookback.
func (fetcher *inboundMessageFetcher) encodeCursor(watermark int64, previous *projectconfig.KeyedSnapshot, upserted []pollingtrigger.KeyedItem) (pollingtrigger.Cursor, error) {
	cutoff := watermark - int64(inboundLookback/time.Second)
	var expired []string
	if previous != nil {
		for _, entry := range previous.Entries {
			if second, ok := inboundSnapshotKeySecond(entry.Key); !ok || second < cutoff {
				expired = append(expired, entry.Key)
			}
		}
	}
	snapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(previous, upserted, expired, inboundSnapshotOptions)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("Twilio messageReceived snapshot: %w", err)
	}
	encoded, err := json.Marshal(inboundMessageCursor{AccountSID: fetcher.client.accountSID, To: fetcher.to, Watermark: watermark})
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Twilio messageReceived cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded), Snapshot: snapshot}, nil
}

// inboundSnapshotKey records one delivered message as "<date_sent Unix second>-<SID>", so pruning needs no values.
type inboundSnapshotKey struct{ key string }

func newInboundSnapshotKey(listed listedMessage) inboundSnapshotKey {
	return inboundSnapshotKey{key: inboundSnapshotKeyFor(listed)}
}

func inboundSnapshotKeyFor(listed listedMessage) string {
	return fmt.Sprintf("%011d-%s", listed.sentAtSecond, listed.message.SID)
}

func inboundSnapshotKeySecond(key string) (int64, bool) {
	prefix, _, found := strings.Cut(key, "-")
	second, err := strconv.ParseInt(prefix, 10, 64)
	return second, found && err == nil
}

// SnapshotKey returns the dated message key.
func (item inboundSnapshotKey) SnapshotKey() string { return item.key }

// SnapshotHash returns a hash of the key; a delivered message never changes its entry.
func (item inboundSnapshotKey) SnapshotHash() string {
	digest := sha256.Sum256([]byte(item.key))
	return hex.EncodeToString(digest[:8])
}

// SnapshotValue returns nil because entries keep no values.
func (inboundSnapshotKey) SnapshotValue() json.RawMessage { return nil }

func latestSentAtSecond(messages []listedMessage) int64 {
	latest := int64(0)
	for _, listed := range messages {
		latest = max(latest, listed.sentAtSecond)
	}
	return latest
}
