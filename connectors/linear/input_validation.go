// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxIssueTitleCharacters is the connector's bound on an issue title; Linear documents none.
	MaxIssueTitleCharacters = 255
	// MaxMarkdownCharacters bounds an issue description or comment body the connector sends.
	MaxMarkdownCharacters = 65536
	// MaxLabelIDs bounds the labels one create or update sends.
	MaxLabelIDs = 50

	dueDateLayout = "2006-01-02"
)

var (
	linearUUIDPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	issueIdentifierPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{0,9})-([1-9][0-9]{0,8})$`)
	teamKeyPattern         = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,9}$`)
)

// isLinearUUID reports whether value has the 8-4-4-4-12 hexadecimal shape of every Linear ID.
func isLinearUUID(value string) bool {
	return linearUUIDPattern.MatchString(value)
}

// validateUUIDField checks an optional ID; required reports a blank value.
func validateUUIDField(value string, field string, isRequired bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "" && isRequired:
		return "", fmt.Errorf("%s is required", field)
	case trimmed == "":
		return "", nil
	case !isLinearUUID(trimmed):
		return "", fmt.Errorf("%s must be a Linear UUID such as 2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4", field)
	}
	return strings.ToLower(trimmed), nil
}

// validateUUIDList checks distinct IDs, at most MaxLabelIDs of them.
func validateUUIDList(values []string, field string) ([]string, error) {
	if len(values) > MaxLabelIDs {
		return nil, fmt.Errorf("%s accepts at most %d IDs", field, MaxLabelIDs)
	}
	seen := make(map[string]bool, len(values))
	validated := make([]string, 0, len(values))
	for _, value := range values {
		identifier, err := validateUUIDField(value, field, true)
		if err != nil {
			return nil, err
		}
		if seen[identifier] {
			return nil, fmt.Errorf("%s lists %s twice", field, identifier)
		}
		seen[identifier] = true
		validated = append(validated, identifier)
	}
	return validated, nil
}

// issueLocator is an issue named by UUID or by team key and number.
type issueLocator struct {
	uuid    string
	teamKey string
	number  int
}

// parseIssueLocator accepts a UUID or an identifier such as ENG-123.
func parseIssueLocator(value string, field string) (issueLocator, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return issueLocator{}, fmt.Errorf("%s is required", field)
	}
	if isLinearUUID(trimmed) {
		return issueLocator{uuid: strings.ToLower(trimmed)}, nil
	}
	parts := issueIdentifierPattern.FindStringSubmatch(trimmed)
	if parts == nil {
		return issueLocator{}, fmt.Errorf("%s must be an issue UUID or an identifier such as ENG-123", field)
	}
	number, err := strconv.Atoi(parts[2])
	if err != nil {
		return issueLocator{}, fmt.Errorf("%s number is out of range", field)
	}
	return issueLocator{teamKey: strings.ToUpper(parts[1]), number: number}, nil
}

// issueFilter selects exactly the located issue in an issues query.
func (locator issueLocator) issueFilter() map[string]any {
	if locator.uuid != "" {
		return map[string]any{"id": map[string]any{"eq": locator.uuid}}
	}
	return map[string]any{
		"team":   map[string]any{"key": map[string]any{"eq": locator.teamKey}},
		"number": map[string]any{"eq": locator.number},
	}
}

// mutationID is what issueUpdate and commentCreate accept: the UUID, or the identifier text.
func (locator issueLocator) mutationID() string {
	if locator.uuid != "" {
		return locator.uuid
	}
	return locator.teamKey + "-" + strconv.Itoa(locator.number)
}

// matches reports whether a read issue is the located one.
func (locator issueLocator) matches(summary IssueSummary) bool {
	if locator.uuid != "" {
		return strings.EqualFold(summary.ID, locator.uuid)
	}
	return strings.EqualFold(summary.Identifier, locator.mutationID())
}

// validateOneLineText checks required one-line text within a character limit.
func validateOneLineText(value string, field string, limit int) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return "", fmt.Errorf("%s is required", field)
	case !utf8.ValidString(trimmed):
		return "", fmt.Errorf("%s must be valid UTF-8", field)
	case utf8.RuneCountInString(trimmed) > limit:
		return "", fmt.Errorf("%s cannot exceed %d characters", field, limit)
	}
	for _, character := range trimmed {
		if character < ' ' || character == 0x7f {
			return "", fmt.Errorf("%s must be one line without control characters", field)
		}
	}
	return trimmed, nil
}

// validateMarkdown checks optional Markdown text; tabs and line breaks are allowed.
func validateMarkdown(value string, field string, isRequired bool) (string, error) {
	switch {
	case strings.TrimSpace(value) == "" && isRequired:
		return "", fmt.Errorf("%s is required", field)
	case !utf8.ValidString(value):
		return "", fmt.Errorf("%s must be valid UTF-8", field)
	case utf8.RuneCountInString(value) > MaxMarkdownCharacters:
		return "", fmt.Errorf("%s cannot exceed %d characters", field, MaxMarkdownCharacters)
	}
	for _, character := range value {
		if (character < ' ' && character != '\n' && character != '\r' && character != '\t') || character == 0x7f {
			return "", fmt.Errorf("%s cannot contain control characters", field)
		}
	}
	return value, nil
}

func validatePriority(priority *int) error {
	if priority != nil && (*priority < 0 || *priority > 4) {
		return errors.New("priority must be 0 (none), 1 (urgent), 2 (high), 3 (medium), or 4 (low)")
	}
	return nil
}

func validateDueDate(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if _, err := time.Parse(dueDateLayout, trimmed); err != nil {
		return "", errors.New("dueDate must be a YYYY-MM-DD date such as 2026-02-18")
	}
	return trimmed, nil
}

// validateEmail checks the shape of an address without resolving it; Linear compares it without case.
func validateEmail(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	local, domain, hasAt := strings.Cut(trimmed, "@")
	switch {
	case trimmed == "":
		return "", errors.New("email is required")
	case len(trimmed) > 320 || !hasAt || local == "" || domain == "" || strings.Contains(domain, "@") || !strings.Contains(domain, "."):
		return "", errors.New("email must be one address such as alice@example.com")
	}
	for index := 0; index < len(trimmed); index++ {
		if trimmed[index] <= ' ' || trimmed[index] == 0x7f {
			return "", errors.New("email cannot contain spaces or control characters")
		}
	}
	return trimmed, nil
}

// clientEntityID derives a UUID-v4-shaped record ID that every attempt of one Step execution repeats.
func clientEntityID(key sdkgo.IdempotencyKey, entity string) (string, error) {
	if strings.TrimSpace(string(key)) == "" {
		return "", errors.New("Linear write has no idempotency key")
	}
	digest := sha256.Sum256([]byte("dex-linear-client-id/v1\x00" + entity + "\x00" + string(key)))
	identity := digest[:16]
	identity[6] = (identity[6] & 0x0f) | 0x40
	identity[8] = (identity[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(identity)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}
