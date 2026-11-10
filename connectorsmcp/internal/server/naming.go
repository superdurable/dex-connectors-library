// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package server

import (
	"strings"

	"github.com/superdurable/dex-connectors-library/connectorsmcp/internal/canonicaljson"
)

const (
	// maximumToolNameLength is the longest tool name every model API accepts.
	maximumToolNameLength = 64
	// shortenedToolNamePrefixLength keeps room for "_" and 8 hex characters.
	shortenedToolNamePrefixLength = 55
)

// toolName qualifies names when a connector has several enabled connections
// and shortens names over 64 characters.
func toolName(connectorID string, connectionName string, operationName string, isQualified bool) string {
	name := connectorID + "_" + operationName
	if isQualified {
		name = connectorID + "_" + connectionSlug(connectionName) + "__" + operationName
	}
	if len(name) <= maximumToolNameLength {
		return name
	}
	return name[:shortenedToolNamePrefixLength] + "_" + canonicaljson.SHA256Hex([]byte(name))[:8]
}

// connectionSlug replaces every character outside [A-Za-z0-9_-] with "_".
func connectionSlug(connectionName string) string {
	var slug strings.Builder
	for _, character := range connectionName {
		isAllowed := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-'
		if isAllowed {
			slug.WriteRune(character)
		} else {
			slug.WriteByte('_')
		}
	}
	return slug.String()
}
