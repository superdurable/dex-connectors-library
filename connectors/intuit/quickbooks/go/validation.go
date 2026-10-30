// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	calendarDateLayout = "2006-01-02"
	// maximumEmailAddressBytes is the RFC 5321 path limit.
	maximumEmailAddressBytes = 254
	// MaxDocNumberCharacters is QuickBooks's DocNumber limit.
	MaxDocNumberCharacters = 21
	// MaxDisplayNameCharacters is QuickBooks's Customer DisplayName limit.
	MaxDisplayNameCharacters = 500
)

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

var errBlankDisplayName = errors.New("displayName is required")

// validateCalendarDate checks a YYYY-MM-DD date; the error names the field, never the value.
func validateCalendarDate(fieldName string, value string) error {
	if _, err := time.Parse(calendarDateLayout, value); err != nil {
		return fmt.Errorf("%s must be a calendar date written YYYY-MM-DD, such as 2026-10-01", fieldName)
	}
	return nil
}

// validateOptionalCalendarDate checks a date that may be blank.
func validateOptionalCalendarDate(fieldName string, value string) error {
	if value == "" {
		return nil
	}
	return validateCalendarDate(fieldName, value)
}

// validateEntityID checks a required QuickBooks Id such as 145.
func validateEntityID(fieldName string, value string) error {
	if !entityIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a QuickBooks Id, a decimal string such as 145", fieldName)
	}
	return nil
}

// validateOptionalEntityID checks a QuickBooks Id that may be blank.
func validateOptionalEntityID(fieldName string, value string) error {
	if value == "" {
		return nil
	}
	return validateEntityID(fieldName, value)
}

// validateCurrencyCode checks an optional ISO 4217 code.
func validateCurrencyCode(fieldName string, value string) error {
	if value != "" && !currencyCodePattern.MatchString(value) {
		return fmt.Errorf("%s must be a three-letter ISO 4217 code such as USD", fieldName)
	}
	return nil
}

// validateText checks optional text: valid UTF-8, bounded, and on one line unless multiline is allowed.
func validateText(fieldName string, value string, maximumCharacters int, isMultiline bool) error {
	switch {
	case !utf8.ValidString(value):
		return fmt.Errorf("%s must be valid UTF-8", fieldName)
	case utf8.RuneCountInString(value) > maximumCharacters:
		return fmt.Errorf("%s can be at most %d characters", fieldName, maximumCharacters)
	}
	for _, character := range value {
		isLineBreak := character == '\n' || character == '\r' || character == '\t'
		if (character < ' ' || character == 0x7f) && !(isMultiline && isLineBreak) {
			return fmt.Errorf("%s must not contain control characters", fieldName)
		}
	}
	return nil
}

// validateEmailAddress accepts one bare address such as jane@example.com.
func validateEmailAddress(fieldName string, value string) error {
	address, err := mail.ParseAddress(value)
	switch {
	case value == "":
		return fmt.Errorf("%s is required", fieldName)
	case len(value) > maximumEmailAddressBytes:
		return fmt.Errorf("%s is longer than 254 bytes", fieldName)
	case err != nil || address.Name != "" || address.Address != value || strings.ContainsAny(value, " \"\\'()<>,;"):
		return fmt.Errorf("%s must be one bare email address such as jane@example.com", fieldName)
	}
	return nil
}

// validateOptionalEmailAddress checks an email address that may be blank.
func validateOptionalEmailAddress(fieldName string, value string) error {
	if value == "" {
		return nil
	}
	return validateEmailAddress(fieldName, value)
}

// validateQueryLiteral checks text for a quoted query literal; backslashes are refused because they escape quotes.
func validateQueryLiteral(fieldName string, value string, maximumCharacters int) error {
	if err := validateText(fieldName, value, maximumCharacters, false); err != nil {
		return err
	}
	if strings.Contains(value, `\`) {
		return fmt.Errorf("%s cannot contain a backslash", fieldName)
	}
	return nil
}

// quoteQueryLiteral single-quotes a validated value, escaping each apostrophe with a backslash.
func quoteQueryLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `\'`) + "'"
}
