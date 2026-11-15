// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go/internal/xeroinvoicefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	triggerInvoiceA = "8a1d0e2f-0000-4000-8000-00000000000a"
	triggerInvoiceB = "8a1d0e2f-0000-4000-8000-00000000000b"
	triggerInvoiceC = "8a1d0e2f-0000-4000-8000-00000000000c"
	triggerTenantID = "70784a63-d24b-46a9-a4db-0e70a274b056"
)

var triggerConnection = sdkgo.ConnectionRef{Provider: "xero", Name: "xero-invoice-changes"}

func newTriggerTestClient(t *testing.T, fake *xeroinvoicefake.Organisation) *Client {
	t.Helper()
	client, err := New(Config{PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{triggerConnection: {
		AuthMethodID: CustomConnectionAuthMethodID, ClientID: "0F1E2D3C4B5A69788796A5B4C3D2E1F0",
		ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("xero-trigger-token"),
	}}, WithLocalProviderURL(fake.URL))
	require.NoError(t, err)
	return client
}

func newTriggerTestFetcher(t *testing.T, fake *xeroinvoicefake.Organisation, configuration InvoiceChangedTriggerConfiguration) *invoiceChangeFetcher {
	t.Helper()
	return newInvoiceChangeFetcher(newTriggerTestClient(t, fake), triggerConnection, configuration)
}

func decodeInvoiceCursor(t *testing.T, cursor pollingtrigger.Cursor) invoiceChangeCursor {
	t.Helper()
	var position invoiceChangeCursor
	require.NoError(t, json.Unmarshal([]byte(cursor.Token), &position))
	return position
}

func changedInvoiceIDs(page pollingtrigger.ChangePage[sdkgo.TriggerEvent[InvoiceChangedEvent]]) []string {
	var invoiceIDs []string
	for _, change := range page.Changes {
		invoiceIDs = append(invoiceIDs, change.Payload.Invoice.InvoiceID)
	}
	return invoiceIDs
}

func TestInvoiceChangeBaselineStartsAtXeroServerTimeWithoutDeliveringExistingInvoices(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	fake.Upsert(triggerInvoiceA, "INV-0001", "ACCREC", "AUTHORISED", "49.95")
	fake.AdvanceClock(time.Minute)
	fetcher := newTriggerTestFetcher(t, fake, InvoiceChangedTriggerConfiguration{})

	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	position := decodeInvoiceCursor(t, cursor)
	require.Equal(t, xeroinvoicefake.BaseTime.Add(1250*time.Millisecond+time.Minute).UnixMilli(), position.Since, "Xero's DateTimeUTC")
	require.Empty(t, position.Seen, "the existing invoice is older than the overlap window")
	requests := fake.Requests()
	require.Len(t, requests, 1)
	require.Empty(t, requests[0].ModifiedSince)
	require.Equal(t, "UpdatedDateUTC DESC", requests[0].Query.Get("order"))

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "the baseline is never delivered")
	require.Equal(t, cursor.Token, page.Next.Token, "an empty poll leaves the cursor unchanged")
}

func TestInvoiceChangeFetchDeliversEachChangeOnceWithStableIDs(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	fake.Upsert(triggerInvoiceA, "INV-0001", "ACCREC", "DRAFT", "49.95")
	fetcher := newTriggerTestFetcher(t, fake, InvoiceChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Len(t, decodeInvoiceCursor(t, cursor).Seen, 1, "an invoice inside the overlap window is part of the baseline")
	created := fake.Upsert(triggerInvoiceB, "INV-0002", "ACCREC", "DRAFT", "20.00")
	approved := fake.SetStatus(triggerInvoiceA, "AUTHORISED")

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.False(t, page.HasMorePages)
	require.Equal(t, []string{triggerInvoiceB, triggerInvoiceA}, changedInvoiceIDs(page), "in UpdatedDateUTC order")
	require.Equal(t, InvoiceStatusAuthorised, page.Changes[1].Payload.Invoice.Status)
	require.Equal(t, Decimal("49.95"), page.Changes[1].Payload.Invoice.Total)
	require.Equal(t, approved, page.Changes[1].OccurredAt)
	require.Equal(t, invoiceChangeID(triggerInvoiceB, created), page.Changes[0].ID)
	request := fake.Requests()[1]
	require.Equal(t, invoiceChangeWindowStart(decodeInvoiceCursor(t, cursor).Since).Format(modifiedSinceLayout), request.ModifiedSince)
	require.Equal(t, "UpdatedDateUTC ASC", request.Query.Get("order"))
	require.Equal(t, "100", request.Query.Get("pageSize"))

	again, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, page.Changes[0].ID, again.Changes[0].ID, "a refetch from the same cursor yields the same IDs")
	next, err := fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, next.Changes, "the overlap window re-reads both invoices without delivering them again")
	require.Equal(t, page.Next.Token, next.Next.Token)

	sameSecond := fake.UpsertAfter(0, triggerInvoiceC, "INV-0003", "ACCPAY", "AUTHORISED", "5.00")
	require.Equal(t, approved, sameSecond)
	last, err := fetcher.FetchChanges(context.Background(), next.Next)
	require.NoError(t, err)
	require.Equal(t, []string{triggerInvoiceC}, changedInvoiceIDs(last), "a change in the same millisecond as a delivered one still arrives")
}

