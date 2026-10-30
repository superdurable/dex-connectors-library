// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MemberStatus is Mailchimp's status of an audience contact.
type MemberStatus string

const (
	// MemberStatusSubscribed receives marketing campaigns.
	MemberStatusSubscribed MemberStatus = "subscribed"
	// MemberStatusUnsubscribed opted out of marketing campaigns; Mailchimp keeps the contact so it stays suppressed.
	MemberStatusUnsubscribed MemberStatus = "unsubscribed"
	// MemberStatusCleaned is an address Mailchimp removed from sending after hard bounces.
	MemberStatusCleaned MemberStatus = "cleaned"
	// MemberStatusPending was sent a confirmation email and has not confirmed yet.
	MemberStatusPending MemberStatus = "pending"
	// MemberStatusTransactional receives only transactional email, never marketing campaigns.
	MemberStatusTransactional MemberStatus = "transactional"
	// MemberStatusArchived was archived; Mailchimp returns it on reads but it cannot be written as a status.
	MemberStatusArchived MemberStatus = "archived"
)

const (
	// MaxMergeFields bounds the merge fields one upsertMember sets.
	MaxMergeFields = 30
	// MaxMergeFieldValueBytes bounds one merge field string; Mailchimp truncates text fields at 255 bytes.
	MaxMergeFieldValueBytes  = 1024
	maximumEmailAddressBytes = 254
)

var (
	// mergeTagPattern matches Mailchimp merge tags such as FNAME, ADDRESS, or MMERGE5.
	mergeTagPattern = regexp.MustCompile(`^[A-Z0-9_]{1,50}$`)
	// mergeAddressKeyPattern matches address merge keys such as addr1, city, state, and zip.
	mergeAddressKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	subscriberHashPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Member is one audience contact as Mailchimp returned it.
type Member struct {
	// SubscriberHash is Mailchimp's member id: the MD5 hash of the lowercased email address.
	SubscriberHash string `json:"subscriberHash"`
	// EmailAddress is the contact's email address.
	EmailAddress string `json:"emailAddress"`
	// ContactID is Mailchimp's contact_id, which stays stable when the address changes.
	ContactID string `json:"contactId,omitempty"`
	// FullName is the contact's full name, or empty.
	FullName string `json:"fullName,omitempty"`
	// Status is the contact's current status, such as subscribed or unsubscribed.
	Status MemberStatus `json:"status"`
	// EmailType is html or text, or empty.
	EmailType string `json:"emailType,omitempty"`
	// MergeFields maps merge tags such as FNAME to Mailchimp's stored values: strings, numbers, or
	// address objects.
	MergeFields map[string]any `json:"mergeFields,omitempty"`
	// Tags holds the names of at most 50 of the contact's tags, as Mailchimp returns them.
	Tags []string `json:"tags,omitempty"`
	// TagCount is the contact's total number of tags, which can exceed len(Tags).
	TagCount int `json:"tagCount"`
	// IsVIP reports Mailchimp's VIP flag.
	IsVIP bool `json:"isVip,omitempty"`
	// Language is the contact's language code, or empty.
	Language string `json:"language,omitempty"`
	// ListID is the audience ID.
	ListID string `json:"listId"`
	// WebID is the ID of the contact's page in the Mailchimp web app, or zero.
	WebID int64 `json:"webId,omitempty"`
	// SignedUpAt is when the contact signed up, or nil when Mailchimp has no signup time.
	SignedUpAt *time.Time `json:"signedUpAt,omitempty"`
	// OptedInAt is when the contact confirmed opt-in, or nil.
	OptedInAt *time.Time `json:"optedInAt,omitempty"`
	// LastChangedAt is when the contact's information last changed, or nil.
	LastChangedAt *time.Time `json:"lastChangedAt,omitempty"`
}

type memberWire struct {
	ID              string         `json:"id"`
	EmailAddress    string         `json:"email_address"`
	ContactID       string         `json:"contact_id"`
	FullName        string         `json:"full_name"`
	WebID           int64          `json:"web_id"`
	EmailType       string         `json:"email_type"`
	Status          string         `json:"status"`
	MergeFields     map[string]any `json:"merge_fields"`
	VIP             bool           `json:"vip"`
	Language        string         `json:"language"`
	TimestampSignup string         `json:"timestamp_signup"`
	TimestampOpt    string         `json:"timestamp_opt"`
	LastChanged     string         `json:"last_changed"`
	TagsCount       int            `json:"tags_count"`
	Tags            []struct {
		Name string `json:"name"`
	} `json:"tags"`
	ListID string `json:"list_id"`
}

// SubscriberHash returns the key Mailchimp uses for a contact in an audience: the hexadecimal MD5
// hash of the lowercased email address. MD5 is Mailchimp's addressing scheme, not a security control.
func SubscriberHash(emailAddress string) string {
	digest := md5.Sum([]byte(strings.ToLower(emailAddress)))
	return hex.EncodeToString(digest[:])
}

// IsWritableMemberStatus reports whether upsertMember can write the status. Cleaned is set by
// Mailchimp after bounces, and archived by archiving, so neither is written.
func IsWritableMemberStatus(status MemberStatus) bool {
	switch status {
	case MemberStatusSubscribed, MemberStatusUnsubscribed, MemberStatusPending, MemberStatusTransactional:
		return true
	default:
		return false
	}
}

// IsReadableMemberStatus reports whether the status is one Mailchimp documents on reads and list filters.
func IsReadableMemberStatus(status MemberStatus) bool {
	return IsWritableMemberStatus(status) || status == MemberStatusCleaned || status == MemberStatusArchived
}

func decodeMemberBody(body []byte, expectedHash string) (Member, error) {
	var wire memberWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Member{}, errors.New("the contact is not valid JSON")
	}
	return convertMember(wire, expectedHash)
}

