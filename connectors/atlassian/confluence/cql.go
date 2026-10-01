// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maximumFilterValues            = 50
	maximumCQLValueCharacters      = 1024
	maximumAdditionalCQLCharacters = 4096
	// modifiedSinceTimeZoneMargin covers every time zone Confluence may read a CQL date in, UTC-12 to UTC+14.
	modifiedSinceTimeZoneMargin = 12 * time.Hour
)

var (
	spaceKeyPattern = regexp.MustCompile(`^~?[A-Za-z0-9_-]{1,255}$`)
	orderByPattern  = regexp.MustCompile(`(?i)\border\s+by\b`)
)

// PageSearchOrder selects the ORDER BY clause of a filter's CQL.
type PageSearchOrder string

const (
	// PageSearchOrderLastModifiedDescending lists the most recently changed pages first. It is the default.
	PageSearchOrderLastModifiedDescending PageSearchOrder = "lastModifiedDescending"
	// PageSearchOrderCreatedDescending lists the newest pages first.
	PageSearchOrderCreatedDescending PageSearchOrder = "createdDescending"
	// PageSearchOrderTitleAscending lists pages alphabetically by title.
	PageSearchOrderTitleAscending PageSearchOrder = "titleAscending"
)

// PageSearchFilter is a typed page search that the connector turns into escaped CQL, so no caller text
// can change the query's structure. The query always starts with type = page, and every set field adds
// one clause joined with AND.
type PageSearchFilter struct {
	// SpaceKeys limits the search to these space keys, such as OPS or ~5b10ac8d82e05b22cc7d4ef5.
	SpaceKeys []string `json:"spaceKeys,omitempty"`
	// TitlePhrase matches pages whose title contains this phrase as Confluence's text search tokenizes
	// it. Text search ignores case and punctuation, so compare the returned titles for an exact match.
	TitlePhrase string `json:"titlePhrase,omitempty"`
	// Labels matches pages that carry at least one of these labels, such as policy or in-force.
	Labels []string `json:"labels,omitempty"`
	// ModifiedSince keeps only pages last modified at or after this instant. CQL compares dates in the
	// authorizing user's Confluence time zone, so the connector widens the CQL bound by half a day and
	// removes earlier pages from each result page itself.
	ModifiedSince *time.Time `json:"modifiedSince,omitempty"`
	// AdditionalCQL is one caller-built clause ANDed inside parentheses, such as
	// ancestor = 123456 OR parent = 123456. It must balance its parentheses and quotes and cannot
	// contain ORDER BY. Quote every value that came from outside the application with QuoteCQLString.
	AdditionalCQL string `json:"additionalCql,omitempty"`
	// Order selects the result order; blank uses PageSearchOrderLastModifiedDescending.
	Order PageSearchOrder `json:"order,omitempty"`
}