func TestInvoiceChangeFetchSendsTheBindingFilterAndBaselinesAfterItChanges(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	sales := InvoiceChangedTriggerConfiguration{Type: InvoiceTypeAccountsReceivable, Statuses: []InvoiceStatus{InvoiceStatusPaid, InvoiceStatusAuthorised}}
	fetcher := newTriggerTestFetcher(t, fake, sales)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Upsert(triggerInvoiceA, "INV-0001", "ACCREC", "PAID", "49.95")
	fake.Upsert(triggerInvoiceB, "BILL-1", "ACCPAY", "PAID", "20.00")
	fake.Upsert(triggerInvoiceC, "INV-0003", "ACCREC", "DRAFT", "5.00")

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{triggerInvoiceA}, changedInvoiceIDs(page))
	request := fake.Requests()[1]
	require.Equal(t, `Type=="ACCREC"`, request.Query.Get("where"))
	require.Equal(t, "PAID,AUTHORISED", request.Query.Get("Statuses"))

	bills := newTriggerTestFetcher(t, fake, InvoiceChangedTriggerConfiguration{Type: InvoiceTypeAccountsPayable})
	_, err = bills.FetchChanges(context.Background(), page.Next)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a changed filter records a new baseline: %v", err)
	reordered := newTriggerTestFetcher(t, fake, InvoiceChangedTriggerConfiguration{Type: InvoiceTypeAccountsReceivable, Statuses: []InvoiceStatus{InvoiceStatusAuthorised, InvoiceStatusPaid}})
	_, err = reordered.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err, "the same statuses in another order are the same filter")
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
}

func TestInvoiceChangeFetchMarksACursorFromAnotherOrganisationInvalid(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	cursor, err := newTriggerTestFetcher(t, fake, InvoiceChangedTriggerConfiguration{}).SeedCursor(context.Background())
	require.NoError(t, err)
	client, err := New(Config{Organisation: triggerTenantID, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{triggerConnection: {
		AuthMethodID: OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("xero-oauth-token"),
	}}, WithLocalProviderURL(fake.URL))
	require.NoError(t, err)

	_, err = newInvoiceChangeFetcher(client, triggerConnection, InvoiceChangedTriggerConfiguration{}).FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "%v", err)
	require.ErrorContains(t, err, "another organisation")
}

