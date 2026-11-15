// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go/internal/quickbooksinvoicefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const fetcherTestRealmID = "9341453050298464"

var fetcherTestConnection = sdkgo.ConnectionRef{Provider: "quickbooks", Name: "quickbooks-trigger-test"}

func TestInvoicePollSeedsWithoutBacklogAndDeliversEachVersionOnce(t *testing.T) {
	fake := quickbooksinvoicefake.New(t, fetcherTestRealmID)
	existing := fake.Create("ORDER-1", "58", "10.00")
	fetcher := newTestInvoiceChangeFetcher(t, fake.URL, EnvironmentProduction)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Equal(t, "select * from Invoice ORDERBY MetaData.LastUpdatedTime DESC STARTPOSITION 1 MAXRESULTS 100", fake.Queries()[0])

	page := fetchInvoiceChanges(t, fetcher, cursor)
	require.Empty(t, page.Changes, "the baseline invoice is never delivered")
	require.Equal(t, cursor.Token, page.Next.Token, "an empty poll leaves the cursor where it was")
	require.Contains(t, fake.Queries()[1], "where MetaData.LastUpdatedTime >= '2026-11-02T15:59:31Z' ORDERBY MetaData.LastUpdatedTime STARTPOSITION 1 MAXRESULTS 100")

	beforeChanges := page.Next
	created := fake.Create("ORDER-2", "58", "25.50")
	fake.Pay(existing)
	page = fetchInvoiceChanges(t, fetcher, beforeChanges)
	require.Len(t, page.Changes, 2)
	require.Equal(t, created, page.Changes[0].Payload.Invoice.InvoiceID)
	require.Equal(t, InvoiceChangeCreated, page.Changes[0].Payload.ChangeKind)
	require.Equal(t, existing, page.Changes[1].Payload.Invoice.InvoiceID)
	require.Equal(t, InvoiceChangeUpdated, page.Changes[1].Payload.ChangeKind)
	require.Equal(t, Decimal("0"), page.Changes[1].Payload.Invoice.Balance)
	require.Equal(t, fetcherTestRealmID, page.Changes[1].Payload.RealmID)
	require.False(t, page.HasMorePages)

	refetched := fetchInvoiceChanges(t, fetcher, beforeChanges)
	require.Equal(t, page.Changes[0].ID, refetched.Changes[0].ID, "a refetch yields the same Trigger event IDs")
	require.Empty(t, fetchInvoiceChanges(t, fetcher, page.Next).Changes, "the window's versions are already delivered")
}

func TestInvoicePollDeliversAWriteMadeVisibleAfterAPollWithAnEarlierTimestamp(t *testing.T) {
	fake := quickbooksinvoicefake.New(t, fetcherTestRealmID)
	baseline := fake.CreateBackdated("ORDER-0", "58", "1.00", 10*time.Second)
	fake.Create("ORDER-1", "58", "1.00")
	fetcher := newTestInvoiceChangeFetcher(t, fake.URL, EnvironmentProduction)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Empty(t, fetchInvoiceChanges(t, fetcher, cursor).Changes, "the baseline window includes %s", baseline)

	fake.Create("ORDER-2", "58", "2.00")
	page := fetchInvoiceChanges(t, fetcher, cursor)
	require.Len(t, page.Changes, 1)
	cursor = page.Next

	late := fake.CreateBackdated("LATE", "58", "3.00", invoiceChangeLookback-5*time.Second)
	fake.CreateBackdated("TOO-LATE", "58", "4.00", invoiceChangeLookback+5*time.Second)
	page = fetchInvoiceChanges(t, fetcher, cursor)
	require.Len(t, page.Changes, 1, "a write stamped inside the lookback window still arrives; one before it cannot")
	require.Equal(t, late, page.Changes[0].Payload.Invoice.InvoiceID)
	require.Empty(t, fetchInvoiceChanges(t, fetcher, page.Next).Changes)
}

