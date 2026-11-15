// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// MeetingChangeKind is the kind of one meetingChanged delivery. Zoom's meeting list reports no change
// type or modification time, so the kind comes from comparing two polls of the list.
type MeetingChangeKind string

const (
	// MeetingChangeScheduled is a meeting the previous poll did not list, such as a newly scheduled meeting.
	MeetingChangeScheduled MeetingChangeKind = "scheduled"
	// MeetingChangeUpdated is a listed meeting whose topic, type, start, duration, time zone, or listed
	// agenda changed, such as a reschedule. A recurring meeting whose listed start moves to its next
	// occurrence also arrives as updated.
	MeetingChangeUpdated MeetingChangeKind = "updated"
	// MeetingChangeRemoved is a meeting the list no longer contains: it was deleted, or it expired, which
	// Zoom does after a meeting's scheduled time has passed.
	MeetingChangeRemoved MeetingChangeKind = "removed"
)

// MeetingChangedTriggerConfiguration is the meetingChanged binding configuration. The binding always
// watches the authorized user's own scheduled meetings, so Dex Web shows no field for it; an application
// may narrow the delivered kinds in code.
type MeetingChangedTriggerConfiguration struct {
	// ChangeKinds limits deliveries to these kinds; empty delivers scheduled, updated, and removed.
	// Skipped kinds still advance the stored snapshot, so they are never delivered later.
	ChangeKinds []MeetingChangeKind `json:"changeKinds,omitempty"`
}

// Validate rejects an unknown or repeated change kind.
func (configuration MeetingChangedTriggerConfiguration) Validate() error {
	seen := map[MeetingChangeKind]bool{}
	for _, kind := range configuration.ChangeKinds {
		switch kind {
		case MeetingChangeScheduled, MeetingChangeUpdated, MeetingChangeRemoved:
		default:
			return fmt.Errorf("meetingChanged changeKinds entry %q must be scheduled, updated, or removed", kind)
		}
		if seen[kind] {
			return fmt.Errorf("meetingChanged changeKinds repeats %q", kind)
		}
		seen[kind] = true
	}
	return nil
}

func (configuration MeetingChangedTriggerConfiguration) delivers(kind MeetingChangeKind) bool {
	if len(configuration.ChangeKinds) == 0 {
		return true
	}
	for _, configured := range configuration.ChangeKinds {
		if configured == kind {
			return true
		}
	}
	return false
}

// MeetingChangedEvent is one meeting scheduled, updated, or removed between two polls of the authorized
// user's scheduled meetings.
type MeetingChangedEvent struct {
	// ChangeKind is the derived change kind; see MeetingChangeKind.
	ChangeKind MeetingChangeKind `json:"changeKind"`
	// Meeting is the meeting as this poll listed it. For a removed meeting it is the last listed state,
	// without UUID or JoinURL, which the stored snapshot never keeps.
	Meeting MeetingSummary `json:"meeting"`
	// PreviousMeeting is the state the previous poll listed, without UUID or JoinURL; set only for updated.
	PreviousMeeting *MeetingSummary `json:"previousMeeting,omitempty"`
}

// trackedMeeting is what the snapshot keeps; it omits the per-instance UUID and the passcode-bearing join URL.
type trackedMeeting struct {
	ID              int64      `json:"id"`
	HostID          string     `json:"hostId,omitempty"`
	Topic           string     `json:"topic"`
	Type            int        `json:"type,omitempty"`
	StartTime       *time.Time `json:"startTime,omitempty"`
	DurationMinutes int        `json:"durationMinutes,omitempty"`
	TimeZone        string     `json:"timeZone,omitempty"`
	Agenda          string     `json:"agenda,omitempty"`
	CreatedAt       *time.Time `json:"createdAt,omitempty"`
}

// listedMeeting adapts one listed meeting to the kit's keyed snapshot.
type listedMeeting struct {
	summary MeetingSummary
	value   json.RawMessage
}

