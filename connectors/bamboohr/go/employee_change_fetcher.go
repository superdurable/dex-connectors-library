// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	employeeChangedTriggerName = "employeeChanged"
	// employeeChangedCursorVersion versions the stored employeeChanged cursor token.
	employeeChangedCursorVersion = 1
	// employeeChangeSettleWindow is how far behind BambooHR's Date header a change must be before it is
	// delivered, covering changes that become visible after a later second was already listed and any
	// difference between the clock that stamps lastChanged and the one that sends Date.
	employeeChangeSettleWindow = 30 * time.Second
)

// employeeChangeFetcher reads BambooHR's changed-employees list for the poll Trigger kit.
type employeeChangeFetcher struct {
	client     *Client
	call       sdkgo.Call
	changeType EmployeeChangeType
}

// employeeChangedCursor is the committed Trigger cursor: listEmployeeChanges' position plus the change
// type it was read with, so a binding whose change type changed starts again instead of mixing filters.
type employeeChangedCursor struct {
	Version         int                `json:"v"`
	Since           time.Time          `json:"since"`
	AfterEmployeeID string             `json:"afterEmployeeId,omitempty"`
	ChangeType      EmployeeChangeType `json:"changeType,omitempty"`
}

func newEmployeeChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration EmployeeChangedTriggerConfiguration) *employeeChangeFetcher {
	return &employeeChangeFetcher{
		client:     client,
		call:       pollingtrigger.NewTriggerCredentialCall(connection, EmployeeChangedTriggerDefinition.Trigger),
		changeType: configuration.ChangeType,
	}
}

// SeedCursor starts the cursor at BambooHR's clock, read from the response Date header, so a local clock
// that runs ahead of BambooHR cannot skip changes; without a usable Date header it uses the local clock.
// No change before that second is delivered; a change BambooHR stamps with that second itself may be,
// because it may have been made after the binding started. The request also proves the API key works.
func (fetcher *employeeChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	response, err := fetcher.listChangedEmployees(ctx, fetcher.client.now())
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	if _, err := decodeEmployeeChanges(response.body); err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("BambooHR returned an invalid change list: %w", err)
	}
	return fetcher.encodeCursor(employeeChangedCursor{Since: fetcher.bambooHRTime(response), ChangeType: fetcher.changeType})
}

// FetchChanges reads every change after cursor and returns the oldest pollingtrigger.MaxChangesPerPage
// of them, ordered by lastChanged and then numeric employee ID. BambooHR does not paginate this list, so
// the remainder is read again by the immediate next fetch. BambooHR stamps lastChanged in whole seconds
// and does not document when a change becomes visible, so only changes at least
// employeeChangeSettleWindow older than BambooHR's Date header are delivered: a change stamped with a
// second the cursor already passed, such as a lower employee ID in the same second, would otherwise be
// lost. Newer changes wait for a later poll. A 400, which BambooHR documents for an
// invalid since, an undecodable cursor, or a cursor read with another change type is marked invalid.
func (fetcher *employeeChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[EmployeeChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[EmployeeChangedEvent]]
	position, err := decodeEmployeeChangedCursor(cursor.Token)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	if position.ChangeType != fetcher.changeType {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("BambooHR employeeChanged cursor was read with another changeType"))
	}
	response, err := fetcher.listChangedEmployees(ctx, position.Since)
	if err != nil {
		return page, err
	}
	changes, err := decodeEmployeeChanges(response.body)
	if err != nil {
		return page, fmt.Errorf("BambooHR returned an invalid change list: %w", err)
	}
	settledThrough := fetcher.bambooHRTime(response).Add(-employeeChangeSettleWindow)
	settled := changes[:0]
	for _, change := range changes {
		if !change.LastChanged.After(settledThrough) {
			settled = append(settled, change)
		}
	}
	listed := pageEmployeeChanges(settled, EmployeeChangeCursor{Since: position.Since, AfterEmployeeID: position.AfterEmployeeID}, pollingtrigger.MaxChangesPerPage)
	for _, change := range listed.Changes {
		page.Changes = append(page.Changes, newEmployeeChangedTriggerEvent(change))
	}
	page.HasMorePages = listed.HasMore
	page.Next, err = fetcher.encodeCursor(employeeChangedCursor{
		Since: listed.NextCursor.Since, AfterEmployeeID: listed.NextCursor.AfterEmployeeID, ChangeType: fetcher.changeType,
	})
	return page, err
}

