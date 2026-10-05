// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"
)

const maximumEmailAddressBytes = 254

// serviceDeskPage is the /rest/servicedeskapi paging envelope: start, limit, size, isLastPage, and values.
type serviceDeskPage[T any] struct {
	Start      int  `json:"start"`
	Size       int  `json:"size"`
	IsLastPage bool `json:"isLastPage"`
	Values     []T  `json:"values"`
}

// serviceDeskDate is the servicedeskapi date object; epochMillis is authoritative and iso8601 a fallback.
type serviceDeskDate struct {
	EpochMillis *int64 `json:"epochMillis"`
	ISO8601     string `json:"iso8601"`
}

// serviceDeskDuration is the servicedeskapi duration object in milliseconds.
type serviceDeskDuration struct {
	Millis int64 `json:"millis"`
}

// serviceDeskUser is the servicedeskapi user object; the email address is read only to match a lookup.
type serviceDeskUser struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       *bool  `json:"active"`
}

// decodeServiceDeskPage decodes one paging envelope and rejects a body that is not one.
func decodeServiceDeskPage[T any](body []byte) (serviceDeskPage[T], error) {
	var envelope struct {
		Values json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Values) == 0 || string(envelope.Values) == "null" {
		return serviceDeskPage[T]{}, errors.New("page has no values list")
	}
	var page serviceDeskPage[T]
	if err := json.Unmarshal(body, &page); err != nil {
		return serviceDeskPage[T]{}, errors.New("page values are invalid")
	}
	return page, nil
}

// time returns the date in UTC, or false when the provider sent neither epochMillis nor a valid iso8601.
func (date *serviceDeskDate) time() (time.Time, bool) {
	if date == nil {
		return time.Time{}, false
	}
	if date.EpochMillis != nil {
		return time.UnixMilli(*date.EpochMillis).UTC(), true
	}
	if parsed, err := parseJiraTime(date.ISO8601); err == nil && !parsed.IsZero() {
		return parsed, true
	}
	return time.Time{}, false
}

// timePointer returns the date as a pointer, or nil when it is absent.
func (date *serviceDeskDate) timePointer() *time.Time {
	value, isPresent := date.time()
	if !isPresent {
		return nil
	}
	return &value
}

func (duration *serviceDeskDuration) milliseconds() int64 {
	if duration == nil {
		return 0
	}
	return duration.Millis
}

func (user *serviceDeskUser) view() *AccountReference {
	if user == nil || !accountIDPattern.MatchString(user.AccountID) {
		return nil
	}
	return &AccountReference{AccountID: user.AccountID, DisplayName: user.DisplayName}
}

// validateEmailAddress accepts one bare address such as jane@example.com, without a display name.
func validateEmailAddress(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Name != "" || parsed.Address != trimmed || len(trimmed) > maximumEmailAddressBytes {
		return "", errors.New("email must be one bare address such as jane@example.com")
	}
	return trimmed, nil
}
