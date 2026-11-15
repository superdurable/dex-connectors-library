// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MemberChangedTriggerConfiguration is the memberChanged binding configuration that the audienceIdEntry
// and memberStatusPicker units save in Dex Web. Changing the audience of a binding that already polled
// makes its next poll record a new baseline for the new audience.
type MemberChangedTriggerConfiguration struct {
	// AudienceID is the watched audience, Mailchimp's list_id, such as 57afe96172. It is required.
	AudienceID string `json:"audienceId"`
	// Statuses keeps only contacts whose status after the change is one of these, such as subscribed;
	// empty delivers changed contacts in every status. A filtered-out change is still passed by the cursor.
	Statuses []MemberStatus `json:"statuses,omitempty"`
}

// Validate requires a Mailchimp audience ID and known, distinct statuses.
func (configuration MemberChangedTriggerConfiguration) Validate() error {
	if err := validateResourceID("audienceId", configuration.AudienceID); err != nil {
		return err
	}
	for index, status := range configuration.Statuses {
		if !IsReadableMemberStatus(status) {
			return fmt.Errorf("statuses[%d] %q is not a Mailchimp contact status", index, status)
		}
		if slices.Contains(configuration.Statuses[:index], status) {
			return fmt.Errorf("statuses lists %q twice", status)
		}
	}
	return nil
}

// accepts reports whether a contact in status passes the binding's status filter.
func (configuration MemberChangedTriggerConfiguration) accepts(status MemberStatus) bool {
	return len(configuration.Statuses) == 0 || slices.Contains(configuration.Statuses, status)
}

// MemberStatuses returns every status the memberStatusPicker offers, in its display order.
func MemberStatuses() []MemberStatus {
	return []MemberStatus{
		MemberStatusSubscribed, MemberStatusUnsubscribed, MemberStatusCleaned,
		MemberStatusPending, MemberStatusTransactional, MemberStatusArchived,
	}
}

// MemberChangedEvent is one contact added to or changed in the watched audience. Mailchimp reports no
// change type, so a new contact and an edited one look alike; Member.SignedUpAt, Member.OptedInAt, and
// Member.Status tell them apart where the application needs it. A contact that changed several times
// between two polls is delivered once, in its latest state.
type MemberChangedEvent struct {
	// AudienceID is the watched audience.
	AudienceID string `json:"audienceId"`
	// Member is the contact as Mailchimp listed it after the change; Member.LastChangedAt is the change time.
	Member Member `json:"member"`
}

// newMemberChangedTriggerEvent converts one listed contact. Its ID is a pure function of the contact
// version, so a refetch after a crash yields the same ID.
func newMemberChangedTriggerEvent(audienceID string, member Member) sdkgo.TriggerEvent[MemberChangedEvent] {
	return sdkgo.TriggerEvent[MemberChangedEvent]{
		ID:         memberChangeID(audienceID, member),
		OccurredAt: member.LastChangedAt.UTC(),
		Payload:    MemberChangedEvent{AudienceID: audienceID, Member: member},
	}
}

// memberChangeID hashes the audience and the listed contact version: its subscriber hash, change time, and
// every listed field. Two changes in one change second that leave different content get different IDs.
func memberChangeID(audienceID string, member Member) string {
	return "mailchimp-" + memberVersionDigest(audienceID, member, 16)
}

func memberVersionDigest(audienceID string, member Member, bytes int) string {
	canonical := member
	canonical.Tags = slices.Sorted(slices.Values(member.Tags)) // Tag order is not part of the version.
	if canonical.LastChangedAt != nil {
		changedAt := canonical.LastChangedAt.UTC()
		canonical.LastChangedAt = &changedAt
	}
	// Struct fields encode in declaration order and map keys sorted, so equal versions encode equally.
	encoded, err := json.Marshal(canonical)
	if err != nil {
		encoded = []byte(member.SubscriberHash + "\x00" + member.LastChangedAt.UTC().Format(time.RFC3339) + "\x00" + string(member.Status))
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{audienceID, string(encoded)}, "\x00")))
	return hex.EncodeToString(digest[:bytes])
}

// memberSnapshotItem keys a contact's delivered version, so rereading the lookback window delivers nothing twice.
type memberSnapshotItem struct {
	audienceID string
	member     Member
}

func (item memberSnapshotItem) SnapshotKey() string { return item.member.SubscriberHash }

func (item memberSnapshotItem) SnapshotHash() string {
	return memberVersionDigest(item.audienceID, item.member, 8)
}

func (memberSnapshotItem) SnapshotValue() json.RawMessage { return nil }

func memberChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("mailchimp memberChanged configuration: %w", err)
}
