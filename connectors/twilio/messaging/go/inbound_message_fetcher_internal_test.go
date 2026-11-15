// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go/internal/twiliofake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// Split literals keep synthetic SIDs from matching repository secret scanning.
	fetcherAccountSID = "AC" + "00112233445566778899aabbccddeeff"
	fetcherAuthToken  = "fetcher-auth-token-0123456789abcd"
	fetcherNumber     = "+14155550100"
	fetcherOtherNum   = "+14155550199"
	fetcherCustomer   = "+14155550123"
)

var fetcherConnection = sdkgo.ConnectionRef{Provider: "twilio", Name: "twilio-messaging"}

type fetcherFixture struct {
	provider *twiliofake.Messages
	client   *Client
	now      time.Time
}

func newFetcherFixture(t *testing.T) *fetcherFixture {
	t.Helper()
	provider := twiliofake.New(t, fetcherAccountSID, fetcherAccountSID, fetcherAuthToken)
	client, err := New(Config{AccountSID: fetcherAccountSID, Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		fetcherConnection: {AuthMethodID: AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(fetcherAuthToken)},
	})
	require.NoError(t, err)
	fixture := &fetcherFixture{provider: provider, client: client, now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	client.now = func() time.Time { return fixture.now }
	return fixture
}

func (fixture *fetcherFixture) fetcher(to string) *inboundMessageFetcher {
	return newInboundMessageFetcher(fixture.client, fetcherConnection, MessageReceivedTriggerConfiguration{To: to})
}

func fetchAll(t *testing.T, fetcher *inboundMessageFetcher, cursor pollingtrigger.Cursor) ([]sdkgo.TriggerEvent[MessageReceivedEvent], pollingtrigger.Cursor) {
	t.Helper()
	var events []sdkgo.TriggerEvent[MessageReceivedEvent]
	for range 20 {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		events = append(events, page.Changes...)
		cursor = page.Next
		if !page.HasMorePages {
			return events, cursor
		}
	}
	t.Fatal("fetcher kept reporting more pages")
	return nil, cursor
}

func TestSeedRecordsABaselineAndLaterInboundMessagesArriveOnceOldestFirst(t *testing.T) {
	fixture := newFetcherFixture(t)
	fixture.provider.Receive(fetcherCustomer, fetcherNumber, "before the baseline", fixture.now.Add(-30*time.Second), "")
	fetcher := fixture.fetcher(fetcherNumber)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"accountSid":"`+fetcherAccountSID+`","to":"`+fetcherNumber+`","watermark":`+strconv.FormatInt(fixture.now.Unix(), 10)+`}`, cursor.Token)
	require.Len(t, cursor.Snapshot.Entries, 1)

	fixture.now = fixture.now.Add(time.Minute)
	second := fixture.provider.Receive(fetcherCustomer, fetcherNumber, "STOP", fixture.now.Add(-10*time.Second), "")
	first := fixture.provider.Receive(fetcherCustomer, fetcherNumber, "hello", fixture.now.Add(-20*time.Second), "")
	fixture.provider.Send(fetcherNumber, fetcherCustomer, "outbound reply", fixture.now.Add(-5*time.Second))
	fixture.provider.Receive(fetcherCustomer, fetcherOtherNum, "to another number", fixture.now.Add(-5*time.Second), "")
	events, cursor := fetchAll(t, fetcher, cursor)
	require.Len(t, events, 2)
	require.Equal(t, "twilio-message-"+first, events[0].ID)
	require.Equal(t, "hello", events[0].Payload.Body)
	require.Equal(t, fetcherCustomer, events[0].Payload.Message.From)
	require.Equal(t, MessageStatusReceived, events[0].Payload.Message.Status)
	require.Equal(t, fixture.now.Add(-20*time.Second), events[0].OccurredAt)
	require.Equal(t, "twilio-message-"+second, events[1].ID)

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "delivered messages in the lookback are never repeated")
	require.Equal(t, cursor, page.Next, "an empty poll leaves the cursor unchanged, so the kit writes nothing")
	for _, query := range fixture.provider.ListRequests() {
		require.Equal(t, fetcherNumber, query.Get("To"))
		require.Equal(t, "200", query.Get("PageSize"))
		require.NotEmpty(t, query.Get("DateSent>"))
	}
}

func TestReceivingMessageWaitsUntilReceivedAndHoldsTheWatermark(t *testing.T) {
	fixture := newFetcherFixture(t)
	fetcher := fixture.fetcher("")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fixture.now = fixture.now.Add(time.Minute)
	mms := fixture.provider.Receive(fetcherCustomer, fetcherNumber, "photo", fixture.now.Add(-50*time.Second), string(MessageStatusReceiving))
	text := fixture.provider.Receive(fetcherCustomer, fetcherOtherNum, "text", fixture.now.Add(-5*time.Second), "")
	events, cursor := fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{"twilio-message-" + text}, eventIDs(events))

	fixture.now = fixture.now.Add(5 * time.Minute)
	fixture.provider.SetStatus(mms, string(MessageStatusReceived))
	events, _ = fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{"twilio-message-" + mms}, eventIDs(events), "the held watermark still lists the finished MMS")
}

func TestManyMessagesSpanProviderPagesAndChangePages(t *testing.T) {
	fixture := newFetcherFixture(t)
	fetcher := fixture.fetcher(fetcherNumber)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fixture.now = fixture.now.Add(time.Minute)
	for index := range 650 {
		fixture.provider.Receive(fetcherCustomer, fetcherNumber, "bulk", fixture.now.Add(-time.Duration(index%60)*time.Second), "")
	}
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, pollingtrigger.MaxChangesPerPage)
	require.True(t, page.HasMorePages)
	rest, _ := fetchAll(t, fetcher, page.Next)
	require.Len(t, rest, 150)
	seen := map[string]bool{}
	for _, event := range append(page.Changes, rest...) {
		require.False(t, seen[event.ID], "event %s delivered twice", event.ID)
		seen[event.ID] = true
	}
}

func TestDateOnlyFiltersStopAtTheFirstMessageBeforeTheLookback(t *testing.T) {
	fixture := newFetcherFixture(t)
	fixture.provider.UseDateOnlyFilters()
	for index := range 2500 {
		fixture.provider.Send(fetcherNumber, fetcherCustomer, "earlier today", fixture.now.Add(-time.Hour-time.Duration(index)*time.Second))
	}
	fetcher := fixture.fetcher("")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err, "a day-wide listing ends at the first message before the lookback")
	fixture.now = fixture.now.Add(time.Minute)
	reply := fixture.provider.Receive(fetcherCustomer, fetcherNumber, "reply", fixture.now.Add(-10*time.Second), "")
	before := len(fixture.provider.ListRequests())
	events, _ := fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{"twilio-message-" + reply}, eventIDs(events))
	require.Equal(t, 1, len(fixture.provider.ListRequests())-before, "one page reaches messages older than the lookback")
}

func TestBusyWindowNarrowsTowardTheWatermarkAndAHeldWindowWaitsForTheNextPoll(t *testing.T) {
	fixture := newFetcherFixture(t)
	fetcher := fixture.fetcher("")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	baseline := fixture.now
	fixture.now = fixture.now.Add(5 * time.Minute)
	held := fixture.provider.Receive(fetcherCustomer, fetcherNumber, "photo", baseline.Add(10*time.Second), string(MessageStatusReceiving))
	early := fixture.provider.Receive(fetcherCustomer, fetcherNumber, "early", baseline.Add(5*time.Second), "")
	for index := range 2100 {
		fixture.provider.Send(fetcherNumber, fetcherCustomer, "campaign", fixture.now.Add(-time.Duration(index%300)*time.Second))
	}
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err, "a busy window narrows toward the watermark")
	require.Equal(t, []string{"twilio-message-" + early}, eventIDs(page.Changes))
	require.False(t, page.HasMorePages, "relisting a held window would return the same messages")

	fixture.provider.SetStatus(held, string(MessageStatusReceived))
	events, _ := fetchAll(t, fetcher, page.Next)
	require.Equal(t, []string{"twilio-message-" + held}, eventIDs(events))
}

func TestRateLimitInvalidCursorAndRejectedCredentials(t *testing.T) {
	fixture := newFetcherFixture(t)
	fetcher := fixture.fetcher(fetcherNumber)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fixture.provider.ThrottleNextList(7 * time.Second)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
	require.Contains(t, err.Error(), "20429")

	_, err = fixture.fetcher(fetcherOtherNum).FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a changed receiving address records a new baseline")
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))

	rejected, err := New(Config{AccountSID: fetcherAccountSID, Endpoint: fixture.provider.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		fetcherConnection: {AuthMethodID: AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString("wrong-token-0123456789abcdef0123")},
	})
	require.NoError(t, err)
	_, err = newInboundMessageFetcher(rejected, fetcherConnection, MessageReceivedTriggerConfiguration{}).SeedCursor(context.Background())
	require.ErrorContains(t, err, "HTTP 401")
	require.NotContains(t, err.Error(), "wrong-token")
	require.False(t, errors.Is(err, context.Canceled))
}

func TestMessageReceivedConfigurationValidation(t *testing.T) {
	for _, valid := range []string{"", fetcherNumber, "whatsapp:" + fetcherNumber} {
		require.NoError(t, MessageReceivedTriggerConfiguration{To: valid}.Validate(), valid)
	}
	for _, invalid := range []string{" +14155550100", "4155550100", "MG0123", "whatsapp:4155550100"} {
		require.Error(t, MessageReceivedTriggerConfiguration{To: invalid}.Validate(), invalid)
	}
	_, err := New(Config{AccountSID: fetcherAccountSID, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 5s")
}

func eventIDs(events []sdkgo.TriggerEvent[MessageReceivedEvent]) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}