func TestInvoicePollKeepsTheCursorBoundedWhenTheWindowOverflows(t *testing.T) {
	fake := quickbooksinvoicefake.New(t, fetcherTestRealmID)
	fetcher := newTestInvoiceChangeFetcher(t, fake.URL, EnvironmentProduction)
	for index := 0; index < maximumSeenInvoiceVersions+150; index++ {
		if index%100 == 0 {
			fake.ReleaseClock()
			fake.HoldClock()
		}
		fake.Create("BULK", "58", "1.00")
	}
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	_, window, err := decodeInvoiceChangeCursor(cursor.Token)
	require.NoError(t, err)
	require.LessOrEqual(t, len(window.seen), maximumSeenInvoiceVersions)
	require.Less(t, len(cursor.Token), 64*1024)
	require.Empty(t, fetchInvoiceChanges(t, fetcher, cursor).Changes, "a partial oldest second is excluded, never delivered")

	fake.HoldClock()
	for index := 0; index < maximumSeenInvoiceVersions+1; index++ {
		fake.Create("ONE-SECOND", "58", "1.00")
	}
	delivered := map[string]bool{}
	for poll, hasMorePages := 0, true; hasMorePages; poll++ {
		require.Less(t, poll, 200, "paging through the overflowing second ends")
		page := fetchInvoiceChanges(t, fetcher, cursor)
		hasMorePages = page.HasMorePages
		for _, change := range page.Changes {
			require.False(t, delivered[change.ID], "invoice %s delivered twice", change.Payload.Invoice.InvoiceID)
			delivered[change.ID] = true
		}
		require.Less(t, len(page.Next.Token), 64*1024)
		cursor = page.Next
	}
	require.GreaterOrEqual(t, len(delivered), maximumSeenInvoiceVersions)
	_, window, err = decodeInvoiceChangeCursor(cursor.Token)
	require.NoError(t, err)
	require.True(t, window.from.After(window.since.Add(-time.Second)), "a second over the bound moves the window past it")
}

