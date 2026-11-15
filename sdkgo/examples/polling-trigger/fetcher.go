// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ItemChanged is the Trigger event payload.
type ItemChanged struct {
	// ItemID is the provider item that changed.
	ItemID string `json:"itemId"`
	// Sequence is the change's position in the provider change log.
	Sequence int64 `json:"sequence"`
	// Title is the item title after the change.
	Title string `json:"title"`
}

// changeLogFetcher is the connector side: it reads the provider feed and never touches storage or targets.
type changeLogFetcher struct {
	api *changeLogClient
}

// SeedCursor stores the newest sequence so existing items are never delivered.
func (fetcher *changeLogFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return pollingtrigger.Cursor{}, err
	}
	latest, err := fetcher.api.latestSequence(ctx)
	if err != nil {
		return pollingtrigger.Cursor{}, fetcher.classify(err)
	}
	return pollingtrigger.Cursor{Token: strconv.FormatInt(latest, 10)}, nil
}

// FetchChanges reads one page after the committed sequence.
func (fetcher *changeLogFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[ItemChanged]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ItemChanged]]
	after, err := strconv.ParseInt(cursor.Token, 10, 64)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(fmt.Errorf("change-log cursor is not a sequence"))
	}
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return page, err
	}
	listed, err := fetcher.api.listChanges(ctx, after, 100)
	if err != nil {
		return page, fetcher.classify(err)
	}
	next := after
	for _, change := range listed.Changes {
		page.Changes = append(page.Changes, sdkgo.TriggerEvent[ItemChanged]{
			// Deterministic: the same change always has the same ID, so a refetch deduplicates.
			ID:         change.ItemID + "@" + strconv.FormatInt(change.Sequence, 10),
			OccurredAt: change.ChangedAt,
			Payload:    ItemChanged{ItemID: change.ItemID, Sequence: change.Sequence, Title: change.Title},
		})
		next = change.Sequence
	}
	page.Next = pollingtrigger.Cursor{Token: strconv.FormatInt(next, 10)}
	page.HasMorePages = listed.HasMore
	return page, nil
}

// classify turns provider statuses into the kit's reset and rate-limit signals.
func (fetcher *changeLogFetcher) classify(err error) error {
	var status *changeLogStatusError
	if !errors.As(err, &status) {
		return err
	}
	switch status.Code {
	case http.StatusGone, http.StatusNotFound:
		return pollingtrigger.MarkCursorInvalid(err)
	case http.StatusTooManyRequests:
		return pollingtrigger.MarkRateLimited(err, providerhttp.ParseRetryAfter(status.RetryAfter, time.Now()))
	default:
		return err
	}
}
