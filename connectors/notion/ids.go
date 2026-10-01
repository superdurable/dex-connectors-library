// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const maximumIDTextBytes = 2048

var (
	dashedIDPattern   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	compactIDPattern  = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	trailingIDPattern = regexp.MustCompile(`([0-9a-fA-F]{32})$`)
)

// ParseID returns the dashed, lowercase form of a Notion page, database, data
// source, or user ID, such as 1f3c9a2e-5b7d-4e8a-9c01-23456789abcd.
//
// It accepts the dashed form, the 32-character form without dashes, and an
// HTTPS URL on notion.so, notion.com, or notion.site whose last path segment
// ends with the ID, such as a database's address bar or the link Copy link
// produces. A query string, such as a database view's ?v=, is ignored. A
// database URL holds the database ID, not a data source ID. Errors never
// repeat the value.
func ParseID(value string) (string, error) {
	text := strings.TrimSpace(value)
	switch {
	case text == "":
		return "", fmt.Errorf("a Notion ID is required")
	case len(text) > maximumIDTextBytes:
		return "", fmt.Errorf("a Notion ID or URL is at most %d bytes", maximumIDTextBytes)
	case strings.HasPrefix(strings.ToLower(text), "https://"):
		return parseIDFromURL(text)
	}
	return canonicalIDFromText(text)
}

func parseIDFromURL(text string) (string, error) {
	parsed, err := url.Parse(text)
	if err != nil || !isNotionHost(parsed.Hostname()) {
		return "", fmt.Errorf("a Notion URL must use a notion.so, notion.com, or notion.site host")
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	last := segments[len(segments)-1]
	if dashedIDPattern.MatchString(last) {
		return strings.ToLower(last), nil
	}
	match := trailingIDPattern.FindStringSubmatch(strings.ReplaceAll(last, "-", ""))
	if match == nil {
		return "", fmt.Errorf("the Notion URL does not end with a page or database ID")
	}
	return canonicalIDFromText(match[1])
}

func canonicalIDFromText(text string) (string, error) {
	switch {
	case dashedIDPattern.MatchString(text):
		return strings.ToLower(text), nil
	case compactIDPattern.MatchString(text):
		lower := strings.ToLower(text)
		return lower[0:8] + "-" + lower[8:12] + "-" + lower[12:16] + "-" + lower[16:20] + "-" + lower[20:32], nil
	default:
		return "", fmt.Errorf("a Notion ID is 32 hexadecimal characters, with or without dashes")
	}
}

func isNotionHost(host string) bool {
	host = strings.ToLower(host)
	for _, domain := range []string{"notion.so", "notion.com", "notion.site"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// parseFieldID validates one ID input field and names the field in its error.
func parseFieldID(value string, field string) (string, error) {
	id, err := ParseID(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	return id, nil
}