func TestInvoiceChangeFetchPagesByTimeAndByPageNumberWithinOneWindow(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	fake.LimitPageSize(2)
	fetcher := newTriggerTestFetcher(t, fake, InvoiceChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for _, invoiceID := range []string{triggerInvoiceA, triggerInvoiceB, triggerInvoiceC} {
		fake.UpsertAfter(10*time.Second, invoiceID, "", "ACCREC", "DRAFT", "1.00")
	}

	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.True(t, first.HasMorePages)
	require.Equal(t, 0, decodeInvoiceCursor(t, first.Next).Page, "spread-out changes advance by time")
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.False(t, second.HasMorePages)
	require.Equal(t, []string{triggerInvoiceA, triggerInvoiceB, triggerInvoiceC}, append(changedInvoiceIDs(first), changedInvoiceIDs(second)...))

	burst := []string{"8a1d0e2f-0000-4000-8000-0000000000b1", "8a1d0e2f-0000-4000-8000-0000000000b2", "8a1d0e2f-0000-4000-8000-0000000000b3"}
	fake.UpsertAfter(10*time.Second, burst[0], "", "ACCREC", "DRAFT", "1.00")
	fake.UpsertAfter(0, burst[1], "", "ACCREC", "DRAFT", "1.00")
	fake.UpsertAfter(0, burst[2], "", "ACCREC", "DRAFT", "1.00")
	cursor = second.Next
	var delivered []string
	for range 4 {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		delivered = append(delivered, changedInvoiceIDs(page)...)
		cursor = page.Next
		if !page.HasMorePages {
			break
		}
	}
	require.Equal(t, burst, delivered, "changes sharing one instant page by number and arrive once each")
	require.Equal(t, 0, decodeInvoiceCursor(t, cursor).Page)
}

func TestInvoiceChangeFetchPausesForXeroRateLimitsAndBacksOffOtherwise(t *testing.T) {
	fake := xeroinvoicefake.New(t)
	fetcher := newTriggerTestFetcher(t, fake, InvoiceChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	var rateLimited *pollingtrigger.RateLimitedError

	fake.RateLimitNext(1, "minute", "7")
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, errors.As(err, &rateLimited), "%v", err)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
	fake.RateLimitNext(1, "day", "43200")
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, errors.As(err, &rateLimited), "%v", err)
	require.Equal(t, 12*time.Hour, rateLimited.RetryAfter, "the daily limit pauses until it resets")
	_, err = fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden} {
		fake.FailNext(1, status)
		_, err = fetcher.FetchChanges(context.Background(), cursor)
		require.Error(t, err)
		require.False(t, errors.As(err, &rateLimited), "HTTP %d backs off this binding only", status)
		require.False(t, pollingtrigger.IsCursorInvalid(err))
		require.NotContains(t, err.Error(), "xero-trigger-token")
	}
}

func TestInvoiceChangedTriggerConfigurationValidation(t *testing.T) {
	require.NoError(t, InvoiceChangedTriggerConfiguration{}.Validate())
	require.NoError(t, InvoiceChangedTriggerConfiguration{Type: InvoiceTypeAccountsPayable, Statuses: []InvoiceStatus{InvoiceStatusPaid}}.Validate())
	require.Error(t, InvoiceChangedTriggerConfiguration{Type: "SALES"}.Validate())
	require.Error(t, InvoiceChangedTriggerConfiguration{Statuses: []InvoiceStatus{"OPEN"}}.Validate())
	require.ErrorContains(t, InvoiceChangedTriggerConfiguration{Statuses: []InvoiceStatus{InvoiceStatusPaid, InvoiceStatusPaid}}.Validate(), "more than once")
}

func TestSeenInvoiceChangesArePrunedToTheWindowAndBounded(t *testing.T) {
	since := xeroinvoicefake.BaseTime.Add(time.Hour)
	old := seenInvoiceChangeKey(Invoice{InvoiceID: triggerInvoiceA, UpdatedAt: since.Add(-time.Minute)})
	recent := seenInvoiceChangeKey(Invoice{InvoiceID: triggerInvoiceB, UpdatedAt: since.Add(-time.Second)})
	require.Equal(t, []string{recent}, pruneSeenInvoiceChanges([]string{recent, old, recent}, since.UnixMilli()))
	var many []string
	for index := range maximumSeenInvoiceChanges + 5 {
		many = append(many, seenInvoiceChangeKey(Invoice{InvoiceID: triggerInvoiceC, UpdatedAt: since.Add(time.Duration(index) * time.Millisecond)}))
	}
	pruned := pruneSeenInvoiceChanges(many, since.UnixMilli())
	require.Len(t, pruned, maximumSeenInvoiceChanges)
	require.Equal(t, many[len(many)-1], pruned[len(pruned)-1], "the newest changes are kept")
	encoded, err := encodeInvoiceChangeCursor(invoiceChangeCursor{Filter: "type=;statuses=", Since: since.UnixMilli(), Seen: pruned})
	require.NoError(t, err)
	require.Less(t, len(encoded.Token), 64<<10, "the cursor stays under the kit's token bound")
}