func newListedMeeting(summary MeetingSummary) (listedMeeting, error) {
	value, err := json.Marshal(trackedMeeting{
		ID: summary.ID, HostID: summary.HostID, Topic: summary.Topic, Type: summary.Type, StartTime: utcTime(summary.StartTime),
		DurationMinutes: summary.DurationMinutes, TimeZone: summary.TimeZone, Agenda: summary.Agenda, CreatedAt: utcTime(summary.CreatedAt),
	})
	if err != nil {
		return listedMeeting{}, fmt.Errorf("encode listed Zoom meeting: %w", err)
	}
	return listedMeeting{summary: summary, value: value}, nil
}

// SnapshotKey is the decimal meeting ID.
func (meeting listedMeeting) SnapshotKey() string { return strconv.FormatInt(meeting.summary.ID, 10) }

// SnapshotHash is the first 16 bytes of SHA-256 over the tracked fields.
func (meeting listedMeeting) SnapshotHash() string {
	digest := sha256.Sum256(meeting.value)
	return hex.EncodeToString(digest[:16])
}

// listsBefore orders two listings of one meeting ID: the earlier start, then the smaller hash.
func (meeting listedMeeting) listsBefore(other listedMeeting) bool {
	start, otherStart := meeting.summary.StartTime, other.summary.StartTime
	switch {
	case start != nil && otherStart != nil && !start.Equal(*otherStart):
		return start.Before(*otherStart)
	case (start == nil) != (otherStart == nil):
		return start != nil
	}
	return meeting.SnapshotHash() < other.SnapshotHash()
}

// SnapshotValue is the tracked JSON, so a removal or update carries the last listed state.
func (meeting listedMeeting) SnapshotValue() json.RawMessage { return meeting.value }

// decodeTrackedMeeting reads a stored snapshot value back as a summary.
func decodeTrackedMeeting(entry projectconfig.KeyedSnapshotEntry) (MeetingSummary, error) {
	var tracked trackedMeeting
	if err := json.Unmarshal(entry.Value, &tracked); err != nil || tracked.ID < 1 || strconv.FormatInt(tracked.ID, 10) != entry.Key {
		return MeetingSummary{}, errors.New("stored Zoom meeting snapshot entry is invalid")
	}
	return MeetingSummary{
		ID: tracked.ID, HostID: tracked.HostID, Topic: tracked.Topic, Type: tracked.Type, StartTime: tracked.StartTime,
		DurationMinutes: tracked.DurationMinutes, TimeZone: tracked.TimeZone, Agenda: tracked.Agenda, CreatedAt: tracked.CreatedAt,
	}, nil
}

// meetingChangeID includes the previous state's commit, so a meeting changed back and forth gets new IDs.
// A scheduled change has no previous state, so its ID includes the cursor's change sequence instead.
func meetingChangeID(kind MeetingChangeKind, key string, previous *projectconfig.KeyedSnapshotEntry, currentHash string, sequence int64) string {
	parts := []string{string(kind), key, currentHash}
	if previous != nil {
		parts = append(parts, previous.Hash, strconv.FormatInt(previous.Seen, 10))
	} else if sequence > 0 {
		parts = append(parts, strconv.FormatInt(sequence, 10))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "zoom-meeting-" + hex.EncodeToString(digest[:16])
}

func newMeetingChangedTriggerEvent(kind MeetingChangeKind, key string, previous *projectconfig.KeyedSnapshotEntry, currentHash string, sequence int64, payload MeetingChangedEvent, observedAt time.Time) sdkgo.TriggerEvent[MeetingChangedEvent] {
	occurredAt := observedAt.UTC()
	if kind == MeetingChangeScheduled && payload.Meeting.CreatedAt != nil {
		occurredAt = payload.Meeting.CreatedAt.UTC()
	}
	return sdkgo.TriggerEvent[MeetingChangedEvent]{
		ID: meetingChangeID(kind, key, previous, currentHash, sequence), OccurredAt: occurredAt, Payload: payload,
	}
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := value.UTC()
	return &converted
}

func meetingChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("zoom meetingChanged configuration: %w", err)
}