// QuoteCQLString returns value as one double-quoted CQL string literal, escaping backslashes and double
// quotes, so it can be spliced into caller-built CQL as a value but never as syntax. It rejects invalid
// UTF-8, control characters, and values longer than 1024 characters.
//
//	clause := "space = " + confluence.MustQuoteCQLString("OPS")
func QuoteCQLString(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("CQL value must be valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maximumCQLValueCharacters {
		return "", fmt.Errorf("CQL value cannot exceed %d characters", maximumCQLValueCharacters)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return "", errors.New("CQL value cannot contain control characters")
		}
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`, nil
}

// MustQuoteCQLString is QuoteCQLString for a trusted constant; it panics when the value is rejected.
func MustQuoteCQLString(value string) string {
	quoted, err := QuoteCQLString(value)
	if err != nil {
		panic(err)
	}
	return quoted
}

// CQL returns the escaped CQL query the filter describes.
func (filter PageSearchFilter) CQL() (string, error) {
	clauses := []string{"type = page"}
	for _, list := range []struct {
		field   string
		values  []string
		isValid func(string) bool
		format  string
	}{
		{"space", filter.SpaceKeys, spaceKeyPattern.MatchString, "a space key such as OPS"},
		{"label", filter.Labels, isLabel, "a label without whitespace"},
	} {
		clause, err := buildInClause(list.field, list.values, list.isValid, list.format)
		if err != nil {
			return "", err
		}
		if clause != "" {
			clauses = append(clauses, clause)
		}
	}
	if phrase := strings.TrimSpace(filter.TitlePhrase); phrase != "" {
		clause, err := buildTitlePhraseClause(phrase)
		if err != nil {
			return "", err
		}
		clauses = append(clauses, clause)
	}
	if filter.ModifiedSince != nil {
		if filter.ModifiedSince.IsZero() {
			return "", errors.New("filter modifiedSince must be a time")
		}
		lowerBound := filter.ModifiedSince.UTC().Add(-modifiedSinceTimeZoneMargin).Format(time.DateOnly)
		clauses = append(clauses, `lastmodified >= "`+lowerBound+`"`)
	}
	if additional := strings.TrimSpace(filter.AdditionalCQL); additional != "" {
		if err := validateAdditionalCQL(additional); err != nil {
			return "", err
		}
		clauses = append(clauses, "("+additional+")")
	}
	order, err := buildOrderClause(filter.Order)
	if err != nil {
		return "", err
	}
	return strings.Join(clauses, " AND ") + " " + order, nil
}

func buildInClause(field string, values []string, isValid func(string) bool, format string) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > maximumFilterValues {
		return "", fmt.Errorf("filter accepts at most %d %s values", maximumFilterValues, field)
	}
	quotedValues := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if !isValid(trimmed) {
			return "", fmt.Errorf("filter %s values must each be %s", field, format)
		}
		quoted, err := QuoteCQLString(trimmed)
		if err != nil {
			return "", fmt.Errorf("filter %s value: %w", field, err)
		}
		quotedValues = append(quotedValues, quoted)
	}
	return field + " in (" + strings.Join(quotedValues, ", ") + ")", nil
}

// buildTitlePhraseClause quotes the phrase once for Confluence's text search and once for CQL.
func buildTitlePhraseClause(phrase string) (string, error) {
	textSearchPhrase := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(phrase) + `"`
	quoted, err := QuoteCQLString(textSearchPhrase)
	if err != nil {
		return "", fmt.Errorf("filter titlePhrase: %w", err)
	}
	return "title ~ " + quoted, nil
}

func buildOrderClause(order PageSearchOrder) (string, error) {
	switch order {
	case "", PageSearchOrderLastModifiedDescending:
		return "ORDER BY lastmodified DESC", nil
	case PageSearchOrderCreatedDescending:
		return "ORDER BY created DESC", nil
	case PageSearchOrderTitleAscending:
		return "ORDER BY title ASC", nil
	default:
		return "", errors.New("filter order must be lastModifiedDescending, createdDescending, or titleAscending")
	}
}

// validateAdditionalCQL keeps a caller clause inside its parentheses and rejects ORDER BY outside string literals.
func validateAdditionalCQL(clause string) error {
	switch {
	case !utf8.ValidString(clause):
		return errors.New("filter additionalCql must be valid UTF-8")
	case utf8.RuneCountInString(clause) > maximumAdditionalCQLCharacters:
		return fmt.Errorf("filter additionalCql cannot exceed %d characters", maximumAdditionalCQLCharacters)
	}
	var unquoted strings.Builder
	depth := 0
	var quote rune
	isEscaped := false
	for _, character := range clause {
		if character < ' ' || character == 0x7f {
			return errors.New("filter additionalCql cannot contain control characters")
		}
		switch {
		case isEscaped:
			isEscaped = false
		case quote != 0 && character == '\\':
			isEscaped = true
		case quote != 0 && character == quote:
			quote = 0
		case quote != 0:
		case character == '"' || character == '\'':
			quote = character
		case character == '(':
			depth++
		case character == ')':
			depth--
			if depth < 0 {
				return errors.New("filter additionalCql closes a parenthesis it did not open")
			}
		}
		if quote == 0 {
			unquoted.WriteRune(character)
		} else {
			unquoted.WriteRune(' ')
		}
	}
	switch {
	case quote != 0:
		return errors.New("filter additionalCql has an unterminated string")
	case depth != 0:
		return errors.New("filter additionalCql has an unclosed parenthesis")
	case orderByPattern.MatchString(unquoted.String()):
		return errors.New("filter additionalCql cannot contain ORDER BY; use order instead")
	}
	return nil
}

func isLabel(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > 255 {
		return false
	}
	return !strings.ContainsAny(value, " \t\r\n,") && utf8.ValidString(value)
}