// listChangedEmployees sends GET /employees/changed one second before since, as listEmployeeChanges does,
// because BambooHR does not document whether since is inclusive.
func (fetcher *employeeChangeFetcher) listChangedEmployees(ctx context.Context, since time.Time) (bambooHRResponse, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return bambooHRResponse{}, err
	}
	credentials, failure := fetcher.client.resolveCredentials(fetcher.call, employeeChangedTriggerName)
	if failure != nil {
		return bambooHRResponse{}, errors.New("BambooHR employeeChanged poll: " + failure.Message)
	}
	query := url.Values{"since": {since.UTC().Add(-changeQueryOverlap).Truncate(time.Second).Format(time.RFC3339)}}
	if fetcher.changeType != "" {
		query.Set("type", string(fetcher.changeType))
	}
	result := fetcher.client.exchangeWithContext(ctx, credentials, employeeChangedTriggerName,
		bambooHRRequest{method: http.MethodGet, path: "/employees/changed", query: query}, false)
	failure = &result.failure
	switch {
	case result.outcome == exchangeSucceeded:
		return result.response, nil
	case result.outcome == exchangeRateLimited, result.response.statusCode == http.StatusServiceUnavailable:
		// BambooHR throttles per company and API key, so a 429 or 503 pauses every binding of the connection.
		return bambooHRResponse{}, pollingtrigger.MarkRateLimited(errors.New(failure.Message), result.retryAfter)
	case result.response.statusCode == http.StatusBadRequest:
		return bambooHRResponse{}, pollingtrigger.MarkCursorInvalid(errors.New(failure.Message))
	case failure.Kind == sdkgo.FailureResponseTooLarge:
		return bambooHRResponse{}, errors.New("BambooHR changed-employees list exceeds maxResponseBytes; raise maxResponseBytes on the connection")
	default:
		return bambooHRResponse{}, errors.New("BambooHR employeeChanged poll: " + failure.Message)
	}
}

// bambooHRTime is BambooHR's clock from the response Date header, or the local clock without one, in
// whole seconds as lastChanged is.
func (fetcher *employeeChangeFetcher) bambooHRTime(response bambooHRResponse) time.Time {
	if serverNow, err := http.ParseTime(response.header.Get("Date")); err == nil && !serverNow.IsZero() {
		return serverNow.UTC().Truncate(time.Second)
	}
	return fetcher.client.now().UTC().Truncate(time.Second)
}

func (fetcher *employeeChangeFetcher) encodeCursor(position employeeChangedCursor) (pollingtrigger.Cursor, error) {
	position.Version = employeeChangedCursorVersion
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode BambooHR employeeChanged cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

func decodeEmployeeChangedCursor(encoded string) (employeeChangedCursor, error) {
	var position employeeChangedCursor
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&position); err != nil || position.Version != employeeChangedCursorVersion || position.Since.IsZero() {
		return position, errEmployeeChangedCursorUndecodable
	}
	if position.AfterEmployeeID != "" && validateEmployeeID("afterEmployeeId", position.AfterEmployeeID) != nil {
		return position, errEmployeeChangedCursorUndecodable
	}
	if (EmployeeChangedTriggerConfiguration{ChangeType: position.ChangeType}).Validate() != nil {
		return position, errEmployeeChangedCursorUndecodable
	}
	return position, nil
}
