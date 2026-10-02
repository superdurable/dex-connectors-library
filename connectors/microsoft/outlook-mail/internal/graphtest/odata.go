// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package graphtest

import (
	"errors"
	"strings"
	"time"
)

// messageFilter is the conjunction of the clauses the connector sends; any other clause is refused.
type messageFilter struct {
	firstProperty     string
	receivedOnOrAfter *time.Time
	receivedBefore    *time.Time
	fromAddress       string
	isUnreadOnly      bool
	subjectContains   string
	markerPropertyID  string
	markerValue       string
}

const markerFilterPrefix = "singleValueExtendedProperties/Any(ep: ep/id eq "

func parseMessageFilter(filter string) (messageFilter, error) {
	var parsed messageFilter
	if strings.TrimSpace(filter) == "" {
		return parsed, nil
	}
	clauses, err := splitTopLevelConjunction(filter)
	if err != nil {
		return messageFilter{}, err
	}
	for index, clause := range clauses {
		property, err := parsed.applyClause(clause)
		if err != nil {
			return messageFilter{}, err
		}
		if index == 0 {
			parsed.firstProperty = property
		}
	}
	return parsed, nil
}

func (filter *messageFilter) applyClause(clause string) (string, error) {
	switch {
	case strings.HasPrefix(clause, "receivedDateTime ge "), strings.HasPrefix(clause, "receivedDateTime lt "):
		instant, err := time.Parse(time.RFC3339, strings.TrimSpace(clause[len("receivedDateTime ge "):]))
		if err != nil {
			return "", errors.New("invalid receivedDateTime literal")
		}
		if strings.HasPrefix(clause, "receivedDateTime ge ") {
			filter.receivedOnOrAfter = &instant
		} else {
			filter.receivedBefore = &instant
		}
		return "receivedDateTime", nil
	case strings.HasPrefix(clause, "from/emailAddress/address eq "):
		value, rest, err := readODataString(clause[len("from/emailAddress/address eq "):])
		if err != nil || rest != "" {
			return "", errors.New("invalid from literal")
		}
		filter.fromAddress = value
		return "from", nil
	case clause == "isRead eq false":
		filter.isUnreadOnly = true
		return "isRead", nil
	case strings.HasPrefix(clause, "contains(subject,"):
		value, rest, err := readODataString(clause[len("contains(subject,"):])
		if err != nil || rest != ")" {
			return "", errors.New("invalid contains literal")
		}
		filter.subjectContains = value
		return "subject", nil
	case strings.HasPrefix(clause, markerFilterPrefix):
		propertyID, rest, err := readODataString(clause[len(markerFilterPrefix):])
		if err != nil || !strings.HasPrefix(rest, " and ep/value eq ") {
			return "", errors.New("invalid extended property filter")
		}
		value, rest, err := readODataString(rest[len(" and ep/value eq "):])
		if err != nil || rest != ")" {
			return "", errors.New("invalid extended property value")
		}
		filter.markerPropertyID, filter.markerValue = propertyID, value
		return "singleValueExtendedProperties", nil
	default:
		return "", errors.New("unsupported filter clause")
	}
}

func (filter messageFilter) matches(message *messageState) bool {
	switch {
	case filter.receivedOnOrAfter != nil && message.receivedAt.Before(*filter.receivedOnOrAfter):
		return false
	case filter.receivedBefore != nil && !message.receivedAt.Before(*filter.receivedBefore):
		return false
	case filter.fromAddress != "" && (message.from == nil || !strings.EqualFold(message.from.address, filter.fromAddress)):
		return false
	case filter.isUnreadOnly && message.isRead:
		return false
	case filter.subjectContains != "" && !strings.Contains(strings.ToLower(message.subject), strings.ToLower(filter.subjectContains)):
		return false
	case filter.markerPropertyID != "":
		// Graph compares the property name with case and the value without case.
		return strings.EqualFold(message.extendedProperties[filter.markerPropertyID], filter.markerValue) &&
			message.extendedProperties[filter.markerPropertyID] != ""
	default:
		return true
	}
}

// splitTopLevelConjunction splits on " and " outside quotes and parentheses.
func splitTopLevelConjunction(filter string) ([]string, error) {
	var clauses []string
	depth, isQuoted, start := 0, false, 0
	for index := 0; index < len(filter); index++ {
		switch character := filter[index]; {
		case character == '\'':
			isQuoted = !isQuoted
		case isQuoted:
		case character == '(':
			depth++
		case character == ')':
			depth--
		case depth == 0 && strings.HasPrefix(filter[index:], " and "):
			clauses = append(clauses, filter[start:index])
			start = index + len(" and ")
			index += len(" and ") - 1
		}
	}
	if isQuoted || depth != 0 {
		return nil, errors.New("unbalanced filter")
	}
	return append(clauses, filter[start:]), nil
}

// readODataString reads one quoted literal with doubled quotes and returns the text after it.
func readODataString(text string) (string, string, error) {
	if !strings.HasPrefix(text, "'") {
		return "", "", errors.New("expected a string literal")
	}
	var value strings.Builder
	for index := 1; index < len(text); index++ {
		if text[index] != '\'' {
			value.WriteByte(text[index])
			continue
		}
		if index+1 < len(text) && text[index+1] == '\'' {
			value.WriteByte('\'')
			index++
			continue
		}
		return value.String(), text[index+1:], nil
	}
	return "", "", errors.New("unterminated string literal")
}