func convertMember(wire memberWire, expectedHash string) (Member, error) {
	if !subscriberHashPattern.MatchString(wire.ID) {
		return Member{}, errors.New("the contact has no subscriber hash")
	}
	if expectedHash != "" && wire.ID != expectedHash {
		return Member{}, errors.New("the contact's subscriber hash does not match the requested address")
	}
	if wire.EmailAddress == "" || wire.Status == "" {
		return Member{}, errors.New("the contact has no email address or status")
	}
	member := Member{
		SubscriberHash: wire.ID, EmailAddress: wire.EmailAddress, ContactID: wire.ContactID, FullName: wire.FullName,
		Status: MemberStatus(wire.Status), EmailType: wire.EmailType, MergeFields: wire.MergeFields,
		TagCount: wire.TagsCount, IsVIP: wire.VIP, Language: wire.Language, ListID: wire.ListID, WebID: wire.WebID,
	}
	for _, tag := range wire.Tags {
		member.Tags = append(member.Tags, tag.Name)
	}
	var err error
	if member.SignedUpAt, err = parseMailchimpTime(wire.TimestampSignup); err != nil {
		return Member{}, err
	}
	if member.OptedInAt, err = parseMailchimpTime(wire.TimestampOpt); err != nil {
		return Member{}, err
	}
	if member.LastChangedAt, err = parseMailchimpTime(wire.LastChanged); err != nil {
		return Member{}, err
	}
	return member, nil
}

// parseMailchimpTime reads an ISO 8601 timestamp; Mailchimp sends an empty string for an unset time.
func parseMailchimpTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("a timestamp is not ISO 8601")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

// formatMailchimpTime writes the ISO 8601 form Mailchimp's filter documentation shows, such as 2015-10-21T15:41:36+00:00.
func formatMailchimpTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05") + "+00:00"
}

func validateEmailAddress(name string, value string) error {
	if value == "" || len(value) > maximumEmailAddressBytes {
		return fmt.Errorf("%s must be one email address of at most %d bytes", name, maximumEmailAddressBytes)
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return fmt.Errorf("%s must be one bare email address such as jane@example.com", name)
	}
	return nil
}

// validateMergeFields accepts Mailchimp's documented shapes: a string, a number, or a string-valued address object.
func validateMergeFields(fields map[string]any) error {
	if len(fields) > MaxMergeFields {
		return fmt.Errorf("mergeFields holds at most %d fields", MaxMergeFields)
	}
	for tag, value := range fields {
		if !mergeTagPattern.MatchString(tag) {
			return fmt.Errorf("merge tag %q must be uppercase letters, digits, or underscores, such as FNAME", tag)
		}
		if err := validateMergeFieldValue(tag, value); err != nil {
			return err
		}
	}
	return nil
}

func validateMergeFieldValue(tag string, value any) error {
	switch typed := value.(type) {
	case string:
		return validateMergeFieldText(tag, typed)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Errorf("merge field %s must be a finite number", tag)
		}
		return nil
	case int, int32, int64:
		return nil
	case json.Number:
		if _, err := typed.Float64(); err != nil {
			return fmt.Errorf("merge field %s must be a finite number", tag)
		}
		return nil
	case map[string]any:
		for key, nested := range typed {
			text, isText := nested.(string)
			if !isText || !mergeAddressKeyPattern.MatchString(key) {
				return fmt.Errorf("merge field %s must be an address object of string values such as addr1 and city", tag)
			}
			if err := validateMergeFieldText(tag, text); err != nil {
				return err
			}
		}
		return nil
	case map[string]string:
		for key, text := range typed {
			if !mergeAddressKeyPattern.MatchString(key) {
				return fmt.Errorf("merge field %s must be an address object such as addr1 and city", tag)
			}
			if err := validateMergeFieldText(tag, text); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("merge field %s must be a string, a number, or an address object", tag)
	}
}

func validateMergeFieldText(tag string, value string) error {
	if !utf8.ValidString(value) || len(value) > MaxMergeFieldValueBytes {
		return fmt.Errorf("merge field %s must be valid UTF-8 of at most %d bytes", tag, MaxMergeFieldValueBytes)
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return fmt.Errorf("merge field %s cannot contain control characters", tag)
		}
	}
	return nil
}