func TestInvoicePollPagesThroughInvoicesThatShareOneSecond(t *testing.T) {
	fake := quickbooksinvoicefake.New(t, fetcherTestRealmID)
	fetcher := newTestInvoiceChangeFetcher(t, fake.URL, EnvironmentProduction)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"realmId":"`+fetcherTestRealmID+`","environment":"production"}`, cursor.Token, "an empty company delivers its first invoice")

	fake.HoldClock()
	for index := 0; index < invoiceChangePageSize+50; index++ {
		fake.Create("BURST", "58", "1.00")
	}
	delivered := map[string]bool{}
	for poll := 0; poll < 5; poll++ {
		page := fetchInvoiceChanges(t, fetcher, cursor)
		for _, change := range page.Changes {
			require.False(t, delivered[change.ID], "invoice %s delivered twice", change.Payload.Invoice.InvoiceID)
			delivered[change.ID] = true
		}
		cursor = page.Next
	}
	require.Len(t, delivered, invoiceChangePageSize+50)

	late := fake.Create("LATE", "58", "2.00")
	var changes []sdkgo.TriggerEvent[InvoiceChangedEvent]
	for isFirstPage, page := true, (pollingtrigger.ChangePage[sdkgo.TriggerEvent[InvoiceChangedEvent]]{}); isFirstPage || page.HasMorePages; isFirstPage = false {
		page = fetchInvoiceChanges(t, fetcher, cursor)
		changes, cursor = append(changes, page.Changes...), page.Next
	}
	require.Len(t, changes, 1, "an invoice written in the same second after the poll still arrives once")
	require.Equal(t, late, changes[0].Payload.Invoice.InvoiceID)
}

func TestInvoicePollThrottlingPausesForRetryAfterOrTheDocumentedMinute(t *testing.T) {
	fake := quickbooksinvoicefake.New(t, fetcherTestRealmID)
	fetcher := newTestInvoiceChangeFetcher(t, fake.URL, EnvironmentProduction)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fake.Throttle(1, "7")
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	fake.Throttle(1, "")
	_, err = fetcher.SeedCursor(context.Background())
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, defaultThrottleDelay, rateLimited.RetryAfter)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestInvoicePollResetsACursorTheCompanyCannotServe(t *testing.T) {
	fake := quickbooksinvoicefake.New(t, fetcherTestRealmID)
	fetcher := newTestInvoiceChangeFetcher(t, fake.URL, EnvironmentProduction)
	emptyCompany, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Create("ORDER-1", "58", "1.00")
	seeded, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fake.RejectWatermarkQueries(true)
	_, err = fetcher.FetchChanges(context.Background(), seeded)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a validation Fault for the watermark query reseeds")
	_, err = fetcher.FetchChanges(context.Background(), emptyCompany)
	require.NoError(t, err, "a query without a watermark is not rejected")
	fake.RejectWatermarkQueries(false)

	for name, token := range map[string]string{
		"undecodable":            "not json",
		"another company":        `{"realmId":"1","environment":"production"}`,
		"another host":           `{"realmId":"` + fetcherTestRealmID + `","environment":"sandbox"}`,
		"invalid watermark":      `{"realmId":"` + fetcherTestRealmID + `","environment":"production","since":"yesterday","from":"yesterday"}`,
		"bound after watermark":  `{"realmId":"` + fetcherTestRealmID + `","environment":"production","since":"2026-11-02T16:00:00Z","from":"2026-11-02T16:00:01Z"}`,
		"version outside window": `{"realmId":"` + fetcherTestRealmID + `","environment":"production","since":"2026-11-02T16:00:00Z","from":"2026-11-02T15:59:30Z","seen":["1000/1:0"]}`,
		"negative offset":        `{"realmId":"` + fetcherTestRealmID + `","environment":"production","offset":-1}`,
	} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), name)
	}
}

func fetchInvoiceChanges(t *testing.T, fetcher *invoiceChangeFetcher, cursor pollingtrigger.Cursor) pollingtrigger.ChangePage[sdkgo.TriggerEvent[InvoiceChangedEvent]] {
	t.Helper()
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	return page
}

func newTestInvoiceChangeFetcher(t *testing.T, providerURL string, environment Environment) *invoiceChangeFetcher {
	t.Helper()
	claims, err := json.Marshal(map[string]any{"iss": "https://oauth.platform.intuit.com/op/v1", "aud": []string{"client"}, "realmid": fetcherTestRealmID})
	require.NoError(t, err)
	idToken := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims) + ".c2ln"
	client, err := New(Config{Environment: environment}, sdkgo.StaticCredentialProvider[Credentials]{fetcherTestConnection: {
		ClientID: "client", ClientSecret: sdkgo.NewSecretString("secret"),
		AccessToken: sdkgo.NewSecretString("trigger-access-token"), IDToken: sdkgo.NewSecretString(idToken),
	}}, WithLocalProviderURL(providerURL))
	require.NoError(t, err)
	return newInvoiceChangeFetcher(client, fetcherTestConnection)
}

func TestInvoicePollDoesNotSkipAnInvoiceWhenAnEarlierOneLeavesTheSecondBetweenPages(t *testing.T) {
	fake := quickbooksinvoicefake.New(t, fetcherTestRealmID)
	fetcher := newTestInvoiceChangeFetcher(t, fake.URL, EnvironmentProduction)
	fake.HoldClock()
	fake.Create("BASELINE", "58", "1.00")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	var burst []string
	for index := 0; index < 2*invoiceChangePageSize; index++ {
		burst = append(burst, fake.Create("BURST", "58", "1.00"))
	}
	fake.ReleaseClock()

	first := fetchInvoiceChanges(t, fetcher, cursor)
	require.True(t, first.HasMorePages)
	for _, paid := range burst[:invoiceTiePageOverlap/2] {
		fake.Pay(paid)
	}
	delivered := map[string]bool{}
	for _, change := range first.Changes {
		delivered[invoiceVersionKey(change.Payload.Invoice)] = true
	}
	for poll, page := 0, first; page.HasMorePages; poll++ {
		require.Less(t, poll, 20)
		page = fetchInvoiceChanges(t, fetcher, page.Next)
		for _, change := range page.Changes {
			key := invoiceVersionKey(change.Payload.Invoice)
			require.False(t, delivered[key], "version %s delivered twice", key)
			delivered[key] = true
		}
	}
	require.Len(t, delivered, 2*invoiceChangePageSize+invoiceTiePageOverlap/2, "every burst invoice and every payment arrives once")
}
