// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// recordChangedCursorVersion versions the stored recordChanged cursor token.
	recordChangedCursorVersion = 2
	// recordChangedOverlap is how far before the latest delivered change a poll still looks for late-visible changes.
	recordChangedOverlap = 5 * time.Second
	// maximumRecordChangedSeen bounds the delivered records the cursor remembers inside the overlap.
	maximumRecordChangedSeen = 2000
)

// recordChangedCursorToken is the JSON position stored in the poll cursor.
type recordChangedCursorToken struct {
	Version int    `json:"v"`
	FieldID string `json:"fieldId"`
	// After is the latest delivered (or, at seed, existing) modification time, blank before any.
	After string `json:"after"`
	// Floor, when set, hides records modified before it even inside the overlap.
	Floor string `json:"floor,omitempty"`
	// Burst is a time whose remembered records were dropped; Floor passes it once the scan lists past it.
	Burst string `json:"burst,omitempty"`
	// Seen lists delivered records inside the overlap as "<recordId>:<milliseconds before After>".
	Seen []string `json:"seen,omitempty"`
	// Scan is the open listRecords iteration, so a long scan resumes instead of starting again.
	Scan *recordChangedScan `json:"scan,omitempty"`
}

// recordChangedScan is one listRecords iteration: its filter bound and the next page's offset.
type recordChangedScan struct {
	Since  string `json:"since"`
	Offset string `json:"offset"`
}

// seenRecord is one delivered record and its modification time, truncated to Airtable's milliseconds.
type seenRecord struct {
	id string
	at time.Time
}

// recordChangedPosition is the decoded cursor.
type recordChangedPosition struct {
	fieldID             string
	after, floor, burst time.Time
	seen                []seenRecord
	scan                *recordChangedScan
}

// lowerBound is the earliest modification time a poll still delivers; zero means every record.
func (position *recordChangedPosition) lowerBound() time.Time {
	if position.after.IsZero() {
		return time.Time{}
	}
	return later(position.after.Add(-recordChangedOverlap), position.floor)
}

// isDelivered reports a record that is older than the lower bound or was delivered at this time.
func (position *recordChangedPosition) isDelivered(changed changedRecord) bool {
	if changed.modifiedAt.Before(position.lowerBound()) {
		return true
	}
	at := changed.modifiedAt.Truncate(time.Millisecond)
	return slices.ContainsFunc(position.seen, func(seen seenRecord) bool { return seen.id == changed.record.ID && seen.at.Equal(at) })
}

// deliver remembers one delivered record. Records arrive oldest first within a scan.
func (position *recordChangedPosition) deliver(changed changedRecord) {
	if !position.burst.IsZero() && changed.modifiedAt.After(position.burst) {
		position.closeBurst()
	}
	position.after = later(position.after, changed.modifiedAt)
	position.seen = append(position.seen, seenRecord{id: changed.record.ID, at: changed.modifiedAt.Truncate(time.Millisecond)})
	position.bound()
}

// closeBurst moves the floor past a burst time once every record at that time was listed.
func (position *recordChangedPosition) closeBurst() {
	if !position.burst.IsZero() {
		position.floor = later(position.floor, position.burst.Add(time.Millisecond))
		position.burst = time.Time{}
	}
	position.bound()
}

// bound forgets records below the lower bound and, past maximumRecordChangedSeen, every record at the oldest times.
func (position *recordChangedPosition) bound() {
	lowerBound := position.lowerBound()
	position.seen = slices.DeleteFunc(position.seen, func(seen seenRecord) bool { return seen.at.Before(lowerBound) })
	if len(position.seen) <= maximumRecordChangedSeen {
		return
	}
	slices.SortStableFunc(position.seen, func(left, right seenRecord) int { return left.at.Compare(right.at) })
	dropped := position.seen[len(position.seen)-maximumRecordChangedSeen-1].at
	position.seen = slices.DeleteFunc(position.seen, func(seen seenRecord) bool { return !seen.at.After(dropped) })
	if dropped.Before(position.after.Truncate(time.Millisecond)) {
		// The scan, oldest first, already listed every record at the dropped time.
		position.floor = later(position.floor, dropped.Add(time.Millisecond))
		return
	}
	position.burst = later(position.burst, dropped)
}

func later(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func encodeRecordChangedCursor(position recordChangedPosition) (pollingtrigger.Cursor, error) {
	token := recordChangedCursorToken{
		Version: recordChangedCursorVersion, FieldID: position.fieldID, Scan: position.scan,
		After: formatCursorTime(position.after), Floor: formatCursorTime(position.floor), Burst: formatCursorTime(position.burst),
	}
	for _, seen := range position.seen {
		token.Seen = append(token.Seen, seen.id+":"+strconv.FormatInt(position.after.Truncate(time.Millisecond).Sub(seen.at).Milliseconds(), 10))
	}
	encoded, err := json.Marshal(token)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

// decodeRecordChangedCursor validates a stored token; every error means the cursor must be reseeded.
func decodeRecordChangedCursor(encoded string) (recordChangedPosition, error) {
	var token recordChangedCursorToken
	if err := json.Unmarshal([]byte(encoded), &token); err != nil || token.Version != recordChangedCursorVersion {
		return recordChangedPosition{}, errors.New("Airtable recordChanged cursor is not a version 2 cursor")
	}
	if len(token.Seen) > maximumRecordChangedSeen {
		return recordChangedPosition{}, errors.New("Airtable recordChanged cursor holds too many record IDs")
	}
	position := recordChangedPosition{fieldID: token.FieldID, scan: token.Scan}
	var err error
	for target, text := range map[*time.Time]string{&position.after: token.After, &position.floor: token.Floor, &position.burst: token.Burst} {
		if *target, err = parseCursorTime(text); err != nil {
			return recordChangedPosition{}, err
		}
	}
	if position.after.IsZero() && (len(token.Seen) > 0 || !position.floor.IsZero() || !position.burst.IsZero()) {
		return recordChangedPosition{}, errors.New("Airtable recordChanged cursor remembers records without a time")
	}
	if token.Scan != nil {
		if _, err := parseCursorTime(token.Scan.Since); err != nil || token.Scan.Since == "" || token.Scan.Offset == "" {
			return recordChangedPosition{}, errors.New("Airtable recordChanged cursor scan is invalid")
		}
	}
	for _, entry := range token.Seen {
		recordID, delta, isPair := strings.Cut(entry, ":")
		milliseconds, err := strconv.ParseInt(delta, 10, 64)
		if !isPair || err != nil || milliseconds < 0 || !recordIDPattern.MatchString(recordID) {
			return recordChangedPosition{}, errors.New("Airtable recordChanged cursor holds an invalid record entry")
		}
		at := position.after.Truncate(time.Millisecond).Add(-time.Duration(milliseconds) * time.Millisecond)
		position.seen = append(position.seen, seenRecord{id: recordID, at: at})
	}
	return position, nil
}

func formatCursorTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func parseCursorTime(text string) (time.Time, error) {
	if text == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, errors.New("Airtable recordChanged cursor time is invalid")
	}
	return at.UTC(), nil
}
